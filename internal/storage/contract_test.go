package storage_test

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"

	"shigoto/internal/storage"
)

func readAll(t *testing.T, s storage.Store, k storage.Key) (string, int64) {
	t.Helper()
	b, err := s.Open(context.Background(), k)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer b.Close()
	got, err := io.ReadAll(b)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(got), b.Size()
}

// runContract runs every backend-agnostic Store case against stores from
// newStore. Each subtest gets a fresh store.
func runContract(t *testing.T, newStore func(t *testing.T) storage.Store) {
	ctx := context.Background()

	t.Run("RoundTrip", func(t *testing.T) {
		s := newStore(t)
		k := key(1, "hello")
		put(t, s, k, "hello")
		if got, size := readAll(t, s, k); got != "hello" || size != 5 {
			t.Fatalf("got %q size %d", got, size)
		}
	})

	t.Run("CommitExistingKeepsOriginal", func(t *testing.T) {
		s := newStore(t)
		k := key(1, "same")
		put(t, s, k, "same")
		put(t, s, k, "same")
		if got, _ := readAll(t, s, k); got != "same" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("AbortThenOpenMissing", func(t *testing.T) {
		s := newStore(t)
		k := key(1, "partial")
		p, err := s.Begin(ctx, 1)
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
		_, _ = io.WriteString(p, "partial")
		if err := p.Abort(); err != nil {
			t.Fatalf("Abort: %v", err)
		}
		if err := p.Abort(); err != nil {
			t.Fatalf("second Abort: %v", err)
		}
		if _, err := s.Open(ctx, k); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("Open err = %v, want ErrNotFound", err)
		}
	})

	t.Run("OpenMissing", func(t *testing.T) {
		s := newStore(t)
		if _, err := s.Open(ctx, key(1, "nope")); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("DeleteMissingIsNil", func(t *testing.T) {
		s := newStore(t)
		if err := s.Delete(ctx, key(1, "nope")); err != nil {
			t.Fatalf("Delete: %v", err)
		}
	})

	t.Run("DeleteThenOpenMissing", func(t *testing.T) {
		s := newStore(t)
		k := key(2, "gone")
		put(t, s, k, "gone")
		if err := s.Delete(ctx, k); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if _, err := s.Open(ctx, k); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("SeekAndRead", func(t *testing.T) {
		s := newStore(t)
		k := key(1, "hello world")
		put(t, s, k, "hello world")
		b, err := s.Open(ctx, k)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer b.Close()
		end, err := b.Seek(0, io.SeekEnd)
		if err != nil || end != 11 {
			t.Fatalf("Seek end = %d, %v", end, err)
		}
		if _, err := b.Seek(6, io.SeekStart); err != nil {
			t.Fatalf("Seek: %v", err)
		}
		got, err := io.ReadAll(b)
		if err != nil || string(got) != "world" {
			t.Fatalf("got %q, %v", got, err)
		}
	})

	t.Run("ConcurrentSameKey", func(t *testing.T) {
		s := newStore(t)
		k := key(1, "racy")
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i := range errs {
			wg.Add(1)
			go func() {
				defer wg.Done()
				p, err := s.Begin(ctx, 1)
				if err != nil {
					errs[i] = err
					return
				}
				if _, err := io.WriteString(p, "racy"); err != nil {
					errs[i] = err
					_ = p.Abort()
					return
				}
				errs[i] = p.Commit(ctx, k)
			}()
		}
		wg.Wait()
		for i, err := range errs {
			if err != nil {
				t.Fatalf("commit %d: %v", i, err)
			}
		}
		if got, _ := readAll(t, s, k); got != "racy" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("OrgIsolation", func(t *testing.T) {
		s := newStore(t)
		k := key(1, "mine")
		put(t, s, k, "mine")
		other := storage.Key{OrgID: 2, SHA256: k.SHA256}
		if _, err := s.Open(ctx, other); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})
}
