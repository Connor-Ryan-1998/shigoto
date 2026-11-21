package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"shigoto/internal/config"
	"shigoto/internal/storage"
)

// bucketWait is how long serve waits for the S3 bucket to answer at
// startup. Container restarts after a backup (plain docker start) and host
// reboots don't honour Compose's depends_on health condition, so the app
// can start a few seconds before SeaweedFS is ready.
const (
	bucketWait     = 60 * time.Second
	bucketInterval = 2 * time.Second
)

// openStore builds the file store selected by cfg.Storage. For S3 it waits
// up to bucketWait for the bucket to be reachable (creating it if missing).
func openStore(ctx context.Context, cfg config.Config) (storage.Store, error) {
	if cfg.Storage != "s3" {
		return storage.NewDisk(cfg.FilesDir), nil
	}
	s, err := storage.NewS3(storage.S3Config{
		Endpoint:        cfg.S3.Endpoint,
		Region:          cfg.S3.Region,
		Bucket:          cfg.S3.Bucket,
		AccessKeyID:     cfg.S3.AccessKeyID,
		SecretAccessKey: cfg.S3.SecretAccessKey,
		Prefix:          cfg.S3.Prefix,
		PathStyle:       cfg.S3.PathStyle,
	})
	if err != nil {
		return nil, err
	}
	if err := retryFor(ctx, bucketWait, bucketInterval, s.EnsureBucket); err != nil {
		return nil, err
	}
	return s, nil
}

// retryFor calls fn every interval until it succeeds, timeout elapses or
// ctx ends, returning the last error wrapped.
func retryFor(ctx context.Context, timeout, every time.Duration, fn func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		err := fn(ctx)
		if err == nil {
			return nil
		}
		slog.Warn("file store not ready, retrying", "error", err)
		select {
		case <-ctx.Done():
			return fmt.Errorf("file store not ready after %s: %w", timeout, err)
		case <-time.After(every):
		}
	}
}
