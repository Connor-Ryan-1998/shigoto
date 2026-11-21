package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"shigoto/internal/config"
	"shigoto/internal/storage"
)

var errBoom = errors.New("boom")

func TestRetryForSucceedsAfterFailures(t *testing.T) {
	calls := 0
	err := retryFor(context.Background(), time.Second, time.Millisecond, func(context.Context) error {
		calls++
		if calls < 3 {
			return errBoom
		}
		return nil
	})
	if err != nil || calls != 3 {
		t.Fatalf("retryFor = %v after %d calls; want nil after 3", err, calls)
	}
}

func TestRetryForTimesOut(t *testing.T) {
	start := time.Now()
	err := retryFor(context.Background(), 50*time.Millisecond, 10*time.Millisecond, func(context.Context) error { return errBoom })
	if !errors.Is(err, errBoom) {
		t.Fatalf("err = %v, want it to wrap errBoom", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("gave up after %v, want ~50ms", elapsed)
	}
}

func TestRetryForHonoursContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	err := retryFor(ctx, time.Minute, time.Second, func(context.Context) error { return errBoom })
	if err == nil {
		t.Fatal("retryFor succeeded with a cancelled context")
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("returned after %v, want prompt return", elapsed)
	}
}

func TestOpenStoreDisk(t *testing.T) {
	s, err := openStore(context.Background(), config.Config{Storage: "disk", FilesDir: t.TempDir()})
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	if _, ok := s.(*storage.Disk); !ok {
		t.Fatalf("store = %T, want *storage.Disk", s)
	}
}
