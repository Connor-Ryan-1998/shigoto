# S3 file storage with SeaweedFS — design

Date: 2026-11-21
Status: approved

## Intent

Shigoto's uploaded files (receipts, insurance and depreciation documents,
maintenance photos/video/voice notes) live in a local directory on ERP2.
This change moves them behind an S3-compatible API so the infrastructure is
ready for later migrations — to Cloudflare R2, OCI Object Storage, AWS S3 or
another S3 provider — by changing configuration and copying data, not code.

SeaweedFS runs in the Shigoto compose stack as the S3 endpoint on ERP2 until
then. This is a portability change, not a resilience or capacity change:
backups stay the protection against failure, and ERP2 has headroom for the
extra service.

## Goals

- An S3 backend for the existing `storage.Store` interface
  (`internal/storage/storage.go`) built on `aws-sdk-go-v2`, usable against
  SeaweedFS, Cloudflare R2 and other S3-compatible stores.
- Backend chosen by configuration; disk remains available.
- SeaweedFS deployed by the `shigoto` Ansible role, internal-network only.
- A one-off, manually run script that copies the existing blobs into S3 and
  verifies them. Not part of the shipped app.
- Weekly cold backups keep pairing the file store with the PostgreSQL dump.
- Production on ERP2 runs the S3 backend; the old files directory is deleted
  after a verified migration and a successful backup.

## Non-goals

- Presigned URLs or browser-direct downloads/uploads. Shigoto keeps
  streaming bytes itself (see Approaches; a later change can add this).
- SeaweedFS replication, multiple volume servers, or any HA setup.
- SeaweedFS continuous backup / remote tiering.
- Migrating to a cloud provider now.
- Changing upload policies, MIME allowlists, size limits, routes, status
  codes or response headers.
- Changing the `files` table or the content-addressing scheme.

## Anti-goals

- Exposing the S3 endpoint through Traefik or on any host port in
  production.
- Provider-specific code paths (SeaweedFS- or R2-only behaviour). Anything
  provider-specific must be a config switch.
- Feature code (handlers, property, sandbox, scheduler) learning about S3.
  All of it continues to use `storage.Store` only.
- Any automated task deleting `/opt/homelab/shigoto/files`.
- Splitting the backup so files and the database dump can drift apart.

## Constraints

- Callers rely on the current `storage.Store` contract:
  - `Begin` → `Pending` (an `io.Writer`), then exactly one of
    `Commit(key)` / `Abort()`. The SHA-256 key is only known after the
    whole body is written (`core.StageFile` hashes while streaming).
  - `Commit` on an existing key keeps the existing blob and discards the
    new bytes, returning nil.
  - `Open` returns a seekable `Blob` with `Size()`; `module.ServeFile`
    passes it to `http.ServeContent`, which must answer Range requests
    (iOS Safari video needs `206` for `bytes=0-1`).
  - `Delete` of a missing key is nil. `Open` of a missing key is
    `storage.ErrNotFound`.
  - `FreeBytes` returns -1 when unknown; `module` already skips the 507
    low-space guard for negative values (`internal/module/files.go:72`).
- Never hold a whole upload in memory (existing plan constraint).
- `SHIGOTO_UPLOAD_MAX_BYTES` can be `0` (unlimited), so blobs may exceed
  5 GiB — the single-request `CopyObject` limit.
- Repo rules: image versions pinned in the owning role's
  `defaults/main.yml` (SeaweedFS digest-pinned like Postgres); secrets only
  in the vault; validation via `uv run --locked task …`; idempotent
  Ansible runs.

## Approaches

Considered:

- **A. App streams bytes, cold backups (chosen).** Shigoto serves every
  download via ranged S3 reads. Backups stop SeaweedFS and snapshot its data
  directory with the dump, as today with the files directory. No new public
  surface; backup/restore runbooks barely change.
- **B. Presigned redirects.** Shigoto authorises, then redirects to a
  short-lived storage URL. Offloads large video from the app but needs the S3
  endpoint public through Traefik, plus CORS/CSP work, and loses the
  app-set `nosniff`/`sandbox` headers unless replicated at the store.
  Deferred: it only touches the serving path, so it can follow a cloud move.
- **C. As A, but backups via SeaweedFS continuous backup to the existing S3
  bucket.** Near-real-time file backup, but files and the dump stop being a
  matched pair and the restore runbook is rewritten. Rejected.

The user picked A.

## Design

### 1. Configuration (`internal/config`)

| Variable | Default | Notes |
|---|---|---|
| `SHIGOTO_STORAGE` | `disk` | `disk` or `s3`; anything else fails startup. |
| `SHIGOTO_FILES_DIR` | `./data/files` | Disk backend only (unchanged). |
| `SHIGOTO_S3_ENDPOINT` | — | Required for `s3`. Full URL, e.g. `http://storage:8333`. |
| `SHIGOTO_S3_REGION` | `auto` | Passed to the SDK; R2 uses `auto`. |
| `SHIGOTO_S3_BUCKET` | — | Required for `s3`. |
| `SHIGOTO_S3_ACCESS_KEY_ID` | — | Required for `s3`. |
| `SHIGOTO_S3_SECRET_ACCESS_KEY` | — | Required for `s3`. |
| `SHIGOTO_S3_PATH_STYLE` | `true` | Path-style addressing; SeaweedFS needs it. |
| `SHIGOTO_S3_PREFIX` | `` | Optional key prefix, so one bucket can host several apps/environments. |

Covered by the existing YAML table test in `internal/config`.

The SDK client is built with `RequestChecksumCalculation` and
`ResponseChecksumValidation` set to "when required" — newer SDK defaults
send checksum headers several S3-compatible stores reject. Not
configurable; it is correct for AWS too.

`httpapi.Deps.withDefaults` builds the store from config (disk or S3) in
place of the hard-coded `storage.NewDisk`. The `serve` command checks the
bucket is reachable (`HeadBucket`) at startup, retrying for up to 60
seconds before exiting non-zero. The retry covers the backup script's plain
`docker start` restarts and host reboots, neither of which honours
`depends_on: service_healthy`; the shared backups role is not changed.

### 2. S3 backend (`internal/storage/s3.go`)

Object keys:

- Committed blob: `<prefix><org id>/<hex sha256>` — the same shape as the
  disk layout, so the migration is a straight mapping.
- Staging upload: `<prefix>staging/<org id>/<random>`.

Behaviour:

- **Begin** returns a `Pending` whose writes stream into a multipart
  upload of the staging key (SDK upload manager fed by an `io.Pipe`), so
  memory stays bounded by part size × concurrency.
- **Commit(key)**: finish the staging upload; `HeadObject` the final key —
  if it exists, delete staging and return nil (dedupe contract); otherwise
  server-side copy staging → final, then delete staging. Copies above 5 GiB
  use multipart `UploadPartCopy`.
- **Abort**: cancel the in-flight upload (`AbortMultipartUpload`) and
  delete the staging object if it was created. Idempotent, like disk.
- **Open**: `HeadObject` for size (404 → `ErrNotFound`); returns a `Blob`
  that tracks an offset, and on `Read` issues `GetObject` with
  `Range: bytes=<offset>-`, reopening after any `Seek` that moves the
  offset. `http.ServeContent`'s seek-to-end/seek-to-start size probe must
  not trigger a GET.
- **Delete**: `DeleteObject`; missing is nil.
- **FreeBytes**: -1 (the 507 guard does not apply on S3).
- **Staging cleanup**: `PruneStaging(ctx, olderThan)` deletes staging
  objects and incomplete multipart uploads older than a cutoff. Exposed as
  an optional interface (`storage.StagingPruner`); the scheduler calls it
  daily with a 24-hour cutoff when the store implements it. No reliance on
  bucket lifecycle rules (they differ by provider).

### 3. Shared contract tests

The existing `internal/storage/disk_test.go` cases become a
backend-agnostic contract suite run against both backends: round trip,
commit-existing keeps original, abort leaves nothing, open-missing,
delete-missing, ranged read/seek, concurrent same-key commits. Disk runs in
the unit suite; S3 runs under the `integration` tag against a real
SeaweedFS, skipping when `SHIGOTO_TEST_S3_ENDPOINT` is unset (mirroring
`SHIGOTO_TEST_DATABASE_URL`). S3-specific integration tests cover
staging-prune and a >5 GiB copy path (may use a small part-size override to
avoid real 5 GiB transfers).

Existing feature integration tests keep using `storage.NewDisk`; one
end-to-end upload + Range download test runs through `testkit` with the S3
store.

Local dev: `docker-compose.yml` gains a `storage` service (SeaweedFS,
same pinned image as production) with the S3 port on `127.0.0.1` only; the
`test-integration` task starts it and exports the S3 test variables.

### 4. Migration script (one-off, not persisted in the app)

Production holds two or three files and downtime is unconstrained, so there
is no migration subcommand or app code. The cutover runbook (role README)
carries a short shell script the operator runs by hand on ERP2 with the app
stopped. It uses a throwaway S3 client container (e.g. the AWS CLI image)
attached to the `shigoto` network with the vault-sourced keys:

1. Copy `/opt/homelab/shigoto/files/<org>/<hex>` to
   `s3://<bucket>/<prefix><org>/<hex>` (skipping `.upload-*` temp files).
   The disk layout already equals the S3 key layout, so this is a straight
   recursive copy.
2. Verify: for each `files` row (`SELECT org_id, encode(sha256,'hex'),
   size_bytes FROM files` via `docker exec shigoto_db psql`), the object
   exists, its size matches, and the SHA-256 of the downloaded bytes equals
   the hex key.
3. Exit non-zero and print offending keys on any mismatch or missing
   object; otherwise print the verified count.

The script lives only in the runbook. It is not committed as an executable
and has no tests.

### 5. Ansible (`ansible/roles/shigoto`)

- `defaults/main.yml`: `shigoto_seaweedfs_version` (digest-pinned;
  `validate.yml` asserts the `@sha256:` form as it does for Postgres),
  `shigoto_storage_backend: s3`, `shigoto_s3_bucket: shigoto`.
- Vault: `shigoto_s3_access_key_id`, `shigoto_s3_secret_access_key`
  (secret ≥ 24 characters, validated like other secrets); placeholders
  added to `ansible/ci/group_vars/all.yml`.
- Compose: a `storage` service (`container_name: shigoto_storage`) running
  SeaweedFS all-in-one (`weed server` with S3 enabled), data bind-mounted
  at `{{ docker_root_dir }}/shigoto/seaweedfs`, on the internal `shigoto`
  network only, with a healthcheck; S3 identities rendered from vault
  variables into a root-owned, read-only config file. App gets
  `depends_on: storage: service_healthy`. Hardened like the app where
  SeaweedFS allows (non-root user, `cap_drop`, `no-new-privileges`).
- `app.env.j2` renders `SHIGOTO_STORAGE` and the `SHIGOTO_S3_*` variables;
  `SHIGOTO_FILES_DIR` and the files mount render only when
  `shigoto_storage_backend == 'disk'`.
- Bucket creation is idempotent and owned by the deployment (SeaweedFS
  bucket create in a task, or app-side create-if-missing guarded by
  config) — builder's call, as long as a second run reports no change.
- `test SUITE=shigoto` rendered-config checks extended: no host port or
  Traefik labels on `storage`, secrets only from vault vars, disk-only
  mount absent in s3 mode.

### 6. Backups

`cloud-playbook.yaml` for ERP2:

- `backup_paths`: `{{ docker_root_dir }}/shigoto/seaweedfs` and the dump
  (replacing `shigoto/files` after cutover; both during cutover).
- `backup_quiesce_containers: [shigoto_app, shigoto_storage]` — app stops
  first so no writes are in flight, then SeaweedFS for a consistent cold
  copy; restart order is the reverse (existing script behaviour).

Restore runbook (role README and Shigoto chapter in `docs/index.html`):
restore `shigoto/seaweedfs` and the dump into an isolated stack, start
storage then app, spot-check a download.

### 7. Cutover on ERP2 (runbook, manual)

1. Deploy with `shigoto_storage_backend: disk` and SeaweedFS present (new
   image, storage service running, app still on disk). Backup paths include
   both locations.
2. Stop the app; run the §4 migration script. Must exit 0.
3. Set `shigoto_storage_backend: s3`; deploy; check, then re-check reports
   no unexpected changes. Spot-check document download and video seek.
4. Run a backup and confirm it succeeded with the SeaweedFS directory.
5. Operator deletes `/opt/homelab/shigoto/files` by hand; remove it from
   `backup_paths` and the role's directory task.

## Left to the builder

- SDK part size/concurrency for uploads and copies.
- Exact SeaweedFS invocation flags and healthcheck command.
- How bucket creation is made idempotent (see §5).
- Internal structure of the S3 `Blob` reader and the `StagingPruner`
  wiring in the scheduler.
- Whether the disk backend also gains staging cleanup (stale `.upload-*`
  files) — optional.

## Calls made by the designer (for review)

- `SHIGOTO_S3_PREFIX` added for shared buckets later.
- Checksum mode fixed to "when required", not configurable.
- Startup `HeadBucket` check retries for up to 60 s, then fails the
  server (instead of failing immediately or changing the shared backups
  role).
- Staging cleanup by app scheduler, not bucket lifecycle rules.
- Migration is a runbook shell script using a throwaway AWS CLI
  container, verified against the `files` table; orphan disk blobs are
  copied too (harmless at this size).
- Production key layout mirrors disk (`<org>/<hex>`).
