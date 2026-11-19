package module

import (
	"bytes"
	"context"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"shigoto/internal/core"
	"shigoto/internal/storage"
)

func storedFile(t *testing.T, mime, name string, data []byte) (storage.Store, core.File) {
	t.Helper()
	s := storage.NewDisk(t.TempDir())
	sum := sha256.Sum256(data)
	p, err := s.Begin(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = p.Write(data)
	if err := p.Commit(context.Background(), storage.Key{OrgID: 1, SHA256: sum[:]}); err != nil {
		t.Fatal(err)
	}
	return s, core.File{ID: 9, OrgID: 1, SHA256: sum[:], MIME: mime, SizeBytes: int64(len(data)), OriginalName: name, CreatedAt: time.Now()}
}

func checkFileHeaders(t *testing.T, h http.Header) {
	t.Helper()
	if h.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("nosniff missing: %v", h)
	}
	if h.Get("Content-Security-Policy") != "sandbox" {
		t.Errorf("CSP = %q", h.Get("Content-Security-Policy"))
	}
}

func TestServeInlineRange(t *testing.T) {
	data := bytes.Repeat([]byte{1, 2, 3, 4}, 100)
	store, f := storedFile(t, "video/mp4", "leak.mp4", data)
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Range", "bytes=0-1")
	rec := httptest.NewRecorder()

	ServeFile(rec, req, store, f, true)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Range"); got != "bytes 0-1/"+strconv.Itoa(len(data)) {
		t.Fatalf("Content-Range = %q", got)
	}
	if got := rec.Header().Get("Content-Disposition"); got != `inline; filename="leak.mp4"` {
		t.Fatalf("Content-Disposition = %q", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "video/mp4" {
		t.Fatalf("Content-Type = %q", got)
	}
	if !bytes.Equal(rec.Body.Bytes(), []byte{1, 2}) {
		t.Fatalf("body = %v", rec.Body.Bytes())
	}
	checkFileHeaders(t, rec.Header())
}

func TestServeInlineIgnoredForPDF(t *testing.T) {
	store, f := storedFile(t, "application/pdf", "policy.pdf", []byte("%PDF-1.7"))
	rec := httptest.NewRecorder()
	ServeFile(rec, httptest.NewRequest(http.MethodGet, "/x", nil), store, f, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="policy.pdf"` {
		t.Fatalf("Content-Disposition = %q", got)
	}
	checkFileHeaders(t, rec.Header())
}

func TestServeAttachmentDefault(t *testing.T) {
	store, f := storedFile(t, "image/png", "r.png", []byte("png-bytes"))
	rec := httptest.NewRecorder()
	ServeFile(rec, httptest.NewRequest(http.MethodGet, "/x", nil), store, f, false)
	if rec.Code != http.StatusOK || rec.Body.String() != "png-bytes" {
		t.Fatalf("status %d body %q", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="r.png"` {
		t.Fatalf("Content-Disposition = %q", got)
	}
	checkFileHeaders(t, rec.Header())
}
