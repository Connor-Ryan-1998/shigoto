//go:build integration

package storage_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"shigoto/internal/storage"
)

const testBucket = "shigoto-test"

var unsafeName = regexp.MustCompile(`[^a-zA-Z0-9-]+`)

func randomHex(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

func testS3Config(t *testing.T, bucket string) storage.S3Config {
	t.Helper()
	endpoint := os.Getenv("SHIGOTO_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("SHIGOTO_TEST_S3_ENDPOINT not set")
	}
	return storage.S3Config{
		Endpoint:        endpoint,
		Region:          "auto",
		Bucket:          bucket,
		AccessKeyID:     "test",
		SecretAccessKey: "test",
		PathStyle:       true,
		Prefix:          strings.Trim(unsafeName.ReplaceAllString(t.Name(), "-"), "-") + "-" + randomHex(t, 4) + "/",
	}
}

// newTestS3 returns an S3 store on the shared test bucket under a prefix
// unique to t, so tests never see each other's objects.
func newTestS3(t *testing.T) *storage.S3 {
	t.Helper()
	return newTestS3With(t, testS3Config(t, testBucket))
}

func newTestS3With(t *testing.T, cfg storage.S3Config) *storage.S3 {
	t.Helper()
	s, err := storage.NewS3(cfg)
	if err != nil {
		t.Fatalf("NewS3: %v", err)
	}
	if err := s.EnsureBucket(context.Background()); err != nil {
		t.Fatalf("EnsureBucket: %v", err)
	}
	return s
}

func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

func keyFor(org int64, data []byte) storage.Key {
	sum := sha256.Sum256(data)
	return storage.Key{OrgID: org, SHA256: sum[:]}
}

type beginner interface {
	Begin(ctx context.Context, orgID int64) (storage.Pending, error)
}

func commitBytes(t *testing.T, s beginner, k storage.Key, data []byte) {
	t.Helper()
	p, err := s.Begin(context.Background(), k.OrgID)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, err := io.Copy(p, bytes.NewReader(data)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := p.Commit(context.Background(), k); err != nil {
		t.Fatalf("Commit: %v", err)
	}
}

// stagingLeft lists staging objects and in-progress multipart uploads
// under s's staging prefix.
func stagingLeft(t *testing.T, s *storage.S3) []string {
	t.Helper()
	ctx := context.Background()
	c, bucket, prefix := s.RawClient(), s.BucketName(), s.KeyPrefix()+"staging/"
	var left []string
	objs, err := c.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: &bucket, Prefix: &prefix})
	if err != nil {
		t.Fatalf("ListObjectsV2: %v", err)
	}
	for _, o := range objs.Contents {
		left = append(left, "object "+aws.ToString(o.Key))
	}
	ups, err := c.ListMultipartUploads(ctx, &s3.ListMultipartUploadsInput{Bucket: &bucket, Prefix: &prefix})
	if err != nil {
		t.Fatalf("ListMultipartUploads: %v", err)
	}
	for _, u := range ups.Uploads {
		left = append(left, "upload "+aws.ToString(u.Key))
	}
	return left
}

func rawObject(t *testing.T, s *storage.S3, k storage.Key) []byte {
	t.Helper()
	bucket := s.BucketName()
	objKey := fmt.Sprintf("%s%d/%s", s.KeyPrefix(), k.OrgID, hex.EncodeToString(k.SHA256))
	out, err := s.RawClient().GetObject(context.Background(), &s3.GetObjectInput{Bucket: &bucket, Key: &objKey})
	if err != nil {
		t.Fatalf("GetObject %s: %v", objKey, err)
	}
	defer out.Body.Close()
	data, err := io.ReadAll(out.Body)
	if err != nil {
		t.Fatalf("read %s: %v", objKey, err)
	}
	return data
}

func TestS3EnsureBucketIdempotent(t *testing.T) {
	cfg := testS3Config(t, "shigoto-ensure-"+randomHex(t, 4))
	s, err := storage.NewS3(cfg)
	if err != nil {
		t.Fatalf("NewS3: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := s.EnsureBucket(context.Background()); err != nil {
			t.Fatalf("EnsureBucket #%d: %v", i+1, err)
		}
	}
	t.Cleanup(func() {
		_, _ = s.RawClient().DeleteBucket(context.Background(), &s3.DeleteBucketInput{Bucket: &cfg.Bucket})
	})
}

func TestS3AbortLeavesNothing(t *testing.T) {
	defer storage.SetUploadPartSize(5 << 20)()
	s := newTestS3(t)
	p, err := s.Begin(context.Background(), 1)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, err := p.Write(randomBytes(t, 6<<20)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := p.Abort(); err != nil {
		t.Fatalf("Abort: %v", err)
	}
	if left := stagingLeft(t, s); len(left) != 0 {
		t.Fatalf("staging left after Abort: %v", left)
	}
}

func TestS3CommitLeavesNoStaging(t *testing.T) {
	defer storage.SetUploadPartSize(5 << 20)()
	for _, size := range []int{1 << 10, 6 << 20} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			s := newTestS3(t)
			data := randomBytes(t, size)
			k := keyFor(1, data)
			commitBytes(t, s, k, data)
			if got := rawObject(t, s, k); !bytes.Equal(got, data) {
				t.Fatalf("stored %d bytes, want %d matching bytes", len(got), len(data))
			}
			if left := stagingLeft(t, s); len(left) != 0 {
				t.Fatalf("staging left after Commit: %v", left)
			}
		})
	}
}

func TestS3MultipartCopy(t *testing.T) {
	defer storage.SetUploadPartSize(5 << 20)()
	defer storage.SetCopyLimits(5<<20, 5<<20)()
	s := newTestS3(t)
	data := randomBytes(t, 12<<20)
	k := keyFor(1, data)
	commitBytes(t, s, k, data)
	if got := sha256.Sum256(rawObject(t, s, k)); !bytes.Equal(got[:], k.SHA256) {
		t.Fatal("copied object does not match uploaded bytes")
	}
	if left := stagingLeft(t, s); len(left) != 0 {
		t.Fatalf("staging left after Commit: %v", left)
	}
}

func TestS3CommitExistingDuringMultipartLeavesNothing(t *testing.T) {
	defer storage.SetUploadPartSize(5 << 20)()
	s := newTestS3(t)
	data := randomBytes(t, 6<<20)
	k := keyFor(1, data)
	commitBytes(t, s, k, data)
	commitBytes(t, s, k, data)
	if left := stagingLeft(t, s); len(left) != 0 {
		t.Fatalf("staging left after duplicate Commit: %v", left)
	}
}

func TestS3FreeBytesUnknown(t *testing.T) {
	s := newTestS3(t)
	if free, err := s.FreeBytes(context.Background()); err != nil || free != -1 {
		t.Fatalf("FreeBytes = %d, %v; want -1, nil", free, err)
	}
}

// getCounter counts object GET requests and records their Range headers.
type getCounter struct {
	base   http.RoundTripper
	mu     sync.Mutex
	ranges []string
}

func (g *getCounter) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method == http.MethodGet && !strings.Contains(r.URL.RawQuery, "list-type") && !strings.Contains(r.URL.RawQuery, "uploads") {
		g.mu.Lock()
		g.ranges = append(g.ranges, r.Header.Get("Range"))
		g.mu.Unlock()
	}
	return g.base.RoundTrip(r)
}

func (g *getCounter) gets() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Clone(g.ranges)
}

func newCountingS3(t *testing.T) (*storage.S3, *getCounter) {
	t.Helper()
	g := &getCounter{base: http.DefaultTransport}
	cfg := testS3Config(t, testBucket)
	cfg.HTTPClient = &http.Client{Transport: g}
	return newTestS3With(t, cfg), g
}

func openHelloWorld(t *testing.T) (storage.Blob, *getCounter) {
	t.Helper()
	s, g := newCountingS3(t)
	data := []byte("hello world")
	k := keyFor(1, data)
	commitBytes(t, s, k, data)
	b, err := s.Open(context.Background(), k)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { b.Close() })
	return b, g
}

func TestS3Contract(t *testing.T) {
	runContract(t, func(t *testing.T) storage.Store { return newTestS3(t) })
}

func TestS3BlobSeekProbeNoGet(t *testing.T) {
	b, g := openHelloWorld(t)
	if end, err := b.Seek(0, io.SeekEnd); err != nil || end != 11 {
		t.Fatalf("Seek end = %d, %v", end, err)
	}
	if start, err := b.Seek(0, io.SeekStart); err != nil || start != 0 {
		t.Fatalf("Seek start = %d, %v", start, err)
	}
	if b.Size() != 11 {
		t.Fatalf("Size = %d", b.Size())
	}
	if got := g.gets(); len(got) != 0 {
		t.Fatalf("size probe issued GETs: %v", got)
	}
}

func TestS3BlobReadAndReseek(t *testing.T) {
	b, g := openHelloWorld(t)
	got, err := io.ReadAll(b)
	if err != nil || string(got) != "hello world" {
		t.Fatalf("ReadAll = %q, %v", got, err)
	}
	if n := len(g.gets()); n != 1 {
		t.Fatalf("ReadAll issued %d GETs, want 1", n)
	}
	if _, err := b.Seek(6, io.SeekStart); err != nil {
		t.Fatalf("Seek: %v", err)
	}
	buf := make([]byte, 5)
	if _, err := io.ReadFull(b, buf); err != nil || string(buf) != "world" {
		t.Fatalf("read after seek = %q, %v", buf, err)
	}
	gets := g.gets()
	if len(gets) != 2 || gets[1] != "bytes=6-" {
		t.Fatalf("GET ranges = %q, want second to be bytes=6-", gets)
	}
}

func TestS3BlobSeekPastEndEOF(t *testing.T) {
	b, g := openHelloWorld(t)
	if _, err := b.Seek(16, io.SeekStart); err != nil {
		t.Fatalf("Seek past end: %v", err)
	}
	if n, err := b.Read(make([]byte, 4)); n != 0 || err != io.EOF {
		t.Fatalf("Read past end = %d, %v; want 0, EOF", n, err)
	}
	if got := g.gets(); len(got) != 0 {
		t.Fatalf("read past end issued GETs: %v", got)
	}
}

func TestS3BlobNegativeSeekErrors(t *testing.T) {
	b, _ := openHelloWorld(t)
	if _, err := b.Seek(-1, io.SeekStart); err == nil {
		t.Fatal("Seek(-1, Start) succeeded")
	}
}
