package property

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"shigoto/internal/core"
	"shigoto/internal/storage"
)

// StrataDocType is the kind of a strata scheme document.
type StrataDocType string

const (
	DocMinutes             StrataDocType = "minutes"
	DocByLaws              StrataDocType = "by_laws"
	DocCMS                 StrataDocType = "cms"
	DocLevyNotice          StrataDocType = "levy_notice"
	DocBudget              StrataDocType = "budget"
	DocFinancialStatement  StrataDocType = "financial_statement"
	DocSinkingFundForecast StrataDocType = "sinking_fund_forecast"
	DocOther               StrataDocType = "other"
)

// StrataDocTypes lists every document type, in display order.
var StrataDocTypes = []StrataDocType{DocMinutes, DocByLaws, DocCMS, DocLevyNotice, DocBudget,
	DocFinancialStatement, DocSinkingFundForecast, DocOther}

// Valid reports whether t is a known document type.
func (t StrataDocType) Valid() bool {
	for _, k := range StrataDocTypes {
		if t == k {
			return true
		}
	}
	return false
}

// Financial reports whether figures are extracted from documents of type t.
func (t StrataDocType) Financial() bool {
	switch t {
	case DocLevyNotice, DocBudget, DocFinancialStatement, DocSinkingFundForecast:
		return true
	}
	return false
}

// StrataDocStatus is a document's indexing state.
type StrataDocStatus string

const (
	DocPending    StrataDocStatus = "pending"
	DocExtracting StrataDocStatus = "extracting"
	DocIndexed    StrataDocStatus = "indexed"
	DocFailed     StrataDocStatus = "failed"
)

const (
	// strataMaxPages caps an uploaded document's page count.
	strataMaxPages = 300
	// strataMaxTitle caps a document title's length.
	strataMaxTitle = 200
	// StrataMaxAttempts is how many times indexing is tried before a
	// failure is final.
	StrataMaxAttempts = 6
)

// StrataDocument is an uploaded scheme document and its indexing state.
type StrataDocument struct {
	ID, OrgID, SchemeID, FileID int64
	DocType                     StrataDocType
	Title                       string
	DocumentDate                time.Time
	Status                      StrataDocStatus
	Error                       string
	Attempts                    int
	NextAttemptAt               *time.Time
	PageCount                   int
	ExtractModel                string
	ExtractedAt                 *time.Time
	CreatedAt                   time.Time
	// ChunksTotal and ChunksEmbedded (chunks embedded with the current
	// embedding model) are filled by ListStrataDocuments only.
	ChunksTotal, ChunksEmbedded int
}

const strataDocumentColumns = `d.id, d.org_id, d.scheme_id, d.file_id, d.doc_type, d.title, d.document_date, d.status,
	d.error, d.attempts, d.next_attempt_at, d.page_count, d.extract_model, d.extracted_at, d.created_at`

func scanStrataDocument(row pgx.Row, extra ...any) (StrataDocument, error) {
	var d StrataDocument
	dest := append([]any{&d.ID, &d.OrgID, &d.SchemeID, &d.FileID, &d.DocType, &d.Title, &d.DocumentDate, &d.Status,
		&d.Error, &d.Attempts, &d.NextAttemptAt, &d.PageCount, &d.ExtractModel, &d.ExtractedAt, &d.CreatedAt}, extra...)
	err := row.Scan(dest...)
	return d, err
}

func validateStrataDocument(d StrataDocument) (StrataDocument, error) {
	d.Title = strings.TrimSpace(d.Title)
	switch {
	case d.Title == "":
		return d, fmt.Errorf("%w: title is required", core.ErrInvalidInput)
	case len([]rune(d.Title)) > strataMaxTitle:
		return d, fmt.Errorf("%w: title must be at most %d characters", core.ErrInvalidInput, strataMaxTitle)
	case d.DocumentDate.IsZero():
		return d, fmt.Errorf("%w: document_date is required", core.ErrInvalidInput)
	case !d.DocType.Valid():
		return d, fmt.Errorf("%w: unknown doc_type %q", core.ErrInvalidInput, d.DocType)
	case d.PageCount < 1:
		return d, fmt.Errorf("%w: the PDF has no pages", core.ErrInvalidInput)
	case d.PageCount > strataMaxPages:
		return d, fmt.Errorf("%w: PDF has more than %d pages", core.ErrInvalidInput, strataMaxPages)
	}
	return d, nil
}

// CreateStrataDocument records file d.FileID as a pending document of
// scheme schemeID. The scheme and the file must both belong to orgID,
// otherwise it returns core.ErrNotFound.
func CreateStrataDocument(ctx context.Context, q core.Querier, orgID, schemeID int64, d StrataDocument) (StrataDocument, error) {
	d, err := validateStrataDocument(d)
	if err != nil {
		return StrataDocument{}, err
	}
	created, err := scanStrataDocument(q.QueryRow(ctx, `
		WITH ins AS (
			INSERT INTO strata_documents (org_id, scheme_id, file_id, doc_type, title, document_date, page_count)
			SELECT $1, s.id, f.id, $4, $5, $6, $7
			FROM strata_schemes s, files f
			WHERE s.org_id = $1 AND s.id = $2 AND f.org_id = $1 AND f.id = $3
			RETURNING *
		)
		SELECT `+strataDocumentColumns+` FROM ins d`,
		orgID, schemeID, d.FileID, d.DocType, d.Title, d.DocumentDate, d.PageCount))
	if errors.Is(err, pgx.ErrNoRows) {
		return StrataDocument{}, core.ErrNotFound
	}
	if isUniqueViolation(err) {
		return StrataDocument{}, fmt.Errorf("%w: this file is already a document of this scheme", core.ErrInvalidInput)
	}
	return created, err
}

// ListStrataDocuments lists scheme schemeID's documents, newest document
// date first, with chunk counts; ChunksEmbedded counts chunks embedded
// with embedModel.
func ListStrataDocuments(ctx context.Context, q core.Querier, orgID, schemeID int64, embedModel string) ([]StrataDocument, error) {
	rows, err := q.Query(ctx, `
		SELECT `+strataDocumentColumns+`,
			(SELECT count(*) FROM strata_chunks c WHERE c.document_id = d.id),
			(SELECT count(*) FROM strata_chunks c WHERE c.document_id = d.id AND c.embed_model = $3)
		FROM strata_documents d
		WHERE d.org_id = $1 AND d.scheme_id = $2
		ORDER BY d.document_date DESC, d.id DESC`, orgID, schemeID, embedModel)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	docs := []StrataDocument{}
	for rows.Next() {
		var total, embedded int
		d, err := scanStrataDocument(rows, &total, &embedded)
		if err != nil {
			return nil, err
		}
		d.ChunksTotal, d.ChunksEmbedded = total, embedded
		docs = append(docs, d)
	}
	return docs, rows.Err()
}

// GetStrataDocument returns document docID of scheme schemeID in orgID,
// or core.ErrNotFound.
func GetStrataDocument(ctx context.Context, q core.Querier, orgID, schemeID, docID int64) (StrataDocument, error) {
	d, err := scanStrataDocument(q.QueryRow(ctx, `SELECT `+strataDocumentColumns+` FROM strata_documents d
		WHERE d.org_id = $1 AND d.scheme_id = $2 AND d.id = $3`, orgID, schemeID, docID))
	if errors.Is(err, pgx.ErrNoRows) {
		return StrataDocument{}, core.ErrNotFound
	}
	return d, err
}

// DeleteStrataDocument deletes a document and everything derived from it,
// then its file and blob when nothing else references them.
func DeleteStrataDocument(ctx context.Context, pool *pgxpool.Pool, store storage.Store, orgID, schemeID, docID int64) error {
	var fileID int64
	err := pool.QueryRow(ctx, `DELETE FROM strata_documents WHERE org_id = $1 AND scheme_id = $2 AND id = $3 RETURNING file_id`,
		orgID, schemeID, docID).Scan(&fileID)
	if errors.Is(err, pgx.ErrNoRows) {
		return core.ErrNotFound
	}
	if err != nil {
		return err
	}
	return DeleteFileIfUnreferenced(ctx, pool, store, fileID)
}

// ReindexStrataDocument queues a document to be read again from scratch,
// e.g. after a failure or an extraction-model change.
func ReindexStrataDocument(ctx context.Context, q core.Querier, orgID, schemeID, docID int64) error {
	tag, err := q.Exec(ctx, `UPDATE strata_documents
		SET status = 'pending', attempts = 0, error = '', next_attempt_at = NULL
		WHERE org_id = $1 AND scheme_id = $2 AND id = $3`, orgID, schemeID, docID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return core.ErrNotFound
	}
	return nil
}
