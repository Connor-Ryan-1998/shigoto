# Sandbox Portal Examples Implementation Plan

> **For agentic workers:** Use executing-plans to implement this plan task-by-task. Execution is native, on main, as requested.

**Goal:** Make `populate-sbx` demonstrate rent write-offs, repairs, media, visits and entry consent, and remain safe to rerun.

**Architecture:** Extend the existing sandbox seeders with property-domain calls. Embed small fictional media, stage it through the configured storage interface, and commit bytes before the database transaction succeeds. On failure, discard only newly created sandbox media.

**Tech Stack:** Go, pgx/PostgreSQL, existing storage.Store and property domain.

**Spec:** Approved scope in this conversation: repair lifecycle, playable local media, availability and consent states, emergency phone, write-off recovery, repeatable isolated reset.

## Global Constraints

- Never populate production/operator databases during verification.
- No runtime downloads, notifications, external providers or new runtime dependencies.
- Preserve collision refusal, transaction rollback, portal isolation and unrelated records.
- Keep existing tester-upload retention policy; do not introduce broad filesystem deletion.

## Review Focus

- Foreign maintenance references must abort reset rather than disappear.
- Storage failure must preserve the previous sandbox.
- Media must have real playable bytes, not just valid headers.
- Seeded visits must be future-dated relative to population day and use the supplied timezone.
- Rent write-offs and recovery must appear through the actual lease account.

### Task 1: Repeatable maintenance-aware reset

Files: `internal/sandbox/reset.go`, `internal/sandbox/reset_integration_test.go`.

- [x] Add a failing test for resetting ticket files, events, availability, visits and pending uploads.
- [x] Assert unrelated maintenance survives and foreign references abort transactionally.
- [x] Delete children in ownership-safe order; scope ticket files through their ticket.
- [x] Run sandbox integration tests through `uv run --locked task test SUITE=integration PACKAGES=./internal/sandbox`.

### Task 2: Complete portal scenarios and stored media

Files: sandbox population/scenario seeders and integration tests, new maintenance seeder and embedded media fixtures, `cmd/shigoto/sandbox.go`, `README.md`.

- [x] Add failing scenario tests for ticket states, consent, messages, emergency phone, write-off/recovery and stored media.
- [x] Make `Populate` require the configured `storage.Store`; update all callers.
- [x] Seed scenarios through domain functions and write tiny embedded media through staging.
- [x] Test storage failure rollback, serving seeded media, tenant visibility and reruns.
- [x] Document example locations, fictional media and retained tester uploads.
- [x] Run sandbox integration tests, then application unit/integration checks and lint sequentially.

## Execution notes

- Reset and scenario tests failed for the expected missing behavior before the fixes.
- Fixture image, H.264 video and AAC speech decoded successfully with FFmpeg.
- Full Go integration suite passed before the final review fix.
- Review found that a lost COMMIT reply could delete committed media; the new
  `TestPopulateLostCommitReplyPreservesMedia` reproduced the failure.
- Ruling: retain tiny demo blobs on unknown commit outcomes rather than query
  again over an unreliable connection. The cost is possible orphan demo bytes;
  this avoids broken committed file references. Confirmed rollbacks still clean up.
- Final `uv run --locked task verify` passed: build, lint, Go unit/integration
  tests, 587 web tests and 15 browser tests. This includes the lost-COMMIT-reply
  regression test after the fix.
