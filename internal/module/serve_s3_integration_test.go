//go:build integration

package module

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"shigoto/internal/core"
	"shigoto/internal/storage"
)

func s3StoredFile(t *testing.T, mime, name string, data []byte) (storage.Store, core.File) {
	t.Helper()
	endpoint := os.Getenv("SHIGOTO_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("SHIGOTO_TEST_S3_ENDPOINT not set")
	}
	prefix := make([]byte, 4)
	_, _ = rand.Read(prefix)
	s, err := storage.NewS3(storage.S3Config{
		Endpoint: endpoint, Region: "auto", Bucket: "shigoto-test",
		AccessKeyID: "test", SecretAccessKey: "test", PathStyle: true,
		Prefix: "module-serve-" + hex.EncodeToString(prefix) + "/",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.EnsureBucket(ctx); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	p, err := s.Begin(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = p.Write(data)
	if err := p.Commit(ctx, storage.Key{OrgID: 1, SHA256: sum[:]}); err != nil {
		t.Fatal(err)
	}
	return s, core.File{ID: 9, OrgID: 1, SHA256: sum[:], MIME: mime, SizeBytes: int64(len(data)), OriginalName: name, CreatedAt: time.Now()}
}

// TestS3ServeRange covers iOS Safari's first video request against the
// S3 backend: Range bytes=0-1 must answer 206 with exactly two bytes.
func TestS3ServeRange(t *testing.T) {
	data := make([]byte, 4096)
	_, _ = rand.Read(data)
	store, f := s3StoredFile(t, "video/mp4", "clip.mp4", data)

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Range", "bytes=0-1")
	rec := httptest.NewRecorder()
	ServeFile(rec, req, store, f, true)
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", rec.Code)
	}
	if !bytes.Equal(rec.Body.Bytes(), data[:2]) {
		t.Fatalf("body = %x, want %x", rec.Body.Bytes(), data[:2])
	}
	if got := rec.Header().Get("Content-Range"); got != "bytes 0-1/4096" {
		t.Fatalf("Content-Range = %q", got)
	}
	if got := rec.Header().Get("Content-Disposition"); got != `inline; filename="clip.mp4"` {
		t.Fatalf("Content-Disposition = %q", got)
	}
	checkFileHeaders(t, rec.Header())

	full := httptest.NewRecorder()
	ServeFile(full, httptest.NewRequest(http.MethodGet, "/x", nil), store, f, true)
	if full.Code != http.StatusOK || !bytes.Equal(full.Body.Bytes(), data) {
		t.Fatalf("full GET = %d with %d bytes, want 200 with %d", full.Code, full.Body.Len(), len(data))
	}
}
