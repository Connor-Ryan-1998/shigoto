package storage

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// Tunables; tests override them through export_test.go.
var (
	// uploadPartSize is the buffered part size of a staged write. A write
	// that never fills one part is stored with a single PutObject at
	// Commit; a larger one streams as a multipart upload to a staging key.
	uploadPartSize int64 = 8 << 20
	// copyMultipartThreshold is the largest object copied with a single
	// CopyObject request (the S3 limit is 5 GiB).
	copyMultipartThreshold int64 = 5 << 30
	// copyPartSize is the part size of a multipart (UploadPartCopy) copy.
	copyPartSize int64 = 512 << 20
)

// S3Config configures an S3-compatible store.
type S3Config struct {
	Endpoint        string
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	// Prefix is prepended to every object key; empty or ending in "/".
	Prefix    string
	PathStyle bool
	// HTTPClient overrides the SDK's HTTP client (nil = SDK default).
	HTTPClient *http.Client
}

// S3 stores blobs in an S3-compatible bucket at
// <prefix><org id>/<hex sha256>. Staged writes that outgrow one part go to
// <prefix>staging/<org id>/<random> and are copied into place on Commit.
type S3 struct {
	client *s3.Client
	bucket string
	prefix string
}

// NewS3 returns an S3 store for cfg. It does no network I/O; call
// EnsureBucket to check the bucket is reachable.
func NewS3(cfg S3Config) (*S3, error) {
	if cfg.Endpoint == "" || cfg.Bucket == "" || cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		return nil, errors.New("storage: S3 endpoint, bucket and credentials are required")
	}
	if cfg.Prefix != "" && !strings.HasSuffix(cfg.Prefix, "/") {
		return nil, fmt.Errorf("storage: S3 prefix %q must end in /", cfg.Prefix)
	}
	region := cfg.Region
	if region == "" {
		region = "auto"
	}
	opts := s3.Options{
		BaseEndpoint: aws.String(cfg.Endpoint),
		Region:       region,
		Credentials:  credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		UsePathStyle: cfg.PathStyle,
		// Newer SDK defaults send checksum headers that several
		// S3-compatible stores reject; only send them when an API needs them.
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	}
	if cfg.HTTPClient != nil {
		opts.HTTPClient = cfg.HTTPClient
	}
	return &S3{client: s3.New(opts), bucket: cfg.Bucket, prefix: cfg.Prefix}, nil
}

func (s *S3) objectKey(k Key) string {
	return s.prefix + strconv.FormatInt(k.OrgID, 10) + "/" + hex.EncodeToString(k.SHA256)
}

func (s *S3) stagingKey(orgID int64) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return s.prefix + "staging/" + strconv.FormatInt(orgID, 10) + "/" + hex.EncodeToString(b[:]), nil
}

// isNotFound reports whether err is an S3 "no such key/bucket" response.
func isNotFound(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NotFound", "NoSuchKey", "NoSuchBucket":
			return true
		}
	}
	var respErr *awshttp.ResponseError
	return errors.As(err, &respErr) && respErr.HTTPStatusCode() == http.StatusNotFound
}

// EnsureBucket checks the bucket exists, creating it when the store reports
// it missing. A bucket that exists is never re-created, so restricted cloud
// credentials without create permission work.
func (s *S3) EnsureBucket(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: &s.bucket})
	if err == nil {
		return nil
	}
	if !isNotFound(err) {
		return fmt.Errorf("storage: checking bucket %q: %w", s.bucket, err)
	}
	_, err = s.client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: &s.bucket})
	var owned *types.BucketAlreadyOwnedByYou
	if err != nil && !errors.As(err, &owned) {
		return fmt.Errorf("storage: creating bucket %q: %w", s.bucket, err)
	}
	return nil
}

func (s *S3) exists(ctx context.Context, key string) (bool, error) {
	_, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: &s.bucket, Key: &key})
	if err == nil {
		return true, nil
	}
	if isNotFound(err) {
		return false, nil
	}
	return false, err
}

// Begin starts a staged write. Bytes are buffered up to one part; only
// when that fills does a multipart upload to a staging key begin.
func (s *S3) Begin(ctx context.Context, orgID int64) (Pending, error) {
	return &s3Pending{s: s, ctx: ctx, orgID: orgID, partSize: uploadPartSize}, nil
}

type s3Pending struct {
	s        *S3
	ctx      context.Context
	orgID    int64
	partSize int64
	buf      bytes.Buffer
	staging  string // set once a multipart upload has started
	uploadID string
	parts    []types.CompletedPart
	size     int64
	done     bool
}

func (p *s3Pending) Write(b []byte) (int, error) {
	if p.done {
		return 0, errors.New("storage: pending already finished")
	}
	written := 0
	for len(b) > 0 {
		room := int(p.partSize) - p.buf.Len()
		n := min(room, len(b))
		p.buf.Write(b[:n])
		b = b[n:]
		written += n
		if int64(p.buf.Len()) == p.partSize {
			if err := p.flushPart(); err != nil {
				return written, err
			}
		}
	}
	return written, nil
}

// flushPart uploads the buffered bytes as the next multipart part,
// starting the multipart upload on first use.
func (p *s3Pending) flushPart() error {
	if p.uploadID == "" {
		key, err := p.s.stagingKey(p.orgID)
		if err != nil {
			return err
		}
		out, err := p.s.client.CreateMultipartUpload(p.ctx, &s3.CreateMultipartUploadInput{Bucket: &p.s.bucket, Key: &key})
		if err != nil {
			return fmt.Errorf("storage: starting upload: %w", err)
		}
		p.staging, p.uploadID = key, aws.ToString(out.UploadId)
	}
	num := int32(len(p.parts) + 1)
	data := p.buf.Bytes()
	out, err := p.s.client.UploadPart(p.ctx, &s3.UploadPartInput{
		Bucket: &p.s.bucket, Key: &p.staging, UploadId: &p.uploadID,
		PartNumber: &num, Body: bytes.NewReader(data), ContentLength: aws.Int64(int64(len(data))),
	})
	if err != nil {
		return fmt.Errorf("storage: uploading part %d: %w", num, err)
	}
	p.parts = append(p.parts, types.CompletedPart{ETag: out.ETag, PartNumber: &num})
	p.size += int64(len(data))
	p.buf.Reset()
	return nil
}

func (p *s3Pending) abortUpload() {
	if p.uploadID == "" {
		return
	}
	// Background context: clean-up must run even when the request that
	// staged the write has been cancelled.
	_, _ = p.s.client.AbortMultipartUpload(context.Background(), &s3.AbortMultipartUploadInput{
		Bucket: &p.s.bucket, Key: &p.staging, UploadId: &p.uploadID,
	})
}

// Commit stores the staged bytes under k, keeping an existing blob.
func (p *s3Pending) Commit(ctx context.Context, k Key) error {
	if p.done {
		return errors.New("storage: pending already finished")
	}
	p.done = true
	dest := p.s.objectKey(k)

	exists, err := p.s.exists(ctx, dest)
	if err != nil {
		p.abortUpload()
		return fmt.Errorf("storage: checking %s: %w", dest, err)
	}
	if exists {
		p.abortUpload()
		return nil
	}

	if p.uploadID == "" {
		data := p.buf.Bytes()
		_, err := p.s.client.PutObject(ctx, &s3.PutObjectInput{
			Bucket: &p.s.bucket, Key: &dest, Body: bytes.NewReader(data), ContentLength: aws.Int64(int64(len(data))),
		})
		if err != nil {
			return fmt.Errorf("storage: storing %s: %w", dest, err)
		}
		return nil
	}

	if p.buf.Len() > 0 {
		if err := p.flushPart(); err != nil {
			p.abortUpload()
			return err
		}
	}
	_, err = p.s.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket: &p.s.bucket, Key: &p.staging, UploadId: &p.uploadID,
		MultipartUpload: &types.CompletedMultipartUpload{Parts: p.parts},
	})
	if err != nil {
		p.abortUpload()
		return fmt.Errorf("storage: completing upload: %w", err)
	}
	copyErr := p.s.copyObject(ctx, p.staging, dest, p.size)
	_, delErr := p.s.client.DeleteObject(context.Background(), &s3.DeleteObjectInput{Bucket: &p.s.bucket, Key: &p.staging})
	if copyErr != nil {
		return copyErr
	}
	if delErr != nil {
		return fmt.Errorf("storage: removing staged upload: %w", delErr)
	}
	return nil
}

// Abort discards the staged bytes.
func (p *s3Pending) Abort() error {
	if p.done {
		return nil
	}
	p.done = true
	p.buf.Reset()
	p.abortUpload()
	return nil
}

func (s *S3) copySource(key string) string {
	return url.PathEscape(s.bucket) + "/" + escapeKey(key)
}

func escapeKey(key string) string {
	segs := strings.Split(key, "/")
	for i, seg := range segs {
		segs[i] = url.PathEscape(seg)
	}
	return strings.Join(segs, "/")
}

// copyObject server-side copies src to dest, using UploadPartCopy for
// objects above copyMultipartThreshold.
func (s *S3) copyObject(ctx context.Context, src, dest string, size int64) error {
	source := s.copySource(src)
	if size <= copyMultipartThreshold {
		if _, err := s.client.CopyObject(ctx, &s3.CopyObjectInput{Bucket: &s.bucket, Key: &dest, CopySource: &source}); err != nil {
			return fmt.Errorf("storage: copying to %s: %w", dest, err)
		}
		return nil
	}
	out, err := s.client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{Bucket: &s.bucket, Key: &dest})
	if err != nil {
		return fmt.Errorf("storage: starting copy to %s: %w", dest, err)
	}
	uploadID := out.UploadId
	abort := func() {
		_, _ = s.client.AbortMultipartUpload(context.Background(), &s3.AbortMultipartUploadInput{Bucket: &s.bucket, Key: &dest, UploadId: uploadID})
	}
	var parts []types.CompletedPart
	for start := int64(0); start < size; start += copyPartSize {
		end := min(start+copyPartSize, size) - 1
		num := int32(len(parts) + 1)
		rng := fmt.Sprintf("bytes=%d-%d", start, end)
		part, err := s.client.UploadPartCopy(ctx, &s3.UploadPartCopyInput{
			Bucket: &s.bucket, Key: &dest, UploadId: uploadID, PartNumber: &num,
			CopySource: &source, CopySourceRange: &rng,
		})
		if err != nil {
			abort()
			return fmt.Errorf("storage: copying part %d to %s: %w", num, dest, err)
		}
		parts = append(parts, types.CompletedPart{ETag: part.CopyPartResult.ETag, PartNumber: &num})
	}
	if _, err := s.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket: &s.bucket, Key: &dest, UploadId: uploadID,
		MultipartUpload: &types.CompletedMultipartUpload{Parts: parts},
	}); err != nil {
		abort()
		return fmt.Errorf("storage: completing copy to %s: %w", dest, err)
	}
	return nil
}

// Delete removes the blob for k; a missing blob is not an error.
func (s *S3) Delete(ctx context.Context, k Key) error {
	key := s.objectKey(k)
	if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &s.bucket, Key: &key}); err != nil && !isNotFound(err) {
		return fmt.Errorf("storage: deleting %s: %w", key, err)
	}
	return nil
}

// FreeBytes is unknown for object storage.
func (s *S3) FreeBytes(context.Context) (int64, error) { return -1, nil }
