package storage

import "github.com/aws/aws-sdk-go-v2/service/s3"

// SetCopyLimits overrides the multipart copy threshold and part size for a
// test, returning a func that restores the previous values.
func SetCopyLimits(threshold, partSize int64) (restore func()) {
	oldT, oldP := copyMultipartThreshold, copyPartSize
	copyMultipartThreshold, copyPartSize = threshold, partSize
	return func() { copyMultipartThreshold, copyPartSize = oldT, oldP }
}

// SetUploadPartSize overrides the multipart upload part size for a test.
func SetUploadPartSize(n int64) (restore func()) {
	old := uploadPartSize
	uploadPartSize = n
	return func() { uploadPartSize = old }
}

// RawClient exposes the underlying S3 client so tests can inspect the bucket.
func (s *S3) RawClient() *s3.Client { return s.client }

// BucketName returns the configured bucket.
func (s *S3) BucketName() string { return s.bucket }

// KeyPrefix returns the configured key prefix.
func (s *S3) KeyPrefix() string { return s.prefix }
