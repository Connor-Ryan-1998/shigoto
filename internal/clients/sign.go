package clients

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"shigoto/internal/core"
	"shigoto/internal/storage"
)

var (
	// ErrNameRequired means the typed name is blank.
	ErrNameRequired error = &core.PublicError{Err: core.ErrInvalidInput, Msg: "type your full name"}
	// ErrNameTooLong means the typed name exceeds 200 characters.
	ErrNameTooLong error = &core.PublicError{Err: core.ErrInvalidInput, Msg: "name must be at most 200 characters"}
	// ErrConsentRequired means the consent box wasn't ticked.
	ErrConsentRequired error = &core.PublicError{Err: core.ErrInvalidInput, Msg: "tick the consent box to sign"}
	// ErrAlreadySigned means the document already has a signature.
	ErrAlreadySigned error = &core.PublicError{Err: core.ErrInvalidState, Msg: "already signed"}
	// ErrFileChanged means the stored file no longer matches the hash
	// recorded at upload, so it can't be signed.
	ErrFileChanged error = &core.PublicError{Err: core.ErrInvalidState, Msg: "document file has changed"}
)

// SignInput is what the signing client supplies plus request evidence.
type SignInput struct {
	UserID    int64
	Email     string
	TypedName string
	IP        string
	UserAgent string
	Consent   bool
}

// Sign records contactID's signature on sent signature document docID: it
// verifies the stored file still matches the hash recorded at upload,
// stores the evidence, generates and stores a signing certificate PDF and
// marks the document signed, all in one transaction. A shared document, or
// one belonging to another contact, is core.ErrNotFound.
func Sign(ctx context.Context, pool *pgxpool.Pool, store storage.Store, loc *time.Location, orgID, contactID, docID int64, in SignInput) (Document, error) {
	var signed Document
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var err error
		signed, err = SignTx(ctx, tx, store, loc, orgID, contactID, docID, in)
		return err
	})
	if err != nil {
		return Document{}, err
	}
	return signed, nil
}

// SignTx is Sign inside the caller's transaction tx. The certificate blob is
// stored before it returns; if tx then rolls back, only an unreferenced blob
// is left behind. in is validated here too.
func SignTx(ctx context.Context, tx pgx.Tx, store storage.Store, loc *time.Location, orgID, contactID, docID int64, in SignInput) (Document, error) {
	in.TypedName = strings.TrimSpace(in.TypedName)
	if in.TypedName == "" {
		return Document{}, ErrNameRequired
	}
	if utf8.RuneCountInString(in.TypedName) > maxTitle {
		return Document{}, ErrNameTooLong
	}
	if !in.Consent {
		return Document{}, ErrConsentRequired
	}
	var staged *core.Staged
	var signed Document
	err := func() error {

		var exists bool
		if err := tx.QueryRow(ctx, `SELECT true FROM client_documents
			WHERE org_id = $1 AND contact_id = $2 AND id = $3 AND kind = 'signature' FOR UPDATE`,
			orgID, contactID, docID).Scan(&exists); errors.Is(err, pgx.ErrNoRows) {
			return core.ErrNotFound
		} else if err != nil {
			return err
		}
		doc, err := GetDocument(ctx, tx, orgID, docID)
		if err != nil {
			return err
		}
		switch doc.Status {
		case StatusSigned:
			return ErrAlreadySigned
		case StatusSent:
		default:
			return ErrBadState
		}

		file, err := core.GetFile(ctx, tx, orgID, doc.FileID)
		if err != nil {
			return err
		}
		sum, err := fileHash(ctx, store, file)
		if err != nil {
			return err
		}
		if !bytes.Equal(sum, doc.FileSHA256) {
			return ErrFileChanged
		}

		contact, err := core.GetContact(ctx, tx, orgID, contactID)
		if err != nil {
			return err
		}
		var orgName string
		if err := tx.QueryRow(ctx, `SELECT name FROM orgs WHERE id = $1`, orgID).Scan(&orgName); err != nil {
			return err
		}
		var sigID int64
		var signedAt time.Time
		if err := tx.QueryRow(ctx, `SELECT nextval(pg_get_serial_sequence('document_signatures', 'id')), now()`).
			Scan(&sigID, &signedAt); err != nil {
			return err
		}
		consent := ConsentText(contact.Name)
		var pdf bytes.Buffer
		if err := WriteCertificatePDF(&pdf, CertificateData{
			DocumentTitle: doc.Title, FileName: DocumentFileName(doc), FileSHA256: sum,
			SignerName: in.TypedName, SignerEmail: in.Email, ClientName: contact.Name, OrgName: orgName,
			IP: in.IP, UserAgent: in.UserAgent, ConsentText: consent, SignedAt: signedAt, SignatureID: sigID,
		}, loc); err != nil {
			return err
		}
		staged, err = core.StageFile(ctx, store, orgID, doc.Title+" - signing certificate.pdf", &pdf, PDFPolicy)
		if err != nil {
			return err
		}
		cert, err := staged.Save(ctx, tx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO document_signatures (id, org_id, document_id, user_id, typed_name, email,
				consent_text, signed_at, ip, user_agent, file_sha256, certificate_file_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
			sigID, orgID, docID, in.UserID, in.TypedName, in.Email, consent, signedAt,
			in.IP, in.UserAgent, sum, cert.ID); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return ErrAlreadySigned
			}
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE client_documents SET status = 'signed', updated_at = now()
			WHERE org_id = $1 AND id = $2`, orgID, docID); err != nil {
			return err
		}
		if err := core.Audit(ctx, tx, orgID, &in.UserID, "client_document", docID, "sign",
			map[string]any{"signature_id": sigID, "certificate_file_id": cert.ID}); err != nil {
			return err
		}
		if signed, err = GetDocument(ctx, tx, orgID, docID); err != nil {
			return err
		}
		// Store the certificate before the transaction commits: a storage
		// failure then rolls the signature back instead of recording one
		// whose certificate is missing. If the commit itself fails after
		// this, only an unreferenced blob is left behind.
		if err := staged.Commit(ctx); err != nil {
			return err
		}
		staged = nil
		return nil
	}()
	if err != nil {
		if staged != nil {
			_ = staged.Abort()
		}
		return Document{}, err
	}
	return signed, nil
}

func fileHash(ctx context.Context, store storage.Store, f core.File) ([]byte, error) {
	blob, err := core.OpenFile(ctx, store, f)
	if err != nil {
		return nil, err
	}
	defer blob.Close()
	h := sha256.New()
	if _, err := io.Copy(h, blob); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

// SignedRecipients returns the emails staff are notified at when a client
// signs: every org owner plus the uploader (when given), deduplicated.
func SignedRecipients(ctx context.Context, q core.Querier, orgID int64, uploaderID *int64) ([]string, error) {
	rows, err := q.Query(ctx, `
		SELECT DISTINCT u.email FROM users u JOIN memberships m ON m.user_id = u.id
		WHERE m.org_id = $1 AND (m.role = 'owner' OR u.id = $2)
		ORDER BY u.email`, orgID, uploaderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
