# Scheduling and Entry Permission Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Tenants offer availability windows and an entry preference; staff book a visit with a supplier; tenants confirm (optionally granting entry) or request a change; entry permission is tied to one confirmed visit, withdrawable until it starts, and reset by any reschedule.

**Architecture:** Two new tables (`maintenance_availability`, `maintenance_visits`) plus `maintenance_tickets.entry_preference`, all in the property module. Visit state changes write `maintenance_events` (tenant-visible) so the plan 03 timeline shows them.

**Tech Stack:** Go, pgx, Postgres, SvelteKit 5, DaisyUI.

**Spec:** `docs/superpowers/specs/2026-11-19-tenant-portal-rent-repairs-design.md` §4

**Depends on:** plan 03 (tickets, events, routes, portal/staff ticket pages).

## Global Constraints

- Availability window: `date` + `slot` ∈ `morning` (08:00–12:00), `afternoon` (12:00–17:00), `custom` (`start_time`, `end_time`, start < end). Max 10 windows per ticket. Dates must be ≥ today. Editable by tenant only while the ticket has no `confirmed` visit and is not terminal.
- Visit statuses: `booked | confirmed | change_requested | cancelled | done`. Open = `booked|confirmed|change_requested`; at most one open visit per ticket (partial unique index).
- Booking a visit sets ticket `scheduled` and requires a supplier (from the request or the ticket's). Cancelling the open visit returns the ticket to `assigned`. `done` leaves ticket status unchanged (completion is separate).
- Entry modes: `tenant_home | permitted`. Ticket `entry_preference` (nullable) is a **preference only**: it pre-fills the tenant's confirm choice and is shown to staff as "Preference: …", never as permission.
- Entry is **in force** only when visit `status = confirmed AND entry_mode = 'permitted' AND entry_withdrawn_at IS NULL`. Staff UI shows exactly one of: "Entry permitted", "Tenant will be home", "Entry withdrawn", "Entry: not yet confirmed".
- Reschedule (`starts_at`/`ends_at` change) → visit back to `booked`, clears `tenant_confirmed_at`, `entry_mode`, `entry_granted_at/by`, `entry_withdrawn_at`.
- Tenant may withdraw entry only while `now < starts_at` → else 422 `{"error":"visit has started"}`.
- Times stored `timestamptz`; input/display in the org's configured location (`cfg.Location`).
- Event kinds added: `availability_updated, visit_booked, visit_confirmed, visit_change_requested, visit_rescheduled, visit_cancelled, visit_done, entry_granted, entry_withdrawn` — all `tenant_visible = true`.

## Review Focus

- Tenant confirms with entry, staff then reschedules → staff now see "Entry: not yet confirmed" and tenant is asked again (Task 2 `TestRescheduleResetsEntry`, Task 4 Vitest).
- Tenant tries to withdraw entry 5 minutes after the visit start → 422 with clear message (Task 2 `TestWithdrawAfterStart`).
- Visit times across DST/timezone: a 09:00 Brisbane booking renders as 09:00 to the tenant regardless of browser timezone (Task 3 YAML asserts ISO offset; Task 5 Vitest renders with a non-Brisbane TZ).
- Two staff book a visit for the same ticket at once → one succeeds, the other 409 (Task 2 `TestOneOpenVisit`).
- Request change with no new windows → allowed (note required instead), so tenants aren't forced to guess dates (Task 2 `TestRequestChangeNoteOnly`).

---

### Task 1: Schema

**Files:** Create `internal/db/migrations/0023_maintenance_visits.sql`

- `maintenance_tickets ADD COLUMN entry_preference text CHECK (entry_preference IN ('tenant_home','permitted'))`.
- `maintenance_availability(id, org_id, ticket_id, date date, slot text CHECK IN ('morning','afternoon','custom'), start_time time NULL, end_time time NULL, created_at; CHECK (slot <> 'custom' OR (start_time IS NOT NULL AND end_time IS NOT NULL AND start_time < end_time)))`.
- `maintenance_visits(id, org_id, ticket_id, supplier_contact_id REFERENCES contacts, starts_at timestamptz, ends_at timestamptz CHECK (ends_at > starts_at), status text CHECK IN (…), tenant_confirmed_at timestamptz NULL, change_note text NOT NULL DEFAULT '', entry_mode text NULL CHECK IN ('tenant_home','permitted'), entry_granted_at timestamptz NULL, entry_granted_by_user_id NULL REFERENCES users, entry_withdrawn_at timestamptz NULL, created_at)`; `CREATE UNIQUE INDEX … ON maintenance_visits(ticket_id) WHERE status IN ('booked','confirmed','change_requested')`.
- Replace `maintenance_events.kind` CHECK with the extended list.

- [ ] **Step 1:** Write migration; `uv run --locked task test SUITE=integration PACKAGES=./internal/db/...` → PASS.
- [ ] **Step 2:** Commit `feat(db): maintenance visits and availability`.

### Task 2: Visit and entry domain

**Files:**
- Create: `internal/property/maintenance_visits.go`
- Modify: `internal/property/maintenance.go` (`NewTicket` gains `EntryPreference *string` and `Availability []AvailabilityWindow`; `CreateTicket` stores both)
- Test: `internal/property/maintenance_visits_integration_test.go`

**Interfaces:**
- Produces:
  ```go
  type AvailabilityWindow struct { Date time.Time; Slot string; Start, End *string /* "HH:MM" */ }
  type Visit struct { ID, TicketID, SupplierContactID int64; StartsAt, EndsAt time.Time; Status string
      TenantConfirmedAt *time.Time; ChangeNote string; EntryMode *string; EntryGrantedAt, EntryWithdrawnAt *time.Time }
  func (v Visit) EntryState() string // "permitted" | "tenant_home" | "withdrawn" | "unconfirmed"
  func SetAvailability(ctx, tx pgx.Tx, orgID, ticketID, userID int64, ws []AvailabilityWindow, today time.Time) error // replaces set
  func ListAvailability(ctx, q core.Querier, orgID, ticketID int64) ([]AvailabilityWindow, error)
  func SetEntryPreference(ctx, tx pgx.Tx, orgID, ticketID int64, pref *string) error
  func BookVisit(ctx, tx pgx.Tx, orgID, ticketID, userID int64, supplierID *int64, starts, ends time.Time) (Visit, error)
  func RescheduleVisit(ctx, tx pgx.Tx, orgID, visitID, userID int64, starts, ends time.Time) (Visit, error)
  func CancelVisit(ctx, tx pgx.Tx, orgID, visitID, userID int64, reason string) (Visit, error)
  func MarkVisitDone(ctx, tx pgx.Tx, orgID, visitID, userID int64) (Visit, error)
  func ConfirmVisit(ctx, tx pgx.Tx, orgID, visitID, userID int64, entryMode string) (Visit, error) // booked|change_requested → confirmed
  func RequestVisitChange(ctx, tx pgx.Tx, orgID, visitID, userID int64, note string, ws []AvailabilityWindow, today time.Time) (Visit, error) // note required; ws optional replaces availability
  func WithdrawEntry(ctx, tx pgx.Tx, orgID, visitID, userID int64, now time.Time) (Visit, error)
  func OpenVisit(ctx, q core.Querier, orgID, ticketID int64) (*Visit, error)
  func ListVisits(ctx, q core.Querier, orgID, ticketID int64) ([]Visit, error)
  ```
  Each mutation writes its event and `core.Audit`.

- [ ] **Step 1: Failing tests:** `TestBookVisitSchedulesTicket`; `TestBookVisitRequiresSupplier` (422); `TestOneOpenVisit` (second concurrent book → `ErrInvalidState`); `TestConfirmWithEntry` (`EntryState()=="permitted"`, granted fields set); `TestPreferenceIsNotPermission` (ticket preference `permitted`, booked visit → `EntryState()=="unconfirmed"`); `TestRescheduleResetsEntry`; `TestWithdrawBeforeStart` / `TestWithdrawAfterStart`; `TestCancelVisitReturnsAssigned`; `TestRequestChangeNoteOnly`; `TestAvailabilityPastDateRejected`; `TestAvailabilityLockedAfterConfirm`; 11 windows → 422; all visit ops on terminal ticket → `ErrInvalidState`; completing ticket with an open visit cancels that visit (event `visit_cancelled`).
- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3: Implement.** Map unique-index violation on the partial index to `core.ErrInvalidState`. Update `CompleteTicket`/`CancelTicket` (plan 03) to cancel the open visit in the same tx.
- [ ] **Step 4:** PASS. Commit `feat(property): maintenance visits and entry permission`.

### Task 3: HTTP routes and YAML cases

**Files:**
- Modify: `internal/property/routes_maintenance.go`, `internal/portal/maintenance.go`; extend ticket detail responses
- Test: `internal/httpapi/testdata/cases/maintenance.yaml`, `portal_maintenance.yaml`

**Interfaces:**
- `visit` JSON: `{id, supplier:{id,name}, starts_at, ends_at (RFC3339 with org offset), status, tenant_confirmed_at, change_note, entry_state, entry_granted_at, entry_withdrawn_at}`. Ticket detail (both sides) adds `entry_preference, availability:[{date, slot, start, end}], open_visit: visit|null, visits:[visit]`. Staff list rows add `open_visit: {starts_at, ends_at, entry_state}|null`.
- Staff (write): `POST /api/v1/orgs/{org}/maintenance/{ticketId}/visits {supplier_contact_id?, starts_at, ends_at}`; `PUT …/visits/{visitId} {starts_at, ends_at}`; `POST …/visits/{visitId}/cancel {reason}`; `POST …/visits/{visitId}/done`.
- Portal: `PUT /api/v1/portal/{org}/maintenance/{id}/availability {windows}`; `PUT …/{id}/entry-preference {entry_preference|null}`; `POST …/{id}/visits/{visitId}/confirm {entry_mode}`; `POST …/{id}/visits/{visitId}/request-change {note, windows?}`; `POST …/{id}/visits/{visitId}/withdraw-entry`. Portal `POST /maintenance` accepts `entry_preference` and `availability`.
- Portal visit JSON hides `entry_granted_by_user_id` and supplier contact details other than name.

- [ ] **Step 1: Failing YAML cases:** visit on neighbour's ticket via own ticket id → 404 exact; confirm visit belonging to a different ticket id in path → 404; bookkeeper book → 403; staff book then tenant detail shows `open_visit.entry_state: unconfirmed`; confirm with `permitted` → staff list shows `entry_state: permitted`; reschedule → staff detail `unconfirmed`; invalid `entry_mode` → 422.
- [ ] **Step 2:** `uv run --locked task test SUITE=integration PACKAGES=./internal/httpapi/...` → FAIL.
- [ ] **Step 3:** implement. **Step 4:** PASS.
- [ ] **Step 5:** Commit `feat(maintenance): visit and entry APIs`.

### Task 4: Staff UI

**Files:**
- Create: `web/src/lib/modules/property/VisitDialog.svelte` (book + reschedule; supplier select, date, start/end time), `web/src/lib/modules/property/EntryPanel.svelte` (+ tests)
- Modify: staff ticket page and Maintenance list (plan 03 files) — Visit section with Book/Reschedule/Cancel visit/Mark visit done; list "Visit" column shows date window and entry badge; availability list shown beside the booking button
- Editors: `web/src/lib/test/editors/maintenance-visit.yaml`, registry entry
- Docs: update ledger rows for the two staff maintenance routes

- [ ] **Step 1: Failing Vitest:** EntryPanel renders each of the four states with distinct text (not colour alone) and shows `access_notes` prominently when `permitted`; preference rendered as "Tenant preference: …" only when state is `unconfirmed`; VisitDialog pre-fills supplier from ticket, rejects end ≤ start client-side, preserves input on 422/409; reschedule dialog warns "Rescheduling clears the tenant's confirmation and entry permission."
- [ ] **Step 2:** `cd web && npm test -- --run` → FAIL. **Step 3:** implement. **Step 4:** `uv run --locked task web:test` → PASS.
- [ ] **Step 5:** Commit `feat(web): staff visit booking and entry panel`.

### Task 5: Portal UI

**Files:**
- Create: `web/src/lib/modules/property/AvailabilityEditor.svelte`, `web/src/lib/modules/property/VisitCard.svelte`, `web/src/lib/modules/property/RequestChangeDialog.svelte` (+ tests)
- Modify: `web/src/routes/portal/[org]/repairs/new/+page.svelte` (add "Times that suit you (optional)" via AvailabilityEditor and "Access" segmented choice "I'll be home" / "OK to enter if I'm out" stored as `entry_preference`, plus access notes), `web/src/routes/portal/[org]/repairs/[id]/+page.svelte` (VisitCard; availability editor while editable)
- Editors: `portal-request-change.yaml`, update `portal-repair-new.yaml`
- Docs: update portal repairs ledger rows

- [ ] **Step 1: Failing Vitest:**
  - AvailabilityEditor: add up to 10 windows; past dates disabled; custom requires start < end; remove window.
  - VisitCard (`booked`): shows date/time in org timezone (test runs with `TZ=America/New_York` and expects Brisbane-local text), supplier name, two actions: **"Confirm time"** and **"Request change"**; entry choice radio "I'll be home" / "OK to enter if I'm out" pre-filled from preference; confirming with "OK to enter" sends `entry_mode: permitted`.
  - VisitCard (`confirmed` + permitted, before start): "You've allowed entry if you're out" with **Withdraw** (confirm prompt); after start the Withdraw button is absent.
  - VisitCard (`change_requested`): "Change requested — waiting for your property manager".
  - RequestChangeDialog: note required; windows optional; preserves input on 422; duplicate submit blocked.
- [ ] **Step 2:** `cd web && npm test -- --run` → FAIL. **Step 3:** implement. **Step 4:** `uv run --locked task web:test` → PASS.
- [ ] **Step 5:** Commit `feat(portal): availability, visit confirmation and entry permission`.

### Task 6: End-to-end and docs

**Files:** Modify `web/e2e/maintenance.spec.ts`; `docs/ui-standards.md` (final ledger check); README feature list

- [ ] **Step 1:** Extend spec: tenant raises repair with two windows and "OK to enter" preference → staff sees "Entry: not yet confirmed" and "Tenant preference: OK to enter" → staff books visit → tenant confirms with entry → staff list shows "Entry permitted" → staff reschedules → tenant sees confirmation requested again; staff shows not yet confirmed → tenant requests change with a note → staff sees change note.
- [ ] **Step 2:** `task web:e2e` → PASS.
- [ ] **Step 3:** Full `uv run --locked task verify` (sequential) → PASS.
- [ ] **Step 4:** Commit `test(e2e): visit scheduling and entry permission`.
