package storage_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shigoto/internal/storage"
)

func key(org int64, content string) storage.Key {
	sum := sha256.Sum256([]byte(content))
	return storage.Key{OrgID: org, SHA256: sum[:]}
}

func put(t *testing.T, s storage.Store, k storage.Key, content string) {
	t.Helper()
	p, err := s.Begin(context.Background(), k.OrgID)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, err := io.WriteString(p, content); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := p.Commit(context.Background(), k); err != nil {
		t.Fatalf("Commit: %v", err)
	}
}

func tempFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var out []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".upload-") {
			out = append(out, e.Name())
		}
	}
	return out
}

func TestDiskRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := storage.NewDisk(dir)
	k := key(1, "hello")
	put(t, s, k, "hello")

	b, err := s.Open(context.Background(), k)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer b.Close()
	got, _ := io.ReadAll(b)
	if string(got) != "hello" || b.Size() != 5 {
		t.Fatalf("got %q size %d", got, b.Size())
	}
	info, err := os.Stat(filepath.Join(dir, "1", hex.EncodeToString(k.SHA256)))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode().Perm())
	}
}

func TestDiskCommitExistingKeepsOriginal(t *testing.T) {
	dir := t.TempDir()
	s := storage.NewDisk(dir)
	k := key(1, "same")
	put(t, s, k, "same")
	put(t, s, k, "same")
	if left := tempFiles(t, filepath.Join(dir, "1")); len(left) != 0 {
		t.Fatalf("temp files left: %v", left)
	}
}

func TestDiskAbortRemovesTemp(t *testing.T) {
	dir := t.TempDir()
	s := storage.NewDisk(dir)
	p, err := s.Begin(context.Background(), 1)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	_, _ = io.WriteString(p, "partial")
	if err := p.Abort(); err != nil {
		t.Fatalf("Abort: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "1"))
	if len(entries) != 0 {
		t.Fatalf("files left: %v", entries)
	}
}

func TestDiskOpenMissing(t *testing.T) {
	s := storage.NewDisk(t.TempDir())
	_, err := s.Open(context.Background(), key(1, "nope"))
	if !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestDiskDeleteMissingIsNil(t *testing.T) {
	s := storage.NewDisk(t.TempDir())
	if err := s.Delete(context.Background(), key(1, "nope")); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

func TestDiskDeleteRemovesBlob(t *testing.T) {
	s := storage.NewDisk(t.TempDir())
	k := key(2, "gone")
	put(t, s, k, "gone")
	if err := s.Delete(context.Background(), k); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Open(context.Background(), k); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestDiskFreeBytesPositive(t *testing.T) {
	s := storage.NewDisk(filepath.Join(t.TempDir(), "not-yet"))
	free, err := s.FreeBytes(context.Background())
	if err != nil {
		t.Fatalf("FreeBytes: %v", err)
	}
	if free <= 0 {
		t.Fatalf("free = %d", free)
	}
}
