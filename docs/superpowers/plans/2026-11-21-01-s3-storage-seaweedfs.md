# S3 File Storage with SeaweedFS Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an `aws-sdk-go-v2` S3 backend behind `storage.Store`, selectable by config, and deploy SeaweedFS in the Shigoto stack on ERP2 as its endpoint, with backups and a manual cutover runbook.

**Architecture:** New `internal/storage/s3.go` implements the existing `Store`/`Pending`/`Blob` contract (staging key → server-side copy on commit; lazy ranged GETs on read). `cmd/shigoto` builds disk or S3 from config and waits up to 60 s for the bucket. The `shigoto` Ansible role runs SeaweedFS all-in-one on the internal network; backups quiesce app then storage and snapshot SeaweedFS's data dir with the dump.

**Tech Stack:** Go 1.27, `github.com/aws/aws-sdk-go-v2` (`service/s3`, `feature/s3/manager`, `credentials`; no shared-config loading), SeaweedFS (`chrislusf/seaweedfs`), Ansible, Python unittest checks.

**Spec:** `docs/superpowers/specs/2026-11-21-s3-storage-seaweedfs-design.md`

## Global Constraints

- `SHIGOTO_STORAGE` = `disk` (default) | `s3`; anything else fails startup.
- S3 env: `SHIGOTO_S3_ENDPOINT` (required, absolute http/https URL), `SHIGOTO_S3_REGION` (default `auto`), `SHIGOTO_S3_BUCKET` (required), `SHIGOTO_S3_ACCESS_KEY_ID` (required), `SHIGOTO_S3_SECRET_ACCESS_KEY` (required), `SHIGOTO_S3_PATH_STYLE` (default `true`), `SHIGOTO_S3_PREFIX` (default empty).
- SDK client always uses `RequestChecksumCalculation: WhenRequired` and `ResponseChecksumValidation: WhenRequired`.
- Keys: committed `<prefix><org id>/<hex sha256>`; staging `<prefix>staging/<org id>/<random>`.
- Never hold a whole upload in memory. Copies above 5 GiB use multipart `UploadPartCopy`.
- Startup bucket check retries for up to 60 s, then `serve` exits non-zero.
- No public S3 endpoint: no Traefik labels, no host ports for `storage` in production.
- SeaweedFS image digest-pinned only in `ansible/roles/shigoto/defaults/main.yml`; S3 secrets only in the vault (CI placeholders in `ansible/ci/group_vars/all.yml`).
- No automated deletion of `/opt/homelab/shigoto/files`. Migration is a runbook script, not app code.
- Run validation sequentially: app via `task …` in `apps/shigoto`; infra via `uv run --locked task …` at repo root.

## Review Focus

- Client disconnects mid-upload → `Abort` leaves no staging object and no incomplete multipart upload (Task 3 `TestS3AbortLeavesNothing`).
- iOS Safari `Range: bytes=0-1` on a video stored in S3 → 206 with exactly two bytes (Task 6 `TestS3ServeRange`).
- Same content committed twice concurrently → both commits nil, one object, no staging left (Task 2 contract `ConcurrentSameKey`).
- App container starts before SeaweedFS answers (backup restart, reboot) → server waits, doesn't crash-loop (Task 6 `TestRetryFor*`).
- `http.ServeContent` size probe (seek end, seek start) must not issue GETs; seek past end then read returns `io.EOF`, negative seek errors (Task 4 `TestS3BlobSeek*`).

---

### Task 1: Storage configuration

**Files:**
- Modify: `internal/config/config.go` (Config struct, `Load`)
- Modify: `internal/config/load_table_test.go` (`loadWant` fields + `check`)
- Test: `internal/config/testdata/load.yaml`

**Interfaces:**
- Produces:
  ```go
  type S3 struct {
      Endpoint, Region, Bucket, AccessKeyID, SecretAccessKey, Prefix string
      PathStyle bool
  }
  // on Config:
  Storage string // "disk" | "s3"
  S3      S3     // zero unless Storage == "s3"
  ```

- [ ] **Step 1: Add YAML cases** to `testdata/load.yaml` (add `storage`, `s3_endpoint`, `s3_region`, `s3_bucket`, `s3_path_style`, `s3_prefix` to `loadWant`):
  - `defaults` gains `storage: disk`.
  - `storage s3 full`: env `SHIGOTO_STORAGE: s3`, `SHIGOTO_S3_ENDPOINT: http://storage:8333`, `SHIGOTO_S3_BUCKET: shigoto`, `SHIGOTO_S3_ACCESS_KEY_ID: k`, `SHIGOTO_S3_SECRET_ACCESS_KEY: s` → want `storage: s3`, `s3_region: auto`, `s3_path_style: true`, `s3_prefix: ""`, `s3_bucket: shigoto`.
  - `storage s3 overrides`: full env + `SHIGOTO_S3_REGION: ap-sydney-1`, `SHIGOTO_S3_PATH_STYLE: "false"`, `SHIGOTO_S3_PREFIX: prod` → `s3_region: ap-sydney-1`, `s3_path_style: false`, `s3_prefix: prod/` (trailing `/` appended when missing).
  - Errors (each from the full s3 env with one change): unknown `SHIGOTO_STORAGE: gcs` → `SHIGOTO_STORAGE`; each of endpoint/bucket/access key/secret set to `""` → that variable's name; `SHIGOTO_S3_ENDPOINT: storage:8333` → `SHIGOTO_S3_ENDPOINT`; `SHIGOTO_S3_PATH_STYLE: maybe` → `SHIGOTO_S3_PATH_STYLE`.
  - `disk ignores s3 vars`: `SHIGOTO_S3_ENDPOINT: nonsense` with no `SHIGOTO_STORAGE` → `storage: disk` (no error).
- [ ] **Step 2:** `go test ./internal/config/` → FAIL (unknown fields / wrong values).
- [ ] **Step 3: Implement** in `Load`: parse `SHIGOTO_STORAGE`; S3 vars read and validated only when `s3`; endpoint validated like `SHIGOTO_BASE_URL`; path style via `strconv.ParseBool`. Document fields in the struct comments like neighbours.
- [ ] **Step 4:** `go test -race ./internal/config/` → PASS.
- [ ] **Step 5: Commit** `feat(config): storage backend and S3 settings`.

### Task 2: Backend contract suite + local SeaweedFS

**Files:**
- Create: `internal/storage/contract_test.go` (package `storage_test`, no build tag)
- Modify: `internal/storage/disk_test.go` (keep disk-only cases: file mode, `.upload-*` temp checks, FreeBytes; call contract)
- Modify: `docker-compose.yml` (add `storage` service), `Taskfile.yml` (`test-integration`)
- Modify: `docs/testing.md` (S3 integration env)

**Interfaces:**
- Produces (test helper, used by Task 3/4):
  ```go
  // runContract runs every backend-agnostic case against stores from newStore.
  func runContract(t *testing.T, newStore func(t *testing.T) storage.Store)
  ```

- [ ] **Step 1: Write `runContract`** with subtests: `RoundTrip` (Begin, write "hello", Commit, Open reads "hello", `Size()==5`), `CommitExistingKeepsOriginal` (commit same key twice, second nil, Open still original), `AbortThenOpenMissing`, `OpenMissing` (`errors.Is(err, storage.ErrNotFound)`), `DeleteMissingIsNil`, `DeleteThenOpenMissing`, `SeekAndRead` (content "hello world": Seek(6,Start) → read "world"; Seek(0,End) returns 11), `ConcurrentSameKey` (two goroutines Begin/write/Commit same content: both nil, Open reads content), `OrgIsolation` (same SHA committed for org 1 only: Open for org 2 → ErrNotFound). Add `TestDiskContract` in `disk_test.go` calling it with `storage.NewDisk(t.TempDir())`; remove disk cases it now duplicates.
- [ ] **Step 2:** `go test -race ./internal/storage/` → PASS (disk already satisfies the contract; this is a refactor).
- [ ] **Step 3: Local SeaweedFS:** `docker-compose.yml` gains `storage` (image `chrislusf/seaweedfs:<same tag@digest as Task 7>`, `server -s3` with a data volume, S3 port published on `127.0.0.1:8333` only, healthcheck). No `-s3.config` locally (anonymous access is fine for dev/test). `test-integration` brings up `storage` with `db mailpit` and exports `SHIGOTO_TEST_S3_ENDPOINT=http://localhost:8333`. Add one line to `docs/testing.md`.
- [ ] **Step 4:** `docker compose up -d --wait storage` → healthy.
- [ ] **Step 5: Commit** `test(storage): backend contract suite and local SeaweedFS`.

### Task 3: S3 backend — write path and bucket

**Files:**
- Create: `internal/storage/s3.go`, `internal/storage/export_test.go`
- Test: `internal/storage/s3_integration_test.go` (`//go:build integration`)
- Modify: `go.mod`, `go.sum`

**Interfaces:**
- Consumes: Task 2 `runContract`.
- Produces:
  ```go
  type S3Config struct {
      Endpoint, Region, Bucket, AccessKeyID, SecretAccessKey, Prefix string
      PathStyle  bool
      HTTPClient *http.Client // nil = SDK default; tests inject a counting transport
  }
  func NewS3(cfg S3Config) (*S3, error)
  func (s *S3) EnsureBucket(ctx context.Context) error // HeadBucket; on not-found, CreateBucket (BucketAlreadyOwnedByYou is success)
  // plus Store methods: Begin, Delete, FreeBytes (-1); Open lands in Task 4
  ```
  `export_test.go`: `func SetCopyLimits(threshold, partSize int64) (restore func())` over package vars `copyMultipartThreshold = 5 << 30`, `copyPartSize = 512 << 20`.

- [ ] **Step 1: Write failing tests** in `s3_integration_test.go`. Helper `newTestS3(t) *storage.S3`: skip if `SHIGOTO_TEST_S3_ENDPOINT` unset; bucket `shigoto-test`; unique `Prefix` per test (`<sanitized t.Name()>-<random>/`); `EnsureBucket`. Tests:
  - `TestS3EnsureBucketIdempotent`: twice on a fresh bucket name → nil both times.
  - `TestS3AbortLeavesNothing`: Begin, write 6 MiB (forces multipart), Abort → `ListObjectsV2` under `<prefix>staging/` empty and `ListMultipartUploads` under it empty.
  - `TestS3CommitLeavesNoStaging`: after Commit, staging prefix empty; object at `<prefix>1/<hex>`.
  - `TestS3MultipartCopy`: `SetCopyLimits(5<<20, 5<<20)`; commit 12 MiB → Open/read back SHA-256 matches (use raw `GetObject` until Task 4).
- [ ] **Step 2:** `task test SUITE=integration PACKAGES=./internal/storage/` → FAIL (`NewS3` undefined).
- [ ] **Step 3: Implement** `s3.go`: client via `s3.New(s3.Options{BaseEndpoint, Region, Credentials: credentials.NewStaticCredentialsProvider, UsePathStyle, RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired, ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired, HTTPClient})`. `Begin` starts `manager.Uploader.Upload` to a random staging key in a goroutine reading an `io.Pipe`; `Write` feeds the pipe. `Commit`: close pipe, await upload (error → delete staging, return error); `HeadObject` final key — exists → delete staging, nil; else copy (single `CopyObject` ≤ threshold, else `CreateMultipartUpload` + `UploadPartCopy` ranges + `Complete`, abort on error) then delete staging. `Abort`: `CloseWithError`, await, best-effort delete staging; idempotent; second Commit/Abort follows disk semantics. `isNotFound(err error) bool` checks `smithy.APIError` codes `NotFound`/`NoSuchKey`/`NoSuchBucket` and HTTP 404. Keys built by one helper honouring `Prefix`.
- [ ] **Step 4:** same command → write-path tests PASS.
- [ ] **Step 5: Commit** `feat(storage): S3 backend write path`.

### Task 4: S3 backend — ranged reads

**Files:**
- Modify: `internal/storage/s3.go` (`Open`, `s3Blob`)
- Test: `internal/storage/s3_integration_test.go`

**Interfaces:**
- Produces: `func (s *S3) Open(ctx context.Context, key Key) (Blob, error)` — `HeadObject` for size (not found → `ErrNotFound`); returned blob is lazy.

- [ ] **Step 1: Write failing tests** using `HTTPClient` with a transport counting GET requests to object keys:
  - `TestS3BlobSeekProbeNoGet`: Open; `Seek(0, End)` == size; `Seek(0, Start)` == 0 → 0 GETs.
  - `TestS3BlobReadAndReseek`: "hello world": `ReadAll` == content with 1 GET; `Seek(6, Start)`, read 5 → "world" (2nd GET with `Range: bytes=6-`).
  - `TestS3BlobSeekPastEndEOF`: `Seek(size+5, Start)` ok, `Read` → `0, io.EOF`, no GET.
  - `TestS3BlobNegativeSeekErrors`: `Seek(-1, Start)` → error.
  - `TestS3Contract`: `runContract(t, func(t) storage.Store { return newTestS3(t) })` (added here because most contract cases need `Open`).
- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3: Implement** `s3Blob` (offset, size, open body): `Seek` updates offset and closes any open body when it changes; `Read` returns `io.EOF` at/after size, else lazily `GetObject` with `Range: bytes=<off>-`; `Close` closes the body.
- [ ] **Step 4:** run → PASS; `go test -race ./internal/storage/` (unit) still PASS.
- [ ] **Step 5: Commit** `feat(storage): S3 ranged blob reads`.

### Task 5: Staging cleanup

**Files:**
- Modify: `internal/storage/storage.go` (interface), `internal/storage/s3.go`
- Modify: `internal/scheduler/scheduler.go` (`Tick`)
- Test: `internal/storage/s3_integration_test.go`, `internal/scheduler/scheduler_integration_test.go`

**Interfaces:**
- Produces:
  ```go
  // StagingPruner is implemented by stores whose abandoned staged writes
  // need explicit clean-up.
  type StagingPruner interface {
      PruneStaging(ctx context.Context, before time.Time) error
  }
  ```
  `*S3` implements it: delete `<prefix>staging/` objects with `LastModified < before`, abort multipart uploads under that prefix initiated before `before`. Committed keys untouched.

- [ ] **Step 1: Write failing tests:**
  - `TestS3PruneStaging`: commit one blob; leave one staged (Begin + write 1 KiB, no Commit/Abort — write a raw staging object via the client if the pipe makes that awkward) and one in-flight multipart (raw `CreateMultipartUpload` under staging); `PruneStaging(ctx, time.Now().Add(time.Minute))` → staging prefix empty, no multipart uploads, committed blob still opens. `PruneStaging(ctx, time.Now().Add(-time.Hour))` on fresh staging leaves it.
  - `TestTickPrunesStaging` (scheduler): fake `storage.Store` embedding `storage.NewDisk(t.TempDir())` plus a `PruneStaging` that records `before`; `Tick(ctx, now)` → recorded `before == now.Add(-24*time.Hour)`.
- [ ] **Step 2:** run both packages (integration) → FAIL.
- [ ] **Step 3: Implement**; in `Tick`, after `PrunePendingFiles`, call `PruneStaging(ctx, now.Add(-24*time.Hour))` when `j.Store` implements `StagingPruner`; log failures like neighbours. (Hourly tick with a 24 h cutoff satisfies the spec's daily clean-up.)
- [ ] **Step 4:** run → PASS.
- [ ] **Step 5: Commit** `feat(storage): prune abandoned S3 staging uploads`.

### Task 6: Wire the backend into `serve`

**Files:**
- Create: `cmd/shigoto/store.go`, `cmd/shigoto/store_test.go`
- Modify: `cmd/shigoto/main.go:189-250` (replace `storage.NewDisk(cfg.FilesDir)`)
- Test: `internal/module/serve_s3_integration_test.go` (`//go:build integration`)

**Interfaces:**
- Consumes: Task 1 `config.S3`, Task 3 `NewS3`/`EnsureBucket`.
- Produces:
  ```go
  func openStore(ctx context.Context, cfg config.Config) (storage.Store, error) // disk → NewDisk(cfg.FilesDir); s3 → NewS3 + retryFor(ctx, 60*time.Second, 2*time.Second, s.EnsureBucket)
  func retryFor(ctx context.Context, timeout, every time.Duration, fn func(context.Context) error) error
  ```
  `httpapi.Deps.withDefaults` keeps its `NewDisk` fallback (tests rely on it); `serve` always passes the store.

- [ ] **Step 1: Write failing tests:**
  - `TestRetryForSucceedsAfterFailures`: fn fails twice then nil; `retryFor(ctx, time.Second, time.Millisecond, fn)` → nil, 3 calls.
  - `TestRetryForTimesOut`: fn always `errBoom`; timeout 50 ms, every 10 ms → error, `errors.Is(err, errBoom)`, returned within ~100 ms.
  - `TestRetryForHonoursContext`: cancelled ctx → returns promptly with error.
  - `TestOpenStoreDisk`: `cfg.Storage="disk"` → `*storage.Disk`.
  - `TestS3ServeRange` (module, integration, skip without `SHIGOTO_TEST_S3_ENDPOINT`): store a 4 KiB MP4-typed blob in S3 via `Begin/Commit`; `ServeFile(rec, req with "Range: bytes=0-1", store, file, true)` → 206, body = first 2 bytes, `Content-Range: bytes 0-1/4096`, `Content-Disposition` inline. Also a no-Range request → 200, full body.
- [ ] **Step 2:** `go test ./cmd/shigoto/` and integration for `./internal/module/` → FAIL.
- [ ] **Step 3: Implement** `store.go`; in `serve`, call `openStore` after migrations and fail with `opening file store: %w`.
- [ ] **Step 4:** run → PASS; then `task verify` (full app suite) → PASS.
- [ ] **Step 5: Commit** `feat: select disk or S3 file store at startup`.

### Task 7: Ansible — SeaweedFS service and S3 settings

Repo root (`homelab-infra`).

**Files:**
- Modify: `ansible/roles/shigoto/defaults/main.yml`, `tasks/validate.yml`, `tasks/main.yml`, `templates/docker-compose.yml.j2`, `templates/app.env.j2`, `README.md` (Inputs and secrets)
- Create: `ansible/roles/shigoto/templates/seaweedfs-s3.json.j2`
- Modify: `ansible/ci/group_vars/all.yml`
- Test: `tests/checks/test_shigoto.py`

**Interfaces:**
- Produces role vars: `shigoto_seaweedfs_image: chrislusf/seaweedfs`, `shigoto_seaweedfs_version: "<tag>@sha256:<64 hex>"` (resolve a current release digest with `docker buildx imagetools inspect`), `shigoto_storage_backend: s3`, `shigoto_s3_bucket: shigoto`, `shigoto_s3_access_key_id: ""`, `shigoto_s3_secret_access_key: ""`. Container `shigoto_storage`, service name `storage`, endpoint `http://storage:8333`, data dir `{{ docker_root_dir }}/shigoto/seaweedfs`.

- [ ] **Step 1: Write failing checks** in `test_shigoto.py` (add the two S3 secrets to `variables()` using `PASSWORD`-style values):
  - `test_private_persistent_database_and_hardened_app` updated: services `{"app","db","storage"}`; `storage.image == image + ":" + version`, version matches `@sha256:[a-f0-9]{64}$`; `container_name == "shigoto_storage"`; no `ports`, no `labels`, `networks == ["shigoto"]`; data mount source `/opt/homelab/shigoto/seaweedfs`; config mount read-only; healthcheck present; app `depends_on.storage.condition == "service_healthy"`.
  - `test_s3_mode_env_and_no_files_mount`: default (s3) app env contains `SHIGOTO_STORAGE=s3`, `SHIGOTO_S3_ENDPOINT=http://storage:8333`, `SHIGOTO_S3_BUCKET=shigoto`, `SHIGOTO_S3_PATH_STYLE=true`, raw access key/secret round-trip; no `SHIGOTO_FILES_DIR`; app has no `/data/files` mount.
  - `test_disk_mode_keeps_files_mount`: `shigoto_storage_backend="disk"` → `SHIGOTO_STORAGE=disk`, `SHIGOTO_FILES_DIR=/data/files`, files mount present, no `SHIGOTO_S3_` lines, storage service still present.
  - `test_seaweedfs_identity_from_vault`: rendered `seaweedfs-s3.json.j2` parses as JSON; single identity whose credentials equal the vault vars.
  - Invalid-settings test extended: `shigoto_storage_backend="gcs"`, empty access key, 23-char secret, bucket `Bad_Bucket`, undigested seaweed version → each fails without writes or secret output.
  - CI vars: S3 secret ≥ 24 chars, access key non-empty, no `shigoto_seaweedfs_version` in CI vars.
- [ ] **Step 2:** `uv run --locked task test SUITE=shigoto` → FAIL.
- [ ] **Step 3: Implement:**
  - `validate.yml`: backend in `[disk, s3]`; seaweed version digest form (extend the release assert); access key `^[A-Za-z0-9]{16,128}$`; secret length ≥ 24; bucket `^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`; add the new values to the single-line loop.
  - Compose: `storage` service as checked above, hardened (`cap_drop`, `no-new-privileges`, non-root user if the image supports it — data dir ownership must match), command `server -s3 -s3.config=/etc/seaweedfs/s3.json -dir=/data` plus needed flags. App `depends_on` storage healthy; files volume only when backend is `disk`.
  - `app.env.j2`: `SHIGOTO_STORAGE={{ shigoto_storage_backend }}`, S3 block when `s3`, `SHIGOTO_FILES_DIR` only when `disk`.
  - `main.yml`: create seaweedfs data dir; render `seaweedfs-s3.json` (`no_log`, not world-readable, owner the container user can read); when it changed, recreate only `storage` (`docker_compose_v2` with `services: [storage]`, `recreate: always`) after the main compose task; upload-dir task only when backend is `disk`. Second run must report no changes.
  - CI placeholders: `shigoto_s3_access_key_id: placeholderShigotoS3Key`, `shigoto_s3_secret_access_key: placeholder-shigoto-s3-secret-key`.
  - README "Inputs and secrets": the two vault inputs.
- [ ] **Step 4:** `uv run --locked task test SUITE=shigoto` → PASS; `uv run --locked task validate` → PASS.
- [ ] **Step 5: Commit** `feat(shigoto): SeaweedFS S3 store in the Shigoto stack`.

### Task 8: ERP2 backups, cutover override and runbook

**Files:**
- Modify: `ansible/playbooks/01_configuration/cloud-playbook.yaml` (ERP2 play vars)
- Modify: `ansible/roles/shigoto/README.md` (Backups and isolated recovery; new "Move uploads to SeaweedFS (one-off)" section)
- Modify: `docs/index.html` (Shigoto chapter: storage service, vault inputs, backups, restore)
- Test: `tests/checks/test_shigoto.py` (`ShigotoIntegrationTests`), `tests/integration/test_backup.py`

**Interfaces:**
- Consumes: Task 7 container/dir names.

- [ ] **Step 1: Update checks first:**
  - `test_erp2_deploys_and_backs_up_shigoto`: `backup_paths == ["{{ docker_root_dir }}/shigoto/files", "{{ docker_root_dir }}/shigoto/seaweedfs", "{{ docker_root_dir }}/backups/dumps/shigoto.sql.gz"]`; `backup_quiesce_containers == ["shigoto_app", "shigoto_storage"]`.
  - `test_erp2_enables_shared_smtp_and_authentik_sso`: expected play var set gains `shigoto_storage_backend`; add the two S3 secrets to `upstream`.
  - `test_backup.py`: shigoto profile paths (line 40) and writers (lines 51, 235) to match.
- [ ] **Step 2:** `uv run --locked task test SUITE=shigoto` and `SUITE=backups` → FAIL.
- [ ] **Step 3: Implement:** playbook vars as asserted, plus `shigoto_storage_backend: disk` with comment `# Cutover only: remove after the SeaweedFS migration (roles/shigoto/README.md).` Runbook in README, following spec §7:
  1. Add vault inputs; `check` then `deploy TARGET=cloud TAG=erp2_service` (storage runs, app on disk).
  2. `docker stop shigoto_app`; run the migration script (spec §4): throwaway pinned `amazon/aws-cli` container on network `shigoto_shigoto`, endpoint `http://storage:8333`, keys from vault via env; `s3 cp --recursive` of `files/` excluding `*/.upload-*`; for each `files` row from `docker exec shigoto_db psql -At` check `head-object` size and downloaded SHA-256 equals the hex key; non-zero exit listing bad keys.
  3. Apply Task 9 commit; deploy; check twice; spot-check a document download and video seek.
  4. Run the backup service and confirm success in `status.json`.
  5. Delete `/opt/homelab/shigoto/files` by hand.

  Restore section: restore `shigoto/seaweedfs` + dump into an isolated stack, start storage before app. Update `docs/index.html` to match.
- [ ] **Step 4:** `uv run --locked task test SUITE=shigoto`, `SUITE=backups`, `SUITE=docs`, then `uv run --locked task validate` → PASS.
- [ ] **Step 5: Commit** `feat(erp2): back up SeaweedFS and document storage cutover`.

### Task 9: Post-cutover cleanup (apply only at runbook step 3)

Prepare on its own branch/commit; do not merge until the migration script has exited 0 on ERP2.

**Files:**
- Modify: `ansible/playbooks/01_configuration/cloud-playbook.yaml`, `tests/checks/test_shigoto.py`, `tests/integration/test_backup.py`, `ansible/roles/shigoto/README.md`

- [ ] **Step 1:** Update checks: `backup_paths` without `shigoto/files`; play var set without `shigoto_storage_backend`.
- [ ] **Step 2:** `uv run --locked task test SUITE=shigoto` / `SUITE=backups` → FAIL.
- [ ] **Step 3:** Remove the override and the files backup path; note in README that the cutover is complete.
- [ ] **Step 4:** Both suites and `uv run --locked task validate` → PASS.
- [ ] **Step 5: Commit** `chore(erp2): finish SeaweedFS cutover`.
