package storage_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

func TestDiskContract(t *testing.T) {
	runContract(t, func(t *testing.T) storage.Store { return storage.NewDisk(t.TempDir()) })
}

func TestDiskFileLayoutAndMode(t *testing.T) {
	dir := t.TempDir()
	s := storage.NewDisk(dir)
	k := key(1, "hello")
	put(t, s, k, "hello")
	info, err := os.Stat(filepath.Join(dir, "1", hex.EncodeToString(k.SHA256)))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode().Perm())
	}
}

func TestDiskCommitExistingLeavesNoTemp(t *testing.T) {
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
