# File Storage Interface and Streaming Uploads Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Put file bytes behind a `storage.Store` interface (disk only), make uploads stream instead of buffering in memory, add per-purpose upload policies (including large maintenance media) and inline Range-capable media serving.

**Architecture:** New `internal/storage` package owns blob I/O; `core.SaveFile` streams through it and no longer touches the filesystem. `module.Deps` carries the `Store`. Existing receipt/insurance/depreciation uploads keep their 10 MiB PDF/JPEG/PNG policy unchanged; a new `MediaPolicy` is defined here for plan 03 to use.

**Tech Stack:** Go 1.27 stdlib, pgx/v5, Postgres migrations, Ansible (Traefik template).

**Spec:** `docs/superpowers/specs/2026-11-19-tenant-portal-rent-repairs-design.md` §1

## Global Constraints

- On-disk layout stays `<SHIGOTO_FILES_DIR>/<org_id>/<hex sha256>`, mode 0600, dirs 0700, atomic temp+rename. No data migration of existing blobs.
- Existing upload/download routes keep status codes and behaviour: 413 too large, 422 bad type, attachment downloads with `nosniff`.
- `SHIGOTO_UPLOAD_MAX_BYTES` default 1 GiB (`1<<30`), `0` = unlimited; applies to `MediaPolicy` only.
- `SHIGOTO_UPLOAD_MIN_FREE_BYTES` default 5 GiB (`5<<30`); refusal is HTTP 507 `{"error":"insufficient storage"}`.
- Media MIME allowlist: `image/jpeg`, `image/png`, `image/heic`, `image/heif`, `video/mp4`, `video/quicktime`, `video/webm`, `audio/mp4`, `audio/webm`, `audio/ogg`, `audio/mpeg`.
- Never hold a whole upload in memory. Run all validation via `uv run --locked task …` / repo Taskfile, sequentially.

## Review Focus

- A client that declares a small part but streams gigabytes: the size cap must trip mid-stream with 413 and leave no temp file behind (Task 2 test `TestSaveFileTooLargeLeavesNoTemp`).
- A disallowed file renamed to `.jpg`: rejected by content sniff before the body is read (Task 2 `TestSaveFileRejectsBeforeReadingBody`).
- Same content uploaded twice concurrently: one `files` row, one blob (Task 2 `TestSaveFileConcurrentDedupe`).
- iOS Safari video playback issues `Range: bytes=0-1` first: inline media must answer 206 (Task 4 `TestServeInlineRange`).
- Disk nearly full: upload refused with 507 before any bytes are written (Task 3 `TestUploadRefusedWhenDiskLow`).

---

### Task 1: `storage.Store` interface + disk implementation

**Files:**
- Create: `internal/storage/storage.go`, `internal/storage/disk.go`
- Test: `internal/storage/disk_test.go`

**Interfaces:**
- Produces:
  ```go
  type Key struct{ OrgID int64; SHA256 []byte }
  type Blob interface { io.ReadSeekCloser; Size() int64 }
  type Pending interface { io.Writer; Commit(ctx context.Context, key Key) error; Abort() error }
  type Store interface {
      Begin(ctx context.Context, orgID int64) (Pending, error) // staging write; Commit moves under key (no-op if key exists)
      Open(ctx context.Context, key Key) (Blob, error)          // ErrNotFound if absent
      Delete(ctx context.Context, key Key) error                // nil if absent
      FreeBytes(ctx context.Context) (int64, error)             // -1 = unknown/unlimited
  }
  var ErrNotFound = errors.New("storage: not found")
  func NewDisk(dir string) *Disk
  ```

- [ ] **Step 1: Write failing tests** in `disk_test.go` (plain `go test`, `t.TempDir()`):
  - `TestDiskRoundTrip`: Begin → write "hello" → Commit(Key{1, sha("hello")}) → Open reads "hello", `Size()==5`; file exists at `<dir>/1/<hex>` with mode `0600`.
  - `TestDiskCommitExistingKeepsOriginal`: commit same key twice; second Commit returns nil; no `.upload-*` temp left in `<dir>/1`.
  - `TestDiskAbortRemovesTemp`: Begin, write, Abort → `<dir>/1` contains no files.
  - `TestDiskOpenMissing`: `errors.Is(err, storage.ErrNotFound)`.
  - `TestDiskDeleteMissingIsNil`.
  - `TestDiskFreeBytesPositive`: `> 0`.
- [ ] **Step 2:** `go test ./internal/storage/` → FAIL (package missing).
- [ ] **Step 3: Implement** in `disk.go`. Temp file via `os.CreateTemp(<dir>/<org>, ".upload-*")`, chmod 0600, Commit = close + `os.Rename` (if dest exists, remove temp and return nil). `FreeBytes` uses `syscall.Statfs` (`Bavail*Bsize`) on `dir` (create dir first).
- [ ] **Step 4:** `go test -race ./internal/storage/` → PASS.
- [ ] **Step 5:** Commit `feat(storage): blob store interface with disk backend`.

### Task 2: Streaming `core.SaveFile` with upload policies

**Files:**
- Modify: `internal/core/files.go` (replace `SaveFile`, `StoredPath`, `writeFileAtomic`, `MaxFileBytes`, `allowedFileMIME`)
- Create: `internal/core/sniff.go`, `internal/core/sniff_test.go`
- Create: `internal/db/migrations/0020_files_bigint.sql`
- Test: `internal/core/files_integration_test.go` (create or extend; build tag `integration`)

**Interfaces:**
- Consumes: Task 1 `storage.Store`, `storage.Key`.
- Produces:
  ```go
  type UploadPolicy struct { MaxBytes int64; Allowed map[string]bool } // MaxBytes 0 = unlimited
  var DocumentPolicy = UploadPolicy{MaxBytes: 10 << 20, Allowed: {pdf, jpeg, png}}
  func MediaPolicy(maxBytes int64) UploadPolicy
  var ErrTooLarge = errors.New("file too large")            // map to 413
  func SniffMIME(head []byte) string                        // sniff.go
  func SaveFile(ctx context.Context, q Querier, store storage.Store, orgID int64, name string, r io.Reader, pol UploadPolicy) (File, error)
  func OpenFile(ctx context.Context, store storage.Store, f File) (storage.Blob, error)
  // File.SizeBytes becomes int64
  ```

- [ ] **Step 1: Write failing sniff tests** (`sniff_test.go`, table): ISO-BMFF `ftyp` at bytes 4–8 with brand `qt  `→`video/quicktime`; `heic`/`heix`/`mif1`→`image/heic`; `M4A `→`audio/mp4`; `isom`/`mp42`/`avc1`→`video/mp4`; WebM EBML `1A 45 DF A3`→`video/webm`; Ogg `OggS`→`audio/ogg`; `ID3`→`audio/mpeg`; JPEG/PNG/PDF fall through to `http.DetectContentType`.
- [ ] **Step 2: Write failing integration tests:**
  - `TestSaveFileStreamsAndDedupes`: save same 3 MiB reader twice → same `File.ID`, `Count("files")==1`.
  - `TestSaveFileRejectsBeforeReadingBody`: reader = 512 bytes of text followed by a reader that `t.Fatal`s on read → `errors.Is(err, ErrInvalidInput)`.
  - `TestSaveFileTooLargeLeavesNoTemp`: `MediaPolicy(1<<20)`, 2 MiB mp4-headed reader → `errors.Is(err, ErrTooLarge)`; org dir has no `.upload-*`.
  - `TestSaveFileConcurrentDedupe`: 2 goroutines, same bytes, separate tx → one row.
  - `TestSaveFileLargeSize`: policy unlimited, size recorded as `int64` > `1<<31` using a synthetic zero reader of `3<<30` bytes behind an `ftyp mp42` header (skip with `testing.Short()`).
- [ ] **Step 3:** run `uv run --locked task test SUITE=integration PACKAGES=./internal/core/...` → FAIL.
- [ ] **Step 4: Implement.** Read first 512 bytes with `io.ReadFull` (tolerate `ErrUnexpectedEOF`), sniff, reject before reading more. Then `io.Copy` into `Pending` through `io.MultiWriter(pending, sha256)` with a counting limit (`MaxBytes+1` sentinel → `ErrTooLarge`, Abort). Insert row with existing `ON CONFLICT DO NOTHING` logic, then Commit (commit after row insert is fine: blob is content-addressed). Migration `0020_files_bigint.sql`: `ALTER TABLE files ALTER COLUMN size_bytes TYPE bigint;`. Delete `StoredPath`.
- [ ] **Step 5:** tests PASS; `go build ./...` shows remaining callers to fix in Task 3.
- [ ] **Step 6:** Commit `feat(core): stream uploads through storage with per-purpose policies`.

### Task 3: Wire the store, config and streaming multipart handler

**Files:**
- Modify: `internal/config/config.go` (add `UploadMaxBytes int64`, `UploadMinFreeBytes int64`, parsed from `SHIGOTO_UPLOAD_MAX_BYTES` / `SHIGOTO_UPLOAD_MIN_FREE_BYTES`; invalid/negative → load error)
- Modify: `internal/module/module.go` (`Deps.Store storage.Store`)
- Modify: `internal/module/files.go` (`HandleUpload` signature, streaming)
- Modify: `cmd/shigoto/main.go` (construct `storage.NewDisk(cfg.FilesDir)`), `internal/httpapi/server.go` (pass into Deps), `internal/testkit/env.go:116` (Disk on `TempDir`; add `HandlerOption` `Store(storage.Store)` for a fake)
- Modify callers: `internal/property/routes_details.go:208,237`, `internal/recon/routes.go:508,527`
- Test: `internal/config/config_test.go`, `internal/module/files_integration_test.go`, existing `property`/`recon` upload tests must pass unchanged
- Docs: README env var table (add both vars)

**Interfaces:**
- Consumes: Task 2 `SaveFile`, `DocumentPolicy`, `ErrTooLarge`; Task 1 `Store`.
- Produces:
  ```go
  func HandleUpload(w http.ResponseWriter, r *http.Request, d Deps, pol core.UploadPolicy, onSaved func(tx pgx.Tx, f core.File) error) (core.File, bool)
  ```

- [ ] **Step 1: Failing tests.** Config: defaults `1<<30` and `5<<30`; `"0"` accepted; `"-1"`/`"abc"` → error. Module: `TestUploadRefusedWhenDiskLow` (fake store `FreeBytes` returns 1 → 507 `insufficient storage`, `Count("files")==0`); `TestUploadDocumentPolicyStill10MiB` (11 MiB PDF → 413).
- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3: Implement.** `HandleUpload`: free-space check first (`free >= 0 && free < cfg.UploadMinFreeBytes` → 507); `r.MultipartReader()`, iterate parts to the one named `file` (ignore others), pass `part` and `part.FileName()` to `SaveFile`; when `pol.MaxBytes > 0` wrap body with `http.MaxBytesReader(pol.MaxBytes + 1<<20)`. Map `ErrTooLarge` and `*http.MaxBytesError` → 413. Existing callers pass `core.DocumentPolicy`. `ServeFile` temporarily opens via `core.OpenFile` (rewritten in Task 4).
- [ ] **Step 4:** `uv run --locked task test` and `task test SUITE=integration` → PASS, including all existing upload/download cases.
- [ ] **Step 5:** Commit `feat(files): streaming multipart uploads, free-space guard, configurable limits`.

### Task 4: Inline, Range-capable media serving

**Files:**
- Modify: `internal/module/files.go` (`ServeFile`)
- Test: `internal/module/files_integration_test.go`

**Interfaces:**
- Produces:
  ```go
  func ServeFile(w http.ResponseWriter, r *http.Request, store storage.Store, f core.File, inline bool)
  ```
  `inline` is honoured only when `f.MIME` is in the media allowlist; otherwise attachment.

- [ ] **Step 1: Failing tests:** `TestServeInlineRange` (mp4 file, `Range: bytes=0-1` → 206, `Content-Range: bytes 0-1/<size>`, `Content-Disposition: inline; filename=…`); `TestServeInlineIgnoredForPDF` (inline=true on PDF → `attachment`); every response has `X-Content-Type-Options: nosniff` and `Content-Security-Policy: sandbox`.
- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3: Implement** with `http.ServeContent(w, r, "", f.CreatedAt, blob)` after setting headers. Update the two existing callers to `ServeFile(w, r, d.Store, f, false)`.
- [ ] **Step 4:** tests PASS.
- [ ] **Step 5:** Commit `feat(files): inline range-capable serving for media`.

### Task 5: Traefik read timeout for large uploads

**Files:**
- Modify: `../../ansible/roles/networking/defaults/main.yml` (add `traefik_websecure_read_timeout: "15m"` with a comment: needed for Shigoto maintenance video uploads on slow mobile links; Traefik's default is 60s; applies to every service on `websecure`)
- Modify: `../../ansible/roles/networking/templates/traefik.yml.j2` (under `websecure:` add `transport: { respondingTimeouts: { readTimeout: "{{ traefik_websecure_read_timeout }}" } }`)
- Docs: `ansible/roles/shigoto/README.md` — note upload size/timeout settings

- [ ] **Step 1:** Run the networking Molecule scenario before change to confirm green: `(cd ansible/roles/networking && uv run --locked molecule --env-file ../common/defaults/main.yml test)` from repo root.
- [ ] **Step 2:** Make the change; add a Molecule verify assertion that rendered `traefik.yml` contains `readTimeout: "{{ traefik_websecure_read_timeout }}"`'s value (assert against the variable, not a literal).
- [ ] **Step 3:** `uv run --locked task validate` and the Molecule scenario → PASS.
- [ ] **Step 4:** Commit `feat(networking): configurable websecure read timeout for large uploads`.
