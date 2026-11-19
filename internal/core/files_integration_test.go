//go:build integration

package core_test

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"shigoto/internal/core"
	"shigoto/internal/storage"
	"shigoto/internal/testkit"
)

// mp4Header is an ISO-BMFF ftyp box with the mp42 brand.
var mp4Header = append([]byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p', 'm', 'p', '4', '2'}, make([]byte, 16)...)

func mp4Reader(total int64) io.Reader {
	return io.MultiReader(bytes.NewReader(mp4Header), io.LimitReader(zeroReader{}, total-int64(len(mp4Header))))
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

// failingReader fails the test if anything reads it.
type failingReader struct{ t *testing.T }

func (f failingReader) Read([]byte) (int, error) {
	f.t.Fatal("body read after a rejecting sniff")
	return 0, io.EOF
}

func uploadTemps(t *testing.T, dir string, orgID int64) []string {
	t.Helper()
	entries, _ := os.ReadDir(filepath.Join(dir, strconv.FormatInt(orgID, 10)))
	var out []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".upload-") {
			out = append(out, e.Name())
		}
	}
	return out
}

func TestSaveFileStreamsAndDedupes(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	store := storage.NewDisk(t.TempDir())
	data := append(append([]byte{}, testkit.PNG...), bytes.Repeat([]byte{7}, 3<<20)...)

	a, err := core.SaveFile(e.Ctx, e.Pool, store, org.ID, "a.png", bytes.NewReader(data), core.MediaPolicy(0))
	if err != nil {
		t.Fatalf("SaveFile: %v", err)
	}
	b, err := core.SaveFile(e.Ctx, e.Pool, store, org.ID, "b.png", bytes.NewReader(data), core.MediaPolicy(0))
	if err != nil {
		t.Fatalf("SaveFile: %v", err)
	}
	if a.ID != b.ID || a.SizeBytes != int64(len(data)) || a.MIME != "image/png" {
		t.Fatalf("a=%+v b=%+v", a, b)
	}
	if n := e.Count("files", "org_id = $1", org.ID); n != 1 {
		t.Fatalf("files rows = %d", n)
	}
	blob, err := core.OpenFile(e.Ctx, store, a)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer blob.Close()
	got, _ := io.ReadAll(blob)
	if !bytes.Equal(got, data) {
		t.Fatal("stored bytes differ")
	}
}

func TestSaveFileRejectsBeforeReadingBody(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	r := io.MultiReader(bytes.NewReader(bytes.Repeat([]byte("a"), 512)), failingReader{t})
	_, err := core.SaveFile(e.Ctx, e.Pool, storage.NewDisk(t.TempDir()), org.ID, "x.jpg", r, core.DocumentPolicy)
	if !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("err = %v", err)
	}
}

func TestSaveFileTooLargeLeavesNoTemp(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	dir := t.TempDir()
	_, err := core.SaveFile(e.Ctx, e.Pool, storage.NewDisk(dir), org.ID, "v.mp4", mp4Reader(2<<20), core.MediaPolicy(1<<20))
	if !errors.Is(err, core.ErrTooLarge) {
		t.Fatalf("err = %v", err)
	}
	if left := uploadTemps(t, dir, org.ID); len(left) != 0 {
		t.Fatalf("temp files left: %v", left)
	}
	if n := e.Count("files", "org_id = $1", org.ID); n != 0 {
		t.Fatalf("files rows = %d", n)
	}
}

func TestSaveFileExactlyAtLimit(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	f, err := core.SaveFile(e.Ctx, e.Pool, storage.NewDisk(t.TempDir()), org.ID, "v.mp4", mp4Reader(1<<20), core.MediaPolicy(1<<20))
	if err != nil {
		t.Fatalf("SaveFile: %v", err)
	}
	if f.SizeBytes != 1<<20 || f.MIME != "video/mp4" {
		t.Fatalf("f = %+v", f)
	}
}

func TestSaveFileConcurrentDedupe(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	store := storage.NewDisk(t.TempDir())
	var wg sync.WaitGroup
	ids := make([]int64, 2)
	errs := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tx, err := e.Pool.Begin(e.Ctx)
			if err != nil {
				errs[i] = err
				return
			}
			f, err := core.SaveFile(e.Ctx, tx, store, org.ID, "r.png", bytes.NewReader(testkit.PNG), core.DocumentPolicy)
			if err != nil {
				_ = tx.Rollback(e.Ctx)
				errs[i] = err
				return
			}
			ids[i] = f.ID
			errs[i] = tx.Commit(e.Ctx)
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatalf("err: %v", err)
		}
	}
	if n := e.Count("files", "org_id = $1", org.ID); n != 1 {
		t.Fatalf("files rows = %d (ids %v)", n, ids)
	}
}

func TestSaveFileLargeSize(t *testing.T) {
	if testing.Short() {
		t.Skip("writes 3 GiB")
	}
	e := testkit.New(t)
	org := e.Org("acme")
	const size = int64(3) << 30
	f, err := core.SaveFile(e.Ctx, e.Pool, storage.NewDisk(t.TempDir()), org.ID, "big.mp4", mp4Reader(size), core.MediaPolicy(0))
	if err != nil {
		t.Fatalf("SaveFile: %v", err)
	}
	if f.SizeBytes != size {
		t.Fatalf("size = %d", f.SizeBytes)
	}
}
