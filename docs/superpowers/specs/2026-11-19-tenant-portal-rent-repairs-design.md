# Tenant portal: rent ledger, repairs, scheduling and entry — design

Date: 2026-11-19 · App: `apps/shigoto` · Status: draft for approval

## Intent

Give tenants a self-service portal that removes the two most common reasons they
chase the landlord: "what do I owe / can I have a statement?" and "what is
happening with my repair?". Tenants get 24/7 access to a transparent rent ledger
with downloadable statements and receipts, and can raise repairs from a phone
with photos, video and voice notes so issues are diagnosed correctly the first
time. Repairs land against the property in Shigoto, staff move them through a
visible lifecycle, and tenants can offer appointment windows, confirm visits and
grant or withdraw permission for a tradesperson to enter when they are out.

## Goals

1. **Rent ledger** (tenant portal): summary, full running-balance history,
   per-payment printable receipt, date-range statement as CSV and PDF
   (financial year by default). Staff can download the same statement.
2. **Repair tickets with media**: tenant raises a ticket against their current
   lease's property with photos, video and voice notes; staff see and manage it
   on the property and on the dashboard.
3. **Visible lifecycle**: New → Viewed → Assigned → Scheduled → Completed
   (+ Cancelled), with a tenant-visible timeline. Tenants check the page; no
   push/email alerts in this release.
4. **Scheduling and entry permission**: tenant availability windows, staff-booked
   visits, tenant confirm / request change, per-visit entry permission with
   access notes, withdrawable.
5. **File storage behind an interface** with disk as the only implementation, so
   a later S3-compatible backend changes no feature code.

## Non-goals (this release)

- Notifications of any kind (push, email, SMS). The event log is designed so
  they can be added later, but nothing is sent.
- Tradesperson/contractor logins. Suppliers remain contacts; staff record all
  progress on their behalf.
- S3 implementation (only the interface + disk implementation).
- Linking tickets to expenses/costs, quotes, invoices to tenants for damage.
- Media transcoding, HEIC→JPEG conversion, thumbnails, virus scanning.
- A sidebar "Maintenance" entry or org-wide maintenance list page.
- Repairs for `client` portal users (tenants only).

## Anti-goals (failure even if it "works")

- A tenant ever seeing a ledger, ticket or file from a lease they are not on,
  or a staff-only note. Portal isolation is absolute; denials stay 404.
- Entry permission silently carrying over to a visit time the tenant never
  agreed to.
- An urgent repair (burst pipe, gas, electrical, no hot water) being only
  "logged" with no prompt to phone.
- One large or slow upload filling the disk, exhausting memory, or degrading
  rent runs and the rest of the box.
- Statement totals that don't reconcile with Shigoto's own ledger/invoices.

## Constraints (from the codebase and repo rules)

- Go stdlib `net/http` server, Postgres via embedded migrations, Svelte 5 SPA
  with Tailwind/DaisyUI and the shared `lib/components`. All requests through
  `web/src/lib/api.ts`. Follow `docs/ui-standards.md` and `docs/testing.md`;
  add screen-ledger rows and editor contract YAML for every new screen/editor.
- Portal endpoints/types/layout stay separate from staff screens
  (`ui-standards.md:128-132`); portal guard is `RequirePortal`, missing
  membership or foreign record → 404, never 403.
- Every mutation writes `audit_log` via `core.Audit`; every query filters on
  `org_id`.
- Money is integer cents; dates are calendar dates in the org's location.
- No queue/worker exists; nothing here needs one.
- **Uploads today buffer whole files in memory** (`core.SaveFile` does
  `io.ReadAll`; `HandleUpload` uses `ParseMultipartForm`). Large media requires
  a streaming path.
- **Traefik v3.7 is in front with no explicit entrypoint timeouts**, so its
  default `readTimeout` (60s) will cut off slow mobile uploads of large video.
  The Go server sets only `ReadHeaderTimeout` (`cmd/shigoto/main.go:224`).
  Deployment needs a deliberate timeout decision (see "Left to the builder").
- `orgs.fy_start_month` already exists (default 7) — use it for FY defaults.

## Approaches considered

### Where maintenance lives

- **A. Inside the existing `property` module — chosen.** Tickets belong to a
  property and lease and use supplier contacts and the property page; they are
  enabled whenever properties are. Portal endpoints live in `internal/portal`
  alongside leases. Least gating and test surface.
- B. Separate `maintenance` module with its own `SHIGOTO_MODULES` flag. Cleaner
  if repairs were wanted without properties, but adds gating to the dashboard,
  property tab and portal, plus more tests. Rejected: no such use case.

### Statement PDF

- **A. Pure-Go PDF library in the server — chosen.** Keeps the single static
  binary; one fixed-layout document is easy to code in Go.
- B. Headless Chromium rendering HTML: reuses print CSS but adds ~300 MB, a
  browser attack surface and memory to the container. Rejected.
- C. External binary (Typst / wkhtmltopdf): extra runtime dependency;
  wkhtmltopdf is abandoned. Rejected.

### Delivery

One design, four independently shippable increments, in order:
1. Storage interface + streaming uploads.
2. Rent ledger and statements.
3. Repair tickets with media (staff + portal + dashboard).
4. Scheduling and entry permission.

## Decisions (agreed with the user)

- Staff record all tradesperson progress; contractors never log in.
- No notifications now; tenants check the ticket page.
- Per-payment receipt is a printable HTML page the tenant opens and prints /
  "Save as PDF". Statement is a date range (FY default) exported as CSV **and**
  server-generated PDF.
- Upload size limit lifted for maintenance media (user will move to
  S3-compatible storage later); file storage goes behind an interface now.
- Urgent tickets show the org's emergency phone with "call, don't just log it".
- Scheduling flow: tenant offers windows → staff book one visit → tenant
  confirms or requests a change. Entry permission is per visit, withdrawable,
  and resets on reschedule.
- Maintenance appears only on the dashboard and under each property — no
  sidebar entry.
- Maintenance lives inside the `property` module; PDF via a pure-Go library.

## Design

### 1. File storage interface and streaming uploads

- New `internal/storage` (name is the builder's call) with a small interface,
  roughly: write a blob for `(orgID, sha256)` from a stream, open a blob for
  reading with seek support (needed for HTTP Range), stat/exists. Disk
  implementation keeps today's layout `<FilesDir>/<org_id>/<hex sha256>`,
  atomic temp-file + rename, mode 0600 — no migration of existing files.
- `core.SaveFile` becomes streaming: read the first 512 bytes to sniff, reject
  disallowed types before reading the rest, then stream the remainder through a
  SHA-256 hasher into a staging temp file and commit it under its hash. Never
  hold the whole file in memory.
- Upload handlers use `r.MultipartReader()` (stream the `file` part) instead of
  `ParseMultipartForm`, so large parts are not spooled twice.
- Allowed types and size limits become **per-purpose policies**:
  - Existing receipts/insurance/depreciation: unchanged (PDF/JPEG/PNG, 10 MiB).
  - Maintenance media: JPEG, PNG, HEIC/HEIF, MP4, QuickTime (MOV), WebM,
    MP4/M4A audio, WebM/Ogg audio, MP3. `http.DetectContentType` does not
    recognise MOV/HEIC/M4A, so add explicit ISO-BMFF `ftyp` brand sniffing.
    Size cap from new config `SHIGOTO_UPLOAD_MAX_BYTES` (default 1 GiB;
    `0` = unlimited). Max 20 files per ticket.
- Migration widens `files.size_bytes` to `bigint` (Go side `int64`).
- **Free-space guard:** uploads are refused (507) when free space on the files
  volume would drop below `SHIGOTO_UPLOAD_MIN_FREE_BYTES` (default 5 GiB).
  The disk backend implements the check; a future S3 backend can no-op it.
- **Pending uploads:** maintenance media uploaded before the ticket exists is
  recorded as pending (owner user, created_at). Submitting the ticket attaches
  it. The existing hourly scheduler deletes pending uploads older than 24 hours
  (row, and blob when no other `files` reference remains). No new worker.
- `module.ServeFile` gains an inline mode for allowlisted media types using
  `http.ServeContent` (Range requests — required for iOS video playback and
  seeking), keeping `X-Content-Type-Options: nosniff` and adding a restrictive
  `Content-Security-Policy: sandbox` on file responses. Attachment mode stays
  the default for documents.
- Existing call sites (recon files, property documents, expense receipts)
  switch to the interface with no behaviour change; their existing tests must
  pass unchanged.

### 2. Rent ledger and statements

**Who sees it (user decision):** every tenant currently listed in
`lease_tenants` for the lease sees the lease's full rent history, receipts and
statements — including invoices addressed to the primary tenant and payments
made by others — from the start of the lease, not only from when they joined.
A contact not on the lease gets 404. This replaces the portal Invoices tab's
`invoices.contact_id = caller` rule for lease-ledger data only; the Invoices tab
itself is unchanged.

**Balance model (decided):** the statement is a running account (charges minus
payments minus write-offs). It is not required to equal `GetLedger`'s
per-invoice floored figures; reconciliation tests assert that the closing
balance equals issued totals − payments − write-offs. A write-off appears as a
"Written off" credit for the unpaid remainder at write-off time. A migration
adds `invoices.written_off_on` (date) and `written_off_amount` (cents),
backfilled from `audit_log`; payments received after a write-off appear as
normal credits. Statements carry "generated on …" because undoing a
reconciliation match deletes a payment and can change past figures.

**Which records belong to a lease's ledger:** invoices linked via
`rent_periods.invoice_id` (rent plus recurring charge lines on those invoices)
and `leases.opening_arrears_invoice_id`; payments recorded against those
invoices. Bond invoices are excluded (bond is held, not rent) — *my call*.
Only issued invoices (open, paid, uncollectible); draft/void excluded, matching
the portal's existing invoice rule.

**Entries:** debit = invoice total, dated by invoice due date, described as
"Rent 14–20 Nov" (+ charge names) or "Opening arrears"; credit = each payment,
dated by payment date, with method; write-offs appear as a "Written off"
credit (see Balance model). Running balance, newest
first on screen, oldest first in exports.

**Summary:** paid-to date, owing now (overdue outstanding), next amount due and
date — computed from the existing `GetLedger`/`Ledger` logic, not re-derived.

**Statement export:** `from`/`to` dates; default the current FY using
`orgs.fy_start_month`, quick picks for the previous two FYs, custom range.
Opening balance = net of all entries before `from`; closing balance at `to`.
- CSV: date, description, debit, credit, balance (+ header rows for org, tenant,
  property, period, opening and closing balance).
- PDF: org name/ABN, tenant name(s), property address, lease dates, period,
  opening balance, lines, totals, closing balance, generated-at timestamp.
  Built server-side with a pure-Go PDF library (A4).
- Filenames like `rent-statement-12-banksia-st-FY2026-27.pdf`.

**Receipt page:** printable portal route per payment: org, tenant, property,
amount, date, method, reference, invoice(s) paid, balance after. Print CSS
reuses the rental-report approach.

**Endpoints** (shapes are the builder's call):
- Portal (`RequirePortal`, tenant only; lease must include the caller's
  `contact_id` via `lease_tenants`, else 404): lease ledger + summary; payment
  receipt; statement CSV; statement PDF.
- Staff (`RequireOrg(property_read)`): statement CSV and PDF for a lease, linked
  from the lease page.

**Portal UI:** tabs become **Rent · Repairs · Invoices** for tenants
(clients keep Invoices only). The Rent tab absorbs the Leases tab;
`/portal/{org}/leases` redirects to Rent. Lease selector appears only when the
tenant has more than one lease (current first; past leases remain viewable).
"Show earlier" pagination on history.

### 3. Repair tickets with media

**Data model** (new migration; names are the builder's call):
- `maintenance_tickets`: `org_id`, per-org human ref (`R-0042`), `property_id`,
  `lease_id` (nullable for staff-created tickets on a vacant property),
  `raised_by_contact_id` (nullable when staff-raised), `raised_by_user_id`,
  title, description, category (plumbing, electrical, appliance,
  heating/cooling, doors/windows/locks, pests, garden/exterior, other),
  urgency (`routine` | `soon` | `urgent`), status (`new` | `viewed` |
  `assigned` | `scheduled` | `completed` | `cancelled`), `supplier_contact_id`
  (must be a supplier contact in the org), `access_notes`, timestamps
  (`viewed_at`, `completed_at`, `cancelled_at`), cancel reason.
- `maintenance_ticket_files`: ticket ↔ `files`, uploaded-by user, created_at.
- `maintenance_ticket_events`: append-only timeline — kind (created, viewed,
  assigned, visit_booked, visit_confirmed, visit_change_requested,
  visit_rescheduled, visit_cancelled, entry_granted, entry_withdrawn,
  completed, cancelled, comment, note, file_added), actor user, optional body,
  JSON data, `tenant_visible` flag, created_at. This is also the future
  notification source.

**Rules:**
- A tenant may raise a ticket only for a lease that includes them and is
  active today (started, not ended/terminated). Otherwise 404/422 as per the
  existing portal conventions.
- All tenants on a lease see that lease's tickets (co-tenants share) — *my call*.
- Tenants see only `tenant_visible` events: status changes, visits, shared
  comments. Staff internal notes are never returned by portal endpoints.
- **After submitting**, tenants can post shared comments and add media until
  the ticket is completed or cancelled. They cannot edit title, description,
  category or urgency (corrections go in a comment).
- **Staff permissions:** reading tickets, files and timeline needs
  `property_read`; every staff mutation (assign, visits, notes, comments,
  complete, cancel, staff-raised tickets) needs `property_write`.
- **Viewed**: the staff ticket page sends an explicit POST when opened; first
  call by a staff user with `property_write` (bookkeepers excluded) sets `viewed_at` and logs the
  event; later calls are no-ops. (Explicit POST rather than a GET side effect
  so it is CSRF-guarded and testable — *my call*.)
- **Assigned**: staff set the supplier → status `assigned` (if not already
  scheduled).
- **Completed / Cancelled**: staff only, with optional note. A tenant may
  cancel their own ticket while it is `new` or `viewed` — *my call*.
- Staff can raise a ticket on a tenant's behalf (phone-in urgent repairs)
  — *my call*.
- Urgency `urgent` in the portal form shows a banner: "Burst pipe, gas, no
  power or hot water, or unsafe? Call {emergency phone} now, then log it here."
  New org setting `emergency_phone` on the organisation settings page (owner /
  `org_admin`). If unset, the banner still says to phone the property manager
  and staff settings show a hint to set it.

**Media capture (portal form):** separate "Take photo/video" (camera capture)
and "Choose from library" inputs; a "Record voice note" button using
`MediaRecorder` (format chosen by the browser: WebM/Opus on Chrome/Android,
MP4/AAC on iOS Safari). Each file uploads as it is added with per-file
progress, retry and remove; the ticket submit is blocked while uploads are in
flight. Failed input is preserved per the editor rules.

**Staff UI:**
- Property page gains a **Maintenance** tab (alongside Bills / Depreciation /
  Notes): list with ref, issue, urgency, status, visit, entry badge; filters
  Open / All.
- Ticket detail is a dedicated route under the property (review-a-record
  layout per `ui-standards.md`, rather than the side panel in the mockup — *my
  call*): media gallery (inline images, video and audio players), tenant and
  lease, preferred windows, entry permission panel, timeline, internal notes
  vs shared comments. Actions open dialogs: assign supplier, book/reschedule
  visit, cancel visit, complete, cancel ticket, add note/comment, add files.
- **Dashboard:** the property module's existing dashboard section adds a
  "Repairs" card: open tickets ordered urgent first, then `new`, then awaiting
  scheduling, top few with links; hidden when none open. Shown only to roles
  with `property_read` (existing dashboard gating).

**Portal UI:** Repairs tab with list (status badges, next visit);
"Report a repair" dedicated route (complex form); ticket detail with status,
current visit card, entry permission, timeline, shared comments, media.

### 4. Scheduling and entry permission

- `maintenance_ticket_availability`: tenant-offered windows — date + `morning`
  (8–12) / `afternoon` (12–5) / custom start–end. Editable by the tenant until
  a visit is confirmed.
- `maintenance_visits`: ticket, supplier contact, `starts_at`, `ends_at`,
  status (`booked` | `confirmed` | `change_requested` | `cancelled` | `done`),
  tenant confirmation timestamp, change-request note, entry mode
  (`tenant_home` | `permitted` | unset), entry granted at/by, entry withdrawn
  at. At most one open (booked/confirmed/change_requested) visit per ticket;
  several visits over a ticket's life are allowed (quote, then repair).
- Staff book a visit → ticket `scheduled`. Tenant sees it and can **Confirm**
  or **Request change** (note + new windows) → staff reschedule.
- **Entry permission** is attached to one visit. A choice made while raising
  the ticket is only a stored *preference*: it pre-fills the visit card but is
  not in force. Permission takes effect only when the tenant confirms that
  specific visit ("Confirm time & allow entry"). Until then staff see
  "Entry: not yet confirmed". It records who granted it and when. The
  tenant can withdraw it until the visit's start time. **Rescheduling resets it**
  and the tenant is asked again. Staff see the entry state and access notes
  prominently on the visit and in the list.
- Visit completion: staff mark the visit `done`; marking the ticket completed is
  a separate action (a visit may only produce a quote).
- Times stored as timestamptz, shown in the org's location.

## Testing (per `docs/testing.md`)

- Go HTTP YAML cases: portal isolation for ledger, receipts, statements,
  tickets, files and visits (other tenant / client / staff-only notes → 404),
  ended-lease create denial, staff permission matrix
  (`property_read` vs `property_write`, bookkeeper), module gating
  (property module off → routes absent, portal Repairs/Rent hidden),
  upload limits and type rejection, Range responses.
- Integration tests: ledger/statement reconciliation against `GetLedger`
  (opening/closing balances, write-offs, FY boundaries using `fy_start_month`),
  ticket status transitions, entry reset on reschedule, viewed idempotency,
  streaming upload without full buffering, storage interface contract test
  run against the disk implementation.
- Vitest + editor contract YAML for every new editor/dialog (raise repair,
  availability, visit booking, entry permission, comments, emergency phone
  setting). Playwright: tenant raises repair with a photo and voice file,
  staff assigns/books, tenant confirms and grants entry; tenant downloads
  statement.
- Screen-ledger rows in `docs/ui-standards.md` for each new route.

## Left to the builder

- Exact package, table, column and endpoint names; JSON shapes.
- Choice of pure-Go PDF library (must be actively maintained, permissive
  licence, no cgo); PDF layout details.
- Storage interface method set, as long as it supports streaming writes,
  seekable reads and needs no feature-code changes for a future S3 backend.
- Ticket ref generation mechanism (per-org sequence/counter).
- Exact category list wording; availability window hours.
- **Upload timeouts:** set an explicit server-side read/idle policy for upload
  routes and raise or scope Traefik's entrypoint `readTimeout` for Shigoto in
  the networking role (repo change outside the app; must stay idempotent and
  not loosen other services without reason). Document the chosen value.
- Disk-usage visibility (e.g. log/metric of files dir size) is welcome but
  optional.
