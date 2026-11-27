package quotes

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"shigoto/internal/core"
)

// Attachment is a file kept with a quote for internal records (a scope of
// work, site photos, the customer's evidence). Attachments are never put
// in the quote PDF or shown in the portal.
type Attachment struct {
	ID        int64
	QuoteID   int64
	FileID    int64
	CreatedBy int64
	CreatedAt time.Time
	File      core.File
}

// requireQuote returns core.ErrNotFound unless quote id exists in orgID.
func requireQuote(ctx context.Context, q core.Querier, orgID, id int64) error {
	var exists bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM quotes WHERE org_id = $1 AND id = $2)`, orgID, id).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return core.ErrNotFound
	}
	return nil
}

// AddAttachment attaches f to quote quoteID, in any status. Attaching the
// same file again returns the existing attachment.
func AddAttachment(ctx context.Context, tx pgx.Tx, orgID, quoteID, userID int64, f core.File) (Attachment, error) {
	if err := requireQuote(ctx, tx, orgID, quoteID); err != nil {
		return Attachment{}, err
	}
	var id int64
	err := tx.QueryRow(ctx, `
		INSERT INTO quote_attachments (org_id, quote_id, file_id, created_by)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (quote_id, file_id) DO UPDATE SET file_id = EXCLUDED.file_id
		RETURNING id`, orgID, quoteID, f.ID, userID).Scan(&id)
	if err != nil {
		return Attachment{}, err
	}
	return GetAttachment(ctx, tx, orgID, quoteID, id)
}

const attachmentSelect = `SELECT a.id, a.quote_id, a.file_id, a.created_by, a.created_at,
	f.id, f.org_id, f.sha256, f.mime, f.size_bytes, f.original_name, f.created_at
	FROM quote_attachments a JOIN files f ON f.id = a.file_id AND f.org_id = a.org_id`

func scanAttachment(row pgx.Row) (Attachment, error) {
	var a Attachment
	err := row.Scan(&a.ID, &a.QuoteID, &a.FileID, &a.CreatedBy, &a.CreatedAt,
		&a.File.ID, &a.File.OrgID, &a.File.SHA256, &a.File.MIME, &a.File.SizeBytes, &a.File.OriginalName, &a.File.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Attachment{}, core.ErrNotFound
	}
	return a, err
}

// GetAttachment returns attachment id of quote quoteID in orgID.
func GetAttachment(ctx context.Context, q core.Querier, orgID, quoteID, id int64) (Attachment, error) {
	return scanAttachment(q.QueryRow(ctx, attachmentSelect+` WHERE a.org_id = $1 AND a.quote_id = $2 AND a.id = $3`, orgID, quoteID, id))
}

// ListAttachments lists quote quoteID's attachments, newest first. It
// returns core.ErrNotFound when the quote isn't in orgID.
func ListAttachments(ctx context.Context, q core.Querier, orgID, quoteID int64) ([]Attachment, error) {
	if err := requireQuote(ctx, q, orgID, quoteID); err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, attachmentSelect+` WHERE a.org_id = $1 AND a.quote_id = $2 ORDER BY a.created_at DESC, a.id DESC`, orgID, quoteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Attachment{}
	for rows.Next() {
		a, err := scanAttachment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// RemoveAttachment removes attachment id from quote quoteID and returns its
// file id, which the caller deletes if unreferenced after commit.
func RemoveAttachment(ctx context.Context, tx pgx.Tx, orgID, quoteID, id int64) (int64, error) {
	var fileID int64
	err := tx.QueryRow(ctx, `DELETE FROM quote_attachments WHERE org_id = $1 AND quote_id = $2 AND id = $3 RETURNING file_id`,
		orgID, quoteID, id).Scan(&fileID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, core.ErrNotFound
	}
	return fileID, err
}
