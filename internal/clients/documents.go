package clients

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"shigoto/internal/core"
	"shigoto/internal/property"
	"shigoto/internal/storage"
)

// PDFPolicy is the upload policy for client documents: PDF only, 10 MiB.
var PDFPolicy = core.UploadPolicy{MaxBytes: 10 << 20, Allowed: map[string]bool{"application/pdf": true}}

// Document states and kinds.
const (
	StatusDraft      = "draft"
	StatusSent       = "sent"
	StatusSigned     = "signed"
	StatusWithdrawn  = "withdrawn"
	StatusSuperseded = "superseded"

	KindSignature = "signature"
	KindShared    = "shared"

	CategoryGeneral   = "general"
	CategoryDiscovery = "discovery"
)

var (
	// ErrProjectOtherClient means the project isn't the document contact's.
	ErrProjectOtherClient error = &core.PublicError{Err: core.ErrInvalidInput, Msg: "project belongs to another client"}
	// ErrNoPortalLogin means a signature document can't be sent because
	// the client has no portal login to sign with.
	ErrNoPortalLogin error = &core.PublicError{Err: core.ErrInvalidInput, Msg: "Invite this client to the portal first"}
	// ErrBadState means the document's status doesn't allow the action.
	ErrBadState error = &core.PublicError{Err: core.ErrInvalidState, Msg: "document can't be changed in its current state"}
	// ErrSigned means the document is signed and therefore immutable.
	ErrSigned error = &core.PublicError{Err: core.ErrInvalidState, Msg: "signed documents can't be changed"}
)

const maxTitle = 200

// Document is a file staff shared with, or sent for signature to, a client
// contact. ProjectID is nil for a general document (e.g. an MSA).
type Document struct {
	ID           int64
	ContactID    int64
	ProjectID    *int64
	Title        string
	Kind         string
	Category     string
	Status       string
	FileID       int64
	FileSHA256   []byte
	SupersedesID *int64
	SentAt       *time.Time
	CreatedBy    *int64
	CreatedAt    time.Time
	Signature    *Signature
}

// Signature is the evidence recorded when the client signed.
type Signature struct {
	ID                int64
	TypedName         string
	Email             string
	SignedAt          time.Time
	CertificateFileID int64
}

// NewDocument is the metadata for a document being uploaded.
type NewDocument struct {
	ContactID int64
	ProjectID *int64
	Title     string
	Kind      string
	Category  string
}

const documentColumns = `d.id, d.contact_id, d.project_id, d.title, d.kind, d.category, d.status,
	d.file_id, d.file_sha256, d.supersedes_id, d.sent_at, d.created_by, d.created_at,
	s.id, s.typed_name, s.email, s.signed_at, s.certificate_file_id`

const documentFrom = ` FROM client_documents d LEFT JOIN document_signatures s ON s.document_id = d.id`

func scanDocument(row pgx.Row) (Document, error) {
	var d Document
	var sigID, certID *int64
	var name, email *string
	var signedAt *time.Time
	err := row.Scan(&d.ID, &d.ContactID, &d.ProjectID, &d.Title, &d.Kind, &d.Category, &d.Status,
		&d.FileID, &d.FileSHA256, &d.SupersedesID, &d.SentAt, &d.CreatedBy, &d.CreatedAt,
		&sigID, &name, &email, &signedAt, &certID)
	if err != nil {
		return Document{}, err
	}
	if sigID != nil {
		d.Signature = &Signature{ID: *sigID, TypedName: *name, Email: *email, SignedAt: *signedAt, CertificateFileID: *certID}
	}
	return d, nil
}

func scanDocuments(rows pgx.Rows) ([]Document, error) {
	defer rows.Close()
	docs := []Document{}
	for rows.Next() {
		d, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		docs = append(docs, d)
	}
	return docs, rows.Err()
}

func validateNewDocument(nd *NewDocument) error {
	nd.Title = strings.TrimSpace(nd.Title)
	if nd.Title == "" {
		return invalid("title is required")
	}
	if utf8.RuneCountInString(nd.Title) > maxTitle {
		return invalid("title must be at most 200 characters")
	}
	if nd.Kind != KindSignature && nd.Kind != KindShared {
		return invalid("kind must be signature or shared")
	}
	if nd.Category == "" {
		nd.Category = CategoryGeneral
	}
	if nd.Category != CategoryGeneral && nd.Category != CategoryDiscovery {
		return invalid("category must be general or discovery")
	}
	return nil
}

// checkProject requires projectID to be one of contactID's projects in orgID.
func checkProject(ctx context.Context, q core.Querier, orgID, contactID int64, projectID *int64) error {
	if projectID == nil {
		return nil
	}
	var owner int64
	err := q.QueryRow(ctx, `SELECT client_contact_id FROM projects WHERE org_id = $1 AND id = $2`, orgID, *projectID).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return invalid("project not found")
	}
	if err != nil {
		return err
	}
	if owner != contactID {
		return ErrProjectOtherClient
	}
	return nil
}

// CreateDocument records f as a new draft document for nd.ContactID. The
// file's SHA-256 is copied so a signature can later prove which bytes were
// signed.
func CreateDocument(ctx context.Context, q core.Querier, orgID, userID int64, nd NewDocument, f core.File) (Document, error) {
	if err := validateNewDocument(&nd); err != nil {
		return Document{}, err
	}
	if _, err := RequireClient(ctx, q, orgID, nd.ContactID); err != nil {
		return Document{}, err
	}
	if err := checkProject(ctx, q, orgID, nd.ContactID, nd.ProjectID); err != nil {
		return Document{}, err
	}
	return insertDocument(ctx, q, orgID, userID, nd, f, nil)
}

func insertDocument(ctx context.Context, q core.Querier, orgID, userID int64, nd NewDocument, f core.File, supersedes *int64) (Document, error) {
	var id int64
	err := q.QueryRow(ctx, `
		INSERT INTO client_documents (org_id, contact_id, project_id, title, kind, category,
			file_id, file_sha256, supersedes_id, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`,
		orgID, nd.ContactID, nd.ProjectID, nd.Title, nd.Kind, nd.Category, f.ID, f.SHA256, supersedes, userID,
	).Scan(&id)
	if err != nil {
		return Document{}, err
	}
	if err := core.Audit(ctx, q, orgID, &userID, "client_document", id, "create",
		map[string]any{"kind": nd.Kind, "title": nd.Title, "file_id": f.ID}); err != nil {
		return Document{}, err
	}
	return GetDocument(ctx, q, orgID, id)
}

// GetDocument returns orgID's document id, with its signature if signed.
func GetDocument(ctx context.Context, q core.Querier, orgID, id int64) (Document, error) {
	d, err := scanDocument(q.QueryRow(ctx, `SELECT `+documentColumns+documentFrom+` WHERE d.org_id = $1 AND d.id = $2`, orgID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, core.ErrNotFound
	}
	return d, err
}

// lockDocument reads id FOR UPDATE.
func lockDocument(ctx context.Context, q core.Querier, orgID, id int64) (Document, error) {
	if _, err := q.Exec(ctx, `SELECT 1 FROM client_documents WHERE org_id = $1 AND id = $2 FOR UPDATE`, orgID, id); err != nil {
		return Document{}, err
	}
	return GetDocument(ctx, q, orgID, id)
}

// ListDocuments returns every document for contactID (all statuses), newest
// first, optionally only those attached to projectID.
func ListDocuments(ctx context.Context, q core.Querier, orgID, contactID int64, projectID *int64) ([]Document, error) {
	if _, err := RequireClient(ctx, q, orgID, contactID); err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `SELECT `+documentColumns+documentFrom+`
		WHERE d.org_id = $1 AND d.contact_id = $2 AND ($3::bigint IS NULL OR d.project_id = $3)
		ORDER BY d.created_at DESC, d.id DESC`, orgID, contactID, projectID)
	if err != nil {
		return nil, err
	}
	return scanDocuments(rows)
}

func setStatus(ctx context.Context, q core.Querier, orgID, userID, id int64, status, action string) (Document, error) {
	if _, err := q.Exec(ctx, `UPDATE client_documents SET status = $1, updated_at = now(),
		sent_at = CASE WHEN $1 = 'sent' THEN now() ELSE sent_at END
		WHERE org_id = $2 AND id = $3`, status, orgID, id); err != nil {
		return Document{}, err
	}
	if err := core.Audit(ctx, q, orgID, &userID, "client_document", id, action, nil); err != nil {
		return Document{}, err
	}
	return GetDocument(ctx, q, orgID, id)
}

// SendDocument makes draft id visible to the client. A signature document
// needs the client to have a portal login, otherwise ErrNoPortalLogin.
func SendDocument(ctx context.Context, q core.Querier, orgID, userID, id int64) (Document, error) {
	d, err := lockDocument(ctx, q, orgID, id)
	if err != nil {
		return Document{}, err
	}
	if d.Status != StatusDraft {
		return Document{}, ErrBadState
	}
	if d.Kind == KindSignature {
		if _, ok, err := PortalLoginEmail(ctx, q, orgID, d.ContactID); err != nil {
			return Document{}, err
		} else if !ok {
			return Document{}, ErrNoPortalLogin
		}
	}
	// Sending a replacement is what supersedes the version it replaces, so
	// deleting an unsent replacement leaves the original untouched.
	if d.SupersedesID != nil {
		prev, err := lockDocument(ctx, q, orgID, *d.SupersedesID)
		if err != nil {
			return Document{}, err
		}
		switch prev.Status {
		case StatusSigned:
			return Document{}, ErrSigned
		case StatusSent:
			if _, err := setStatus(ctx, q, orgID, userID, prev.ID, StatusSuperseded, "supersede"); err != nil {
				return Document{}, err
			}
		}
	}
	return setStatus(ctx, q, orgID, userID, id, StatusSent, "send")
}

// WithdrawDocument withdraws sent, unsigned document id. The client still
// sees it, marked withdrawn, without a Sign action.
func WithdrawDocument(ctx context.Context, q core.Querier, orgID, userID, id int64) (Document, error) {
	d, err := lockDocument(ctx, q, orgID, id)
	if err != nil {
		return Document{}, err
	}
	switch d.Status {
	case StatusSigned:
		return Document{}, ErrSigned
	case StatusSent:
		return setStatus(ctx, q, orgID, userID, id, StatusWithdrawn, "withdraw")
	default:
		return Document{}, ErrBadState
	}
}

// ReplaceDocument creates a draft replacement for sent, unsigned document
// id, holding f and the same contact, project, title, kind and category.
// The original stays live until the replacement is sent.
func ReplaceDocument(ctx context.Context, q core.Querier, orgID, userID, id int64, f core.File) (Document, error) {
	d, err := lockDocument(ctx, q, orgID, id)
	if err != nil {
		return Document{}, err
	}
	switch d.Status {
	case StatusSigned:
		return Document{}, ErrSigned
	case StatusSent:
	default:
		return Document{}, ErrBadState
	}
	if err := core.Audit(ctx, q, orgID, &userID, "client_document", id, "replace", nil); err != nil {
		return Document{}, err
	}
	return insertDocument(ctx, q, orgID, userID, NewDocument{
		ContactID: d.ContactID, ProjectID: d.ProjectID, Title: d.Title, Kind: d.Kind, Category: d.Category,
	}, f, &d.ID)
}

// DeleteDraft deletes draft document id and, when nothing else uses it,
// its file. Only drafts (never seen by the client) can be deleted. It runs
// its own transaction and removes the blob only after commit.
func DeleteDraft(ctx context.Context, pool *pgxpool.Pool, store storage.Store, orgID, userID, id int64) error {
	var fileID int64
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		d, err := lockDocument(ctx, tx, orgID, id)
		if err != nil {
			return err
		}
		if d.Status == StatusSigned {
			return ErrSigned
		}
		if d.Status != StatusDraft {
			return ErrBadState
		}
		// A replacement draft points at the document it replaces, never the
		// other way round, and the original isn't superseded until the
		// replacement is sent, so deleting a draft changes nothing else.
		if _, err := tx.Exec(ctx, `DELETE FROM client_documents WHERE org_id = $1 AND id = $2`, orgID, id); err != nil {
			return err
		}
		fileID = d.FileID
		return core.Audit(ctx, tx, orgID, &userID, "client_document", id, "delete", nil)
	})
	if err != nil {
		return err
	}
	return property.DeleteFileIfUnreferenced(ctx, pool, store, fileID)
}

// PortalLoginEmail returns the email of the user with a client portal
// membership linked to contactID, and false when there is none.
func PortalLoginEmail(ctx context.Context, q core.Querier, orgID, contactID int64) (string, bool, error) {
	var email string
	err := q.QueryRow(ctx, `
		SELECT u.email FROM memberships m JOIN users u ON u.id = m.user_id
		WHERE m.org_id = $1 AND m.contact_id = $2 AND m.role = 'client'
		ORDER BY u.id LIMIT 1`, orgID, contactID).Scan(&email)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return email, true, nil
}
