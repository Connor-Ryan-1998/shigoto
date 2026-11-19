//go:build integration

package module_test

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"testing"
	"time"

	"shigoto/internal/storage"
	"shigoto/internal/testkit"
)

// lowDisk is a disk store that reports almost no free space.
type lowDisk struct{ *storage.Disk }

func (lowDisk) FreeBytes(context.Context) (int64, error) { return 1, nil }

func TestUploadRefusedWhenDiskLow(t *testing.T) {
	e := testkit.New(t)
	h := e.Handler(testkit.Store(lowDisk{storage.NewDisk(t.TempDir())}))
	org, s := e.OwnerSession("acme", h)
	_ = org

	rr := s.Upload("/api/v1/orgs/acme/files", "receipt.png", testkit.PNG)
	if rr.Code != 507 || !bytes.Contains(rr.Body, []byte(`"insufficient storage"`)) {
		t.Fatalf("status %d body %s", rr.Code, rr.Body)
	}
	if n := e.Count("files", "true"); n != 0 {
		t.Fatalf("files rows = %d", n)
	}
}

func TestUploadDocumentPolicyStill10MiB(t *testing.T) {
	e := testkit.New(t)
	h := e.Handler()
	_, s := e.OwnerSession("acme", h)

	pdf := append([]byte("%PDF-1.7\n"), bytes.Repeat([]byte{' '}, 11<<20)...)
	rr := s.Upload("/api/v1/orgs/acme/files", "big.pdf", pdf)
	if rr.Code != 413 {
		t.Fatalf("status %d body %s", rr.Code, rr.Body)
	}
	if n := e.Count("files", "true"); n != 0 {
		t.Fatalf("files rows = %d", n)
	}

	ok := s.Upload("/api/v1/orgs/acme/files", "small.pdf", []byte("%PDF-1.7\nsmall"))
	if ok.Code != 201 {
		t.Fatalf("small upload status %d body %s", ok.Code, ok.Body)
	}
}

func TestUploadIgnoresOtherPartsBeforeFile(t *testing.T) {
	e := testkit.New(t)
	h := e.Handler()
	_, s := e.OwnerSession("acme", h)
	rr := s.UploadForm("/api/v1/orgs/acme/files", "r.png", testkit.PNG, map[string]string{"note": "x"})
	if rr.Code != 201 {
		t.Fatalf("status %d body %s", rr.Code, rr.Body)
	}
}

// TestUploadHoldsNoConnectionWhileStreaming: a slow client must not pin a
// pool connection (or an open transaction) for the length of its upload.
func TestUploadHoldsNoConnectionWhileStreaming(t *testing.T) {
	e := testkit.New(t)
	h := e.Handler()
	_, s := e.OwnerSession("acme", h)

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	done := make(chan testkit.Response, 1)
	go func() {
		done <- s.DoWith("POST", "/api/v1/orgs/acme/files", pr, map[string]string{"Content-Type": mw.FormDataContentType()})
	}()
	part, _ := mw.CreateFormFile("file", "slow.png")
	if _, err := part.Write(testkit.PNG); err != nil {
		t.Fatal(err)
	}
	// The handler has read the sniff bytes and is blocked waiting for more.
	time.Sleep(200 * time.Millisecond)
	if n := e.Pool.Stat().AcquiredConns(); n != 0 {
		t.Errorf("acquired connections while the body is still streaming = %d, want 0", n)
	}
	_, _ = part.Write(bytes.Repeat([]byte{0}, 1024))
	_ = mw.Close()
	_ = pw.Close()
	if rr := <-done; rr.Code != 201 {
		t.Fatalf("status %d %s", rr.Code, rr.Body)
	}
}
