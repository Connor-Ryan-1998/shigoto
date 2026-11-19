# Repair Tickets with Media Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Tenants raise repair tickets with photos, video and voice notes from the portal; staff manage them on the property's Maintenance tab and see open repairs on the dashboard; both sides see a status timeline.

**Architecture:** Tickets live in the `property` module (`internal/property/maintenance*.go`), with an append-only event table as the timeline (and future notification source). Media uses plan 01's `storage.Store`, `core.MediaPolicy` and inline `ServeFile`. Portal handlers live in `internal/portal/maintenance.go` with lease-membership checks reused from plan 02.

**Tech Stack:** Go, pgx, Postgres, SvelteKit 5, DaisyUI, browser `MediaRecorder`.

**Spec:** `docs/superpowers/specs/2026-11-19-tenant-portal-rent-repairs-design.md` §3

**Depends on:** plan 01 (storage, `MediaPolicy`, `HandleUpload(…, pol, …)`, `ServeFile(…, inline)`), plan 02 (`property.LeaseHasContact`, portal Rent tab nav).

## Global Constraints

- Statuses: `new | viewed | assigned | scheduled | completed | cancelled` (`scheduled` is set by plan 04). Terminal: `completed`, `cancelled`; mutations on terminal tickets → 409 (`core.ErrInvalidState`).
- Urgency: `routine | soon | urgent`. Categories: `plumbing, electrical, appliance, heating_cooling, doors_windows_locks, pests, garden_exterior, other` (labels: "Plumbing", "Electrical", "Appliance", "Heating & cooling", "Doors, windows & locks", "Pests", "Garden & exterior", "Other").
- Ref: per-org sequence, displayed `R-` + zero-padded 4 digits (`R-0042`).
- Tenant may raise only on a lease that includes their contact **and** is active today (`start_date <= today` and not ended/terminated) → otherwise 422 `{"error":"lease is not active"}`; foreign lease → 404.
- All tenants on a ticket's lease see it. Tenants see only events with `tenant_visible = true`; internal notes never appear in any portal response.
- Staff: read endpoints `property_read`; every mutation `property_write`. "Viewed" only by `property_write`.
- Tenants after submit: may add comments and files until terminal; may cancel only while `new|viewed`; may not edit title/description/category/urgency.
- Max 20 files per ticket (pending + attached) → 422 `{"error":"too many files"}`.
- Pending uploads older than 24h are deleted by the hourly scheduler (row; blob only when no other `files` reference remains).
- Urgent banner copy (portal form, when urgency = urgent): **"Burst pipe, gas leak, no power or hot water, or anything unsafe? Call {emergency_phone} now, then log it here."** If no phone is set: **"…? Phone your property manager now, then log it here."**
- No sidebar entry; maintenance appears only on the property page and the dashboard.

## Review Focus

- Tenant whose lease ended yesterday tries to raise a ticket → 422, and the form explains why (Task 4 YAML, Task 8 Vitest).
- Tenant submits while a video is still uploading → submit disabled until uploads finish or are removed; a failed upload shows Retry/Remove and never blocks forever (Task 8 Vitest "blocks submit during upload").
- Staff internal note text never leaks via portal ticket detail, list, or events (Task 4 YAML exact-body cases using a sentinel string).
- Two staff open a new ticket simultaneously → exactly one `viewed` event (Task 2 `TestMarkViewedConcurrent`).
- iPhone voice note (`audio/mp4`) and Android (`audio/webm`) both accepted and playable inline (Task 3 integration test with fixture headers; Task 9 e2e uses a webm fixture).

---

### Task 1: Schema

**Files:**
- Create: `internal/db/migrations/0022_maintenance.sql`
- Test: `internal/db/migrate_integration_test.go` (existing migration test must still pass)

Tables (all with `org_id bigint NOT NULL REFERENCES orgs`, composite FKs `(org_id, id)` where the codebase already uses them):
- `orgs ADD COLUMN emergency_phone text NOT NULL DEFAULT ''`, `ADD COLUMN maintenance_seq integer NOT NULL DEFAULT 0`.
- `maintenance_tickets(id, org_id, ref_no int, property_id, lease_id NULL, raised_by_contact_id NULL REFERENCES contacts, raised_by_user_id NULL REFERENCES users, title text CHECK length 1..120, description text CHECK length 1..4000, category text CHECK IN (…), urgency text CHECK IN (…), status text CHECK IN (…) DEFAULT 'new', supplier_contact_id NULL REFERENCES contacts, access_notes text NOT NULL DEFAULT '', viewed_at, completed_at, cancelled_at timestamptz NULL, cancel_reason text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now(), UNIQUE(org_id, ref_no))`; index `(org_id, property_id, status)`.
- `maintenance_ticket_files(ticket_id, file_id REFERENCES files, uploaded_by_user_id, created_at, PRIMARY KEY(ticket_id, file_id))`.
- `maintenance_events(id, org_id, ticket_id, kind text CHECK IN ('created','viewed','assigned','completed','cancelled','comment','note','file_added'), actor_user_id NULL, body text NOT NULL DEFAULT '', data jsonb NOT NULL DEFAULT '{}', tenant_visible boolean NOT NULL, created_at)`; index `(ticket_id, created_at)`. Plan 04 extends the `kind` CHECK.
- `maintenance_pending_files(id, org_id, file_id REFERENCES files, user_id REFERENCES users, created_at)`.

- [ ] **Step 1:** Write migration; run `uv run --locked task test SUITE=integration PACKAGES=./internal/db/...` → PASS.
- [ ] **Step 2:** Commit `feat(db): maintenance tickets schema`.

### Task 2: Ticket domain and store

**Files:**
- Create: `internal/property/maintenance.go`, `internal/property/maintenance_events.go`
- Test: `internal/property/maintenance_integration_test.go`; add `testkit` builder `func (e *Env) Ticket(org core.Org, propertyID int64, opts ...TicketOption) property.Ticket` in `internal/testkit/records.go`

**Interfaces:**
- Produces:
  ```go
  type Ticket struct { ID, OrgID, PropertyID int64; Ref string; LeaseID, RaisedByContactID, RaisedByUserID, SupplierContactID *int64
      Title, Description, Category, Urgency, Status, AccessNotes, CancelReason string
      ViewedAt, CompletedAt, CancelledAt *time.Time; CreatedAt time.Time }
  type TicketEvent struct { ID int64; Kind, Body string; Data map[string]any; ActorUserID *int64; TenantVisible bool; CreatedAt time.Time }
  type NewTicket struct { PropertyID int64; LeaseID *int64; ContactID *int64; UserID int64
      Title, Description, Category, Urgency, AccessNotes string; PendingFileIDs []int64 }
  func CreateTicket(ctx, tx pgx.Tx, orgID int64, in NewTicket, today time.Time) (Ticket, error) // validates, active-lease rule when ContactID set, attaches pending files, 'created' event (tenant_visible)
  func GetTicket(ctx, q core.Querier, orgID, id int64) (Ticket, error)
  func ListTickets(ctx, q core.Querier, orgID int64, f TicketFilter) ([]TicketSummary, error) // TicketFilter{PropertyID *int64; LeaseIDs []int64; OpenOnly bool}
  func MarkViewed(ctx, tx pgx.Tx, orgID, id, userID int64) (Ticket, error)       // idempotent; only from 'new'
  func AssignSupplier(ctx, tx pgx.Tx, orgID, id, userID, supplierID int64) (Ticket, error) // supplier must be kind 'supplier' in org → else 422; new|viewed → assigned; assigned|scheduled keep status
  func CompleteTicket(ctx, tx pgx.Tx, orgID, id, userID int64, note string) (Ticket, error)
  func CancelTicket(ctx, tx pgx.Tx, orgID, id, userID int64, reason string, byTenant bool) (Ticket, error) // byTenant: only new|viewed
  func AddTicketEvent(ctx, tx pgx.Tx, orgID, ticketID int64, ev TicketEvent) (TicketEvent, error) // comment(tenant_visible)|note(internal)
  func AttachTicketFile(ctx, tx pgx.Tx, orgID, ticketID, fileID, userID int64) error // enforces 20-file cap; 'file_added' event tenant_visible
  func ListTicketEvents(ctx, q core.Querier, orgID, ticketID int64, tenantView bool) ([]TicketEvent, error)
  func ListTicketFiles(ctx, q core.Querier, orgID, ticketID int64) ([]core.File, error)
  ```
  Every mutation also writes `core.Audit(…, "maintenance_ticket", id, <action>, …)`.

- [ ] **Step 1: Failing tests:** create by tenant (event `created`, ref `R-0001`, second `R-0002`); refs independent per org; ended lease → `ErrInvalidInput` "lease is not active"; `TestMarkViewedConcurrent` (two tx goroutines → one `viewed` event); MarkViewed on `assigned` is a no-op; assign non-supplier → `ErrInvalidInput`; tenant cancel on `assigned` → `ErrInvalidState`; complete then comment → `ErrInvalidState`; 21st file → `ErrInvalidInput` "too many files"; `ListTicketEvents(tenantView=true)` excludes `note`; pending file owned by another user → `ErrNotFound`.
- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3: Implement.** Ref via `UPDATE orgs SET maintenance_seq = maintenance_seq + 1 WHERE id=$1 RETURNING maintenance_seq`. Viewed via `UPDATE … SET status='viewed', viewed_at=now() WHERE status='new' RETURNING` (event only when a row returns). Pending attach: `DELETE FROM maintenance_pending_files WHERE org_id=$1 AND user_id=$2 AND file_id = ANY($3) RETURNING file_id`, count must match.
- [ ] **Step 4:** PASS. Commit `feat(property): maintenance ticket domain`.

### Task 3: Media upload, download and pending clean-up

**Files:**
- Create: `internal/property/maintenance_files.go`, `internal/property/files_gc.go`
- Modify: `internal/scheduler/scheduler.go` (call `property.PrunePendingFiles(ctx, j.Pool, j.Store, now)` every tick when property enabled; add `Store storage.Store` to `Jobs`), `cmd/shigoto/main.go`
- Test: `internal/property/maintenance_files_integration_test.go`, `internal/scheduler/scheduler_integration_test.go`

**Interfaces:**
- Produces:
  ```go
  func SavePendingFile(ctx, tx pgx.Tx, orgID, userID int64, f core.File) (pendingID int64, err error) // used as HandleUpload onSaved; 20-cap per user's open pending set
  func PrunePendingFiles(ctx context.Context, pool *pgxpool.Pool, store storage.Store, now time.Time) error
  var fileReferences = []struct{ Table, Column string }{…} // every FK to files(id)
  ```

- [ ] **Step 1: Failing tests:** `TestFileReferencesCoverAllForeignKeys` (query `pg_constraint` for FKs to `files`; set equals `fileReferences` + `maintenance_pending_files.file_id`); `TestPrunePendingFiles` (pending at now−25h unreferenced → row and blob deleted; pending at now−25h whose file is also attached to an expense → pending row deleted, file kept; now−1h kept); media fixtures for jpeg, heic, mov, mp4, webm audio, m4a, mp3 accepted under `MediaPolicy`; `.exe` rejected 422.
- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3: Implement.** Delete `files` row only when no reference exists (single `DELETE … WHERE NOT EXISTS (…)` built from `fileReferences`), then `store.Delete` (blob dedupe is per `(org, sha)`, so a deleted row means no other row uses that blob). Errors logged per file, not fatal.
- [ ] **Step 4:** PASS. Commit `feat(property): maintenance media uploads and pending clean-up`.

### Task 4: HTTP routes (staff and portal) with YAML isolation cases

**Files:**
- Create: `internal/property/routes_maintenance.go`, `internal/portal/maintenance.go`
- Modify: `internal/property/routes.go` (register), `internal/portal/routes.go`, casefile harness (`tickets:` seed `{org, property, lease, raised_by_contact, status, note: <internal text>}`)
- Test: `internal/httpapi/testdata/cases/maintenance.yaml`, `internal/httpapi/testdata/cases/portal_maintenance.yaml`, additions to `modules_disabled.yaml`

**Interfaces (JSON shapes; `ticket` = `{id, ref, property_id, property_address, lease_id, title, description, category, urgency, status, supplier:{id,name}|null, access_notes, created_at, viewed_at, completed_at, cancelled_at}`):**
- Staff (`RequireOrg`; read=`property_read`, write=`property_write`):
  - `GET /api/v1/orgs/{org}/properties/{id}/maintenance?status=open|all` (read) → `[ticket + {raised_by_name, file_count}]`
  - `POST /api/v1/orgs/{org}/properties/{id}/maintenance` (write) `{lease_id?, title, description, category, urgency, access_notes}` → 201 ticket
  - `GET /api/v1/orgs/{org}/maintenance/{ticketId}` (read) → `{ticket, tenants:[{id,name,email,phone}], files:[FileResponse], events:[…all…]}`
  - `POST …/{ticketId}/viewed`, `PUT …/{ticketId}/supplier {supplier_contact_id}`, `POST …/{ticketId}/complete {note}`, `POST …/{ticketId}/cancel {reason}`, `POST …/{ticketId}/events {kind: "note"|"comment", body}`, `POST …/{ticketId}/files` (multipart, `MediaPolicy`) — all write
  - `GET …/{ticketId}/files/{fileId}?inline=1` (read; file must be attached to the ticket)
- Portal (`RequirePortal`, tenant only, property module enabled, ticket's lease must include caller via `LeaseHasContact`; else 404):
  - `GET /api/v1/portal/{org}/maintenance` → `{emergency_phone, leases:[{id, property_address, active}], tickets:[ticket]}`
  - `POST /api/v1/portal/{org}/maintenance/uploads` (multipart) → 201 `{pending_id, file:FileResponse}`
  - `DELETE /api/v1/portal/{org}/maintenance/uploads/{pendingId}` → 204 (own only)
  - `POST /api/v1/portal/{org}/maintenance` `{lease_id, title, description, category, urgency, access_notes, pending_file_ids}` → 201 ticket
  - `GET /api/v1/portal/{org}/maintenance/{id}` → `{ticket, files, events (tenant-visible only)}` — `supplier` exposes name only
  - `POST …/{id}/comments {body}`, `POST …/{id}/files` (multipart), `POST …/{id}/cancel {reason}`
  - `GET …/{id}/files/{fileId}?inline=1`

- [ ] **Step 1: Failing YAML cases** (`isolate: true` on writes; `exact: true` on denials): tenant create on own active lease 201; on co-tenant's lease (both listed) 201; on other lease 404; ended lease 422; client any route 404; beta tenant 404; tenant reads ticket of neighbour lease 404; portal detail of ticket with internal note `SENTINEL-INTERNAL` — assert via exact events list that it is absent; tenant fetches file of another ticket 404; bookkeeper GET staff detail 200, POST viewed 403; staff POST viewed then tenant sees status `viewed`; assign client contact as supplier 422; property module off → portal and staff maintenance routes 404.
- [ ] **Step 2:** `uv run --locked task test SUITE=integration PACKAGES=./internal/httpapi/...` → FAIL.
- [ ] **Step 3: Implement** handlers (thin: decode, call Task 2 functions in `module.RunInTx`, map errors with `module.WriteCoreError`). Request body limits via existing `ReadJSON`.
- [ ] **Step 4:** PASS. Commit `feat(maintenance): staff and portal ticket APIs`.

### Task 5: Emergency phone setting

**Files:**
- Modify: `internal/core/settings.go` (`OrgSettings.EmergencyPhone`; validate: empty or 8–20 chars of digits, spaces, `+`, `(`, `)`, `-`), settings HTTP handler/response, `web/src/routes/[org]/settings/+page.svelte` (field "Emergency repairs phone" in the organisation group, hint "Shown to tenants who mark a repair as urgent"), `web/src/lib/test/editors/settings-page.yaml` (fill it)
- Test: `internal/core/settings_test.go`, `internal/httpapi/testdata/cases/settings.yaml`, settings page Vitest

- [ ] **Step 1: Failing tests:** valid `0400 123 456` and `+61 7 3000 0000` saved and returned; `call me` → 422; settings page saves the new field.
- [ ] **Step 2:** run → FAIL. **Step 3:** implement. **Step 4:** `task test`, `task test SUITE=integration`, `task web:test` → PASS.
- [ ] **Step 5:** Commit `feat(settings): emergency repairs phone`.

### Task 6: Staff Maintenance tab and ticket page

**Files:**
- Create: `web/src/routes/[org]/properties/[id]/maintenance/+page.svelte` (+ test), `web/src/routes/[org]/properties/[id]/maintenance/[ticketId]/+page.svelte` (+ test), `web/src/lib/modules/property/maintenance.ts` (types, labels for status/urgency/category), `web/src/lib/modules/property/MediaGallery.svelte` (+ test), dialogs `NewTicketDialog.svelte`, `AssignSupplierDialog.svelte`, `TicketNoteDialog.svelte` (kind note|comment), `TicketCloseDialog.svelte` (complete|cancel)
- Modify: `web/src/routes/[org]/properties/[id]/+layout.svelte:17-20` (add `{ href: \`${base}/maintenance\`, label: 'Maintenance' }` after Notes)
- Editors: `web/src/lib/test/editors/{maintenance-new-ticket,maintenance-assign,maintenance-note,maintenance-close}.yaml`, register in `editors.registry.ts`
- Docs: ledger rows for both routes; update `[org]/properties/[id]/+layout.svelte` row (adds Maintenance)

- [ ] **Step 1: Failing Vitest:** list shows ref, title, urgency badge, status `StatusBadge`, Open/All filter in URL (`?status=all`); EmptyState "No open repairs"; detail page POSTs `/viewed` once on load only when the session has `property_write` (not for bookkeeper); write actions hidden without `property_write`; gallery renders `<img>` for images, `<video controls preload="metadata">` for video, `<audio controls>` for audio, with `src` ending `?inline=1`; timeline marks internal notes "Internal"; tenant contact details shown.
- [ ] **Step 2:** `cd web && npm test -- --run` → FAIL.
- [ ] **Step 3: Implement** with existing components (`PageHeader` back link to the Maintenance tab, `DataTable`, `StatusBadge`, `Dialog`, `FormActions`, `Field`, `Select`). Supplier picker lists contacts `kind=supplier` via existing contacts API.
- [ ] **Step 4:** `uv run --locked task web:test` → PASS (including editor contract suite).
- [ ] **Step 5:** Commit `feat(web): staff property maintenance tab and ticket page`.

### Task 7: Dashboard repairs card

**Files:**
- Modify: `internal/property/routes.go` (`propertyDashboard.Repairs repairsSummary` json `repairs`: `{open_count int, items: [{ticket_id, property_id, ref, title, urgency, status, property_address}]}`, top 5 ordered: urgent first, then status `new`, then `viewed|assigned`, then oldest), `web/src/lib/modules/property/DashboardCard.svelte` (or new `RepairsCard.svelte` rendered alongside), dashboard tests
- Test: `internal/httpapi/testdata/cases/dashboard.yaml`, `internal/property/maintenance_integration_test.go`, `DashboardCard.test.ts`

- [ ] **Step 1: Failing tests:** ordering case (routine-new, urgent-assigned, soon-new → urgent-assigned, soon-new, routine-new); completed/cancelled excluded; card hidden when `open_count == 0`; items link to `/{org}/properties/{property_id}/maintenance/{ticket_id}`; bookkeeper sees card (read perm).
- [ ] **Step 2:** run → FAIL. **Step 3:** implement (single SQL query). **Step 4:** PASS.
- [ ] **Step 5:** Commit `feat(dashboard): open repairs card`.

### Task 8: Portal Repairs tab, report form and ticket page

**Files:**
- Create: `web/src/routes/portal/[org]/repairs/+page.svelte`, `web/src/routes/portal/[org]/repairs/new/+page.svelte`, `web/src/routes/portal/[org]/repairs/[id]/+page.svelte` (each + test), `web/src/lib/modules/property/MediaPicker.svelte` (+ test), `web/src/lib/modules/property/VoiceRecorder.svelte` (+ test), `web/src/lib/modules/property/uploadWithProgress.ts` (+ test)
- Modify: `web/src/routes/portal/+layout.svelte` (tenant tabs `Rent · Repairs · Invoices`)
- Editors: `web/src/lib/test/editors/portal-repair-new.yaml`, `portal-repair-comment.yaml`
- Docs: ledger rows for three portal routes; update portal layout row

**Interfaces:**
- `uploadWithProgress(url: string, file: Blob, name: string, onProgress: (fraction: number) => void, signal: AbortSignal): Promise<T>` — uses `XMLHttpRequest` (fetch has no upload progress) and sends the same `X-Shigoto-CSRF: 1` header and credentials as `lib/api.ts`; export it from `lib/api.ts` so all requests still go through that module.

- [ ] **Step 1: Failing Vitest:**
  - Repairs list: status badges, "Report a repair" button, EmptyState; ended-only leases → button disabled with "You can report repairs for a current lease".
  - New form: required title/description/category/urgency; selecting Urgent shows the banner with `tel:` link to the emergency phone (and the fallback copy when empty); `MediaPicker` exposes "Take photo or video" (`accept="image/*,video/*" capture="environment"`) and "Choose from library" (`accept="image/*,video/*,audio/*" multiple`); each file uploads immediately with a progress bar, Retry and Remove (Remove calls `DELETE …/uploads/{pendingId}`); **submit disabled while any upload is in flight**; 422 keeps all input and uploaded files; duplicate submit blocked; Cancel with edits confirms; navigating away with edits confirms.
  - `VoiceRecorder`: hidden when `MediaRecorder` is undefined; Record → Stop produces a Blob with the recorder's `mimeType`, preview `<audio>`, Use/Discard; max 5 minutes then auto-stop.
  - Ticket page: status badge, timeline (newest first), shared comments composer, add photos (same picker, attached directly), Cancel request button only when status `new|viewed`.
- [ ] **Step 2:** `cd web && npm test -- --run` → FAIL.
- [ ] **Step 3: Implement.** Mobile-first single column; touch targets ≥44px; dedicated route for the form per `ui-standards.md`; `useUnsavedChanges` for the form.
- [ ] **Step 4:** `uv run --locked task web:test` → PASS.
- [ ] **Step 5:** Commit `feat(portal): report and track repairs with media`.

### Task 9: End-to-end

**Files:** Create `web/e2e/maintenance.spec.ts`; fixtures `web/e2e/fixtures/leak.jpg`, `web/e2e/fixtures/voice.webm` (tiny, committed)

- [ ] **Step 1:** Spec: tenant reports urgent plumbing repair with a photo and audio file (banner visible) → staff opens property Maintenance tab, ticket shows media and status becomes Viewed → staff assigns a supplier and adds an internal note and a shared comment → tenant sees Viewed/Assigned on the timeline and the shared comment, not the internal note → dashboard shows the repair card.
- [ ] **Step 2:** `task web:e2e` → PASS.
- [ ] **Step 3:** Commit `test(e2e): maintenance ticket workflow`.
