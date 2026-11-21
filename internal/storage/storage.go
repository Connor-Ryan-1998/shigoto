// Package storage holds uploaded file bytes behind a small interface, so
// feature code never touches the filesystem directly. Blobs are content
// addressed by (org, SHA-256). Disk is the only implementation today; an
// S3-compatible backend can be added without changing callers.
package storage

import (
	"context"
	"errors"
	"io"
	"time"
)

// ErrNotFound is returned by Open when no blob exists for a key.
var ErrNotFound = errors.New("storage: not found")

// Key identifies a blob: the owning org and the SHA-256 of its bytes.
type Key struct {
	OrgID  int64
	SHA256 []byte
}

// Blob is an open, seekable blob (seeking supports HTTP Range requests).
type Blob interface {
	io.ReadSeekCloser
	Size() int64
}

// Pending is a blob being written. Exactly one of Commit or Abort must be
// called. Commit stores the bytes under key; if key already exists the
// existing blob is kept and the new bytes are discarded.
type Pending interface {
	io.Writer
	Commit(ctx context.Context, key Key) error
	Abort() error
}

// Store reads and writes blobs.
type Store interface {
	// Begin starts a staged write for orgID.
	Begin(ctx context.Context, orgID int64) (Pending, error)
	// Open returns the blob for key, or ErrNotFound.
	Open(ctx context.Context, key Key) (Blob, error)
	// Delete removes the blob for key; a missing blob is not an error.
	Delete(ctx context.Context, key Key) error
	// FreeBytes reports remaining capacity, or -1 when unknown/unlimited.
	FreeBytes(ctx context.Context) (int64, error)
}

// StagingPruner is implemented by stores whose abandoned staged writes
// (from a crash between Begin and Commit/Abort) need explicit clean-up.
type StagingPruner interface {
	// PruneStaging removes staged writes started before before. Committed
	// blobs are never touched.
	PruneStaging(ctx context.Context, before time.Time) error
}
