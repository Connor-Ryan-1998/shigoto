package storage

import (
	"context"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

// Disk stores blobs on the local filesystem at <dir>/<org id>/<hex sha256>,
// written atomically (temp file in the same directory, then rename) with
// mode 0600 under 0700 directories.
type Disk struct {
	dir string
}

// NewDisk returns a Disk store rooted at dir.
func NewDisk(dir string) *Disk { return &Disk{dir: dir} }

func (d *Disk) orgDir(orgID int64) string {
	return filepath.Join(d.dir, strconv.FormatInt(orgID, 10))
}

func (d *Disk) path(k Key) string {
	return filepath.Join(d.orgDir(k.OrgID), hex.EncodeToString(k.SHA256))
}

// Begin creates a temp file in the org's directory.
func (d *Disk) Begin(_ context.Context, orgID int64) (Pending, error) {
	dir := d.orgDir(orgID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		return nil, err
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return nil, err
	}
	return &diskPending{d: d, f: f}, nil
}

type diskPending struct {
	d    *Disk
	f    *os.File
	done bool
}

func (p *diskPending) Write(b []byte) (int, error) { return p.f.Write(b) }

func (p *diskPending) Commit(_ context.Context, k Key) error {
	if p.done {
		return errors.New("storage: pending already finished")
	}
	p.done = true
	name := p.f.Name()
	if err := p.f.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	dest := p.d.path(k)
	if _, err := os.Stat(dest); err == nil {
		return os.Remove(name)
	}
	if err := os.Rename(name, dest); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}

func (p *diskPending) Abort() error {
	if p.done {
		return nil
	}
	p.done = true
	name := p.f.Name()
	_ = p.f.Close()
	if err := os.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

type diskBlob struct {
	*os.File
	size int64
}

func (b diskBlob) Size() int64 { return b.size }

// Open opens the blob for k.
func (d *Disk) Open(_ context.Context, k Key) (Blob, error) {
	f, err := os.Open(d.path(k))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return diskBlob{File: f, size: info.Size()}, nil
}

// Delete removes the blob for k.
func (d *Disk) Delete(_ context.Context, k Key) error {
	if err := os.Remove(d.path(k)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// FreeBytes reports bytes available to unprivileged users on the volume
// holding the store's directory (created if missing).
func (d *Disk) FreeBytes(_ context.Context) (int64, error) {
	if err := os.MkdirAll(d.dir, 0o700); err != nil {
		return 0, err
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(d.dir, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}
