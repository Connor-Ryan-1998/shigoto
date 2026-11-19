package core

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io"
	"time"

	"github.com/jackc/pgx/v5"

	"shigoto/internal/storage"
)

// ErrTooLarge is returned by SaveFile when the content exceeds the upload
// policy's MaxBytes. Handlers map it to 413.
var ErrTooLarge = errors.New("file too large")

// UploadPolicy limits what SaveFile accepts: MaxBytes caps the size (0
// means unlimited) and Allowed lists the MIME types, sniffed from the
// file's own bytes rather than trusted from its name or declared type.
type UploadPolicy struct {
	MaxBytes int64
	Allowed  map[string]bool
}

// DocumentPolicy is for receipts, insurance and depreciation documents:
// PDF, JPEG or PNG up to 10 MiB.
var DocumentPolicy = UploadPolicy{
	MaxBytes: 10 << 20,
	Allowed: map[string]bool{
		"application/pdf": true,
		"image/jpeg":      true,
		"image/png":       true,
	},
}

// MediaPolicy is for maintenance photos, video and voice notes, capped at
// maxBytes (0 = unlimited).
func MediaPolicy(maxBytes int64) UploadPolicy {
	return UploadPolicy{
		MaxBytes: maxBytes,
		Allowed: map[string]bool{
			"image/jpeg":      true,
			"image/png":       true,
			"image/heic":      true,
			"image/heif":      true,
			"video/mp4":       true,
			"video/quicktime": true,
			"video/webm":      true,
			"audio/mp4":       true,
			"audio/webm":      true,
			"audio/ogg":       true,
			"audio/mpeg":      true,
		},
	}
}

// File is an uploaded file's metadata. Its bytes live in a storage.Store
// under (OrgID, SHA256) - never under OriginalName, which is kept purely as
// download metadata.
type File struct {
	ID           int64
	OrgID        int64
	SHA256       []byte
	MIME         string
	SizeBytes    int64
	OriginalName string
	CreatedAt    time.Time
}

// Key returns f's storage key.
func (f File) Key() storage.Key { return storage.Key{OrgID: f.OrgID, SHA256: f.SHA256} }

const fileColumns = `id, org_id, sha256, mime, size_bytes, original_name, created_at`

func scanFile(row pgx.Row) (File, error) {
	var f File
	err := row.Scan(&f.ID, &f.OrgID, &f.SHA256, &f.MIME, &f.SizeBytes, &f.OriginalName, &f.CreatedAt)
	return f, err
}

// limitedHashWriter forwards writes to w and h, failing with ErrTooLarge
// once more than max bytes (max > 0) have been written.
type limitedHashWriter struct {
	w   io.Writer
	h   hash.Hash
	n   int64
	max int64
}

func (l *limitedHashWriter) Write(p []byte) (int, error) {
	if l.max > 0 && l.n+int64(len(p)) > l.max {
		return 0, ErrTooLarge
	}
	l.h.Write(p)
	n, err := l.w.Write(p)
	l.n += int64(n)
	return n, err
}

// Staged is an upload streamed into the store's staging area but not yet
// recorded: StageFile reads the body with no database work, so a slow
// client never holds a connection or transaction. Call Save inside a short
// transaction, then Commit after it commits (or Abort on any failure).
type Staged struct {
	OrgID   int64
	Name    string
	MIME    string
	Size    int64
	SHA256  []byte
	pending storage.Pending
}

// StageFile sniffs r's MIME type from its first 512 bytes, rejecting a type
// pol does not allow with ErrInvalidInput before reading further, then
// streams the rest into store, failing with ErrTooLarge past pol.MaxBytes.
// On error nothing is left behind. A read error from r (for example
// *http.MaxBytesError) is returned wrapped.
func StageFile(ctx context.Context, store storage.Store, orgID int64, name string, r io.Reader, pol UploadPolicy) (*Staged, error) {
	head := make([]byte, 512)
	n, err := io.ReadFull(r, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, err
	}
	head = head[:n]
	mime := SniffMIME(head)
	if !pol.Allowed[mime] {
		return nil, fmt.Errorf("%w: unsupported file type %q", ErrInvalidInput, mime)
	}
	pending, err := store.Begin(ctx, orgID)
	if err != nil {
		return nil, err
	}
	w := &limitedHashWriter{w: pending, h: sha256.New(), max: pol.MaxBytes}
	if _, err := w.Write(head); err != nil {
		_ = pending.Abort()
		return nil, err
	}
	if _, err := io.Copy(w, r); err != nil {
		_ = pending.Abort()
		return nil, err
	}
	if name == "" {
		name = "receipt"
	}
	return &Staged{OrgID: orgID, Name: name, MIME: mime, Size: w.n, SHA256: w.h.Sum(nil), pending: pending}, nil
}

// Save records the staged file in files. If an (org_id, sha256) row
// already exists - including a concurrent upload of the same content - the
// existing row is returned. ON CONFLICT DO NOTHING (rather than catching
// the unique_violation) keeps the caller's transaction usable for the
// follow-up lookup.
func (s *Staged) Save(ctx context.Context, q Querier) (File, error) {
	row := q.QueryRow(ctx, `
		INSERT INTO files (org_id, sha256, mime, size_bytes, original_name)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (org_id, sha256) DO NOTHING
		RETURNING `+fileColumns,
		s.OrgID, s.SHA256, s.MIME, s.Size, s.Name,
	)
	created, err := scanFile(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return GetFileByHash(ctx, q, s.OrgID, s.SHA256)
	}
	return created, err
}

// Commit moves the staged bytes into place under their content key (a
// no-op if that blob already exists).
func (s *Staged) Commit(ctx context.Context) error {
	return s.pending.Commit(ctx, storage.Key{OrgID: s.OrgID, SHA256: s.SHA256})
}

// Abort discards the staged bytes.
func (s *Staged) Abort() error { return s.pending.Abort() }

// SaveFile stages r, records it with q and commits the blob: StageFile,
// Save and Commit in one call, for callers whose q is not a long-lived
// transaction (tests, imports).
func SaveFile(ctx context.Context, q Querier, store storage.Store, orgID int64, name string, r io.Reader, pol UploadPolicy) (File, error) {
	staged, err := StageFile(ctx, store, orgID, name, r, pol)
	if err != nil {
		return File{}, err
	}
	f, err := staged.Save(ctx, q)
	if err != nil {
		_ = staged.Abort()
		return File{}, err
	}
	return f, staged.Commit(ctx)
}

// OpenFile opens f's bytes from store.
func OpenFile(ctx context.Context, store storage.Store, f File) (storage.Blob, error) {
	return store.Open(ctx, f.Key())
}

// GetFileByHash looks up a file by its content hash, scoped to orgID. It
// returns ErrNotFound if no such file exists in that org.
func GetFileByHash(ctx context.Context, q Querier, orgID int64, sha256 []byte) (File, error) {
	row := q.QueryRow(ctx, `SELECT `+fileColumns+` FROM files WHERE org_id = $1 AND sha256 = $2`, orgID, sha256)
	f, err := scanFile(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return File{}, ErrNotFound
	}
	return f, err
}

// GetFile looks up a file by id, scoped to orgID. It returns
// ErrNotFound if no such file exists in that org - including when the
// id belongs to another org.
func GetFile(ctx context.Context, q Querier, orgID, id int64) (File, error) {
	row := q.QueryRow(ctx, `SELECT `+fileColumns+` FROM files WHERE org_id = $1 AND id = $2`, orgID, id)
	f, err := scanFile(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return File{}, ErrNotFound
	}
	return f, err
}
