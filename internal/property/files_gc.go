package property

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"shigoto/internal/core"
	"shigoto/internal/storage"
)

// pendingFileTTL is how long an upload may wait for its ticket.
const pendingFileTTL = 24 * time.Hour

// FileReferences lists every column (other than
// maintenance_pending_files.file_id) that references files(id). A file is
// only deleted when none of these still use it; a test checks the list
// against the schema's foreign keys.
var FileReferences = []struct{ Table, Column string }{
	{"expenses", "receipt_file_id"},
	{"insurance_policies", "file_id"},
	{"depreciation_documents", "file_id"},
	{"maintenance_ticket_files", "file_id"},
	{"client_documents", "file_id"},
	{"document_signatures", "certificate_file_id"},
	{"strata_documents", "file_id"},
	{"orgs", "logo_file_id"},
}

// unreferencedClause is the WHERE condition that no FileReferences column
// or pending upload still points at files row f.
func unreferencedClause() string {
	var refs []string
	for _, r := range FileReferences {
		refs = append(refs, fmt.Sprintf("NOT EXISTS (SELECT 1 FROM %s r WHERE r.%s = f.id)", r.Table, r.Column))
	}
	refs = append(refs, "NOT EXISTS (SELECT 1 FROM maintenance_pending_files r WHERE r.file_id = f.id)")
	return strings.Join(refs, " AND ")
}

// DeleteFileIfUnreferenced deletes files row id, then its blob, when
// nothing references it any more. A still-referenced file is kept. Call it
// outside (after) the transaction that dropped the last reference, so a
// rollback can't leave a files row without its blob.
func DeleteFileIfUnreferenced(ctx context.Context, q core.Querier, store storage.Store, id int64) error {
	var key storage.Key
	err := q.QueryRow(ctx, `DELETE FROM files f WHERE f.id = $1 AND `+unreferencedClause()+`
		RETURNING f.org_id, f.sha256`, id).Scan(&key.OrgID, &key.SHA256)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return store.Delete(ctx, key)
}

// DeletePendingFile removes userID's own pending upload (else
// core.ErrNotFound) and its file and bytes when nothing else uses them.
func DeletePendingFile(ctx context.Context, pool *pgxpool.Pool, store storage.Store, orgID, userID, pendingID int64) error {
	var fileID int64
	err := pool.QueryRow(ctx, `DELETE FROM maintenance_pending_files WHERE org_id = $1 AND user_id = $2 AND id = $3 RETURNING file_id`,
		orgID, userID, pendingID).Scan(&fileID)
	if errors.Is(err, pgx.ErrNoRows) {
		return core.ErrNotFound
	}
	if err != nil {
		return err
	}
	return DeleteFileIfUnreferenced(ctx, pool, store, fileID)
}

// PrunePendingFiles deletes pending uploads older than 24 hours as of now,
// then each such file's row and blob when nothing else references it.
// Failures for one file are logged; the rest continue.
func PrunePendingFiles(ctx context.Context, pool *pgxpool.Pool, store storage.Store, now time.Time) error {
	rows, err := pool.Query(ctx, `
		WITH expired AS (
			DELETE FROM maintenance_pending_files WHERE created_at < $1 RETURNING file_id
		)
		SELECT DISTINCT file_id FROM expired`, now.Add(-pendingFileTTL))
	if err != nil {
		return err
	}
	var fileIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		fileIDs = append(fileIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range fileIDs {
		if err := DeleteFileIfUnreferenced(ctx, pool, store, id); err != nil {
			slog.Error("deleting expired upload", "file_id", id, "error", err)
		}
	}
	return nil
}
