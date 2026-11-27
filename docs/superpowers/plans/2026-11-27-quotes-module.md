# Quotes Module Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Staff build itemised quotes (labour + material lines, internal attachments), send a frozen PDF, and accept a quote into a fixed-price project with one task per labour line; projects gain a Costs tab and clients see sent quotes in the portal.

**Architecture:** New Go package `internal/quotes` registered as module `quotes` (requires `timesheets`), owning quotes, lines, attachments, numbering, the PDF, the accept transaction and the project costs endpoint. `internal/timesheets` allows a null hourly rate on fixed-price projects. Core org settings gain a business profile (address, phone, email, logo, licences, default quote terms). `internal/portal` gains client quote routes. The Svelte SPA gets a quotes module (list, editor, detail), a project Costs tab, a settings Business profile section and portal quote views.

**Tech Stack:** Go stdlib `net/http`, pgx/Postgres embedded migrations, `github.com/signintech/gopdf`, Svelte 5 + Tailwind/DaisyUI, Vitest, Playwright.

**Spec:** `docs/superpowers/specs/2026-11-27-quotes-module-design.md`

## Global Constraints

- Read `docs/ui-standards.md` and `docs/testing.md` before UI or test work. Add every new route to the "Existing-screen coverage" ledger.
- Test each behaviour in its owning layer (`docs/testing.md` table): HTTP YAML cases for perms/isolation/gating/validation codes; `internal/testkit` integration tests for workflows and concurrency; editor YAML for editors.
- Money is integer cents (`core.Cents`); quantities are `quantity_milli`. GST per line via `core.LineAmount` + `core.GSTOnExclusive`; quote GST codes are `GST`, `FRE`, `N-T` only, and `GST` needs `orgs.gst_registered` (same rule and message as `core.CreateDraftInvoice`).
- Line bounds match invoices: `quantity_milli` 1..10,000,000, `unit_amount` 0..100,000,000,000.
- Every row has `org_id`; every query filters by it. Staff denials across orgs are 404. Portal denials are 404 and filter by principal `OrgID` **and** `ContactID`.
- Quote `status` stored values: `draft | sent | accepted | declined | superseded`. `expired` is never stored: the API reports `display_status = "expired"` for `sent` with `valid_until < today` (today = `module` org-local date, as `timesheets.today`).
- Number format: `Q-` + 4-digit zero-padded number; revisions >1 append `-R{n}` (`Q-0007`, `Q-0007-R2`).
- Line `kind`: `labour | material`. Labour: `quantity_milli` = hours×1000, `unit_amount` = hourly rate.
- Quote contacts must be `contacts.kind = 'client'` (reuse `timesheets` `checkClient` semantics; message `contact_id is not a client`).
- Quotes never touch `invoices`, `payments`, recon, P&L or BAS.
- Attachments: `core.DocumentPolicy`, staff-only, never in PDF or portal.
- Logo: PNG or JPEG, max 2 MiB.
- Editors: failed-input preservation, duplicate-submit protection, explicit Cancel, dirty-edit preservation, busy-dismissal blocking. API calls only via `web/src/lib/api.ts`.
- Migrations: `0027_project_rate_nullable.sql`, `0028_org_profile.sql`, `0029_quotes.sql`.
- Run Taskfile validation sequentially through `uv run --locked` (per `AGENTS.md`): `uv run --locked task test`, `... task test SUITE=integration`, `... task web:test`, `... task lint`, `... task web:e2e` (`go-task` if that is the binary). Steps below abbreviate these as `task ...`. Never point tests at operator/production databases.

## Review Focus

1. **Accept races** (two tabs click Accept): exactly one project/task set; second gets 409 `quote is not open`. → Task 5 `TestAcceptConcurrentOnlyOnce`.
2. **Revision sent after another revision was accepted**: send refused 409 `another revision of this quote was accepted`, nothing superseded. → Task 4 `TestSendRevisionAfterAcceptRefused`.
3. **Accepted project name or task name already taken**: suffix ` (Q-0007)` applied, accept succeeds; a second clash on the suffixed name still fails cleanly 422 without partial writes. → Task 5 `TestAcceptNameClashes`.
4. **Fixed project with null rate appearing in time entries, dashboard, billing run**: entries list shows rate 0, dashboard and billing run unaffected. → Task 1 `TestFixedProjectNullRateReaders`.
5. **Client guessing another contact's quote id, a draft's id, or a PDF url**: 404 with `{error: not found}` exact. → Task 8 cases in `portal_quotes.yaml`.

---

## File map

| Path | Responsibility |
|---|---|
| `internal/db/migrations/0027_project_rate_nullable.sql` | Nullable `projects.hourly_rate` + fixed-only check |
| `internal/db/migrations/0028_org_profile.sql` | Org profile columns |
| `internal/db/migrations/0029_quotes.sql` | Quote tables |
| `internal/timesheets/projects.go`, `routes.go`, `entries.go`, `model.go` | Null rate handling |
| `internal/core/settings.go`, `internal/httpapi/settings_routes.go` | Business profile + logo routes |
| `internal/property/files_gc.go` | Register `orgs.logo_file_id`, `quotes.pdf_file_id`, `quote_attachments.file_id` |
| `internal/config/config.go` | `quotes` module, requires `timesheets` |
| `internal/auth/rbac.go` | `quotes_read`, `quotes_write` |
| `internal/quotes/model.go` | Types, statuses, number formatting |
| `internal/quotes/quotes.go` | Draft create/update/get/list, decline, revise |
| `internal/quotes/send.go` | Mark sent: freeze PDF, supersede predecessor |
| `internal/quotes/pdf.go` | `WriteQuotePDF` |
| `internal/quotes/accept.go` | Accept → project + tasks |
| `internal/quotes/attachments.go` | Attachment add/list/delete |
| `internal/quotes/costs.go` | Project costs query |
| `internal/quotes/routes.go` | Staff HTTP routes, `Module()` |
| `internal/portal/quotes.go` | Client portal quote routes |
| `internal/httpapi/server.go` | Register `quotes.Module()` |
| `internal/httpapi/casefile/casefile.go` | `quotes` seed kind |
| `internal/httpapi/testdata/cases/quotes.yaml`, `portal_quotes.yaml`, `settings_profile.yaml` | HTTP cases |
| `internal/sandbox/quotes.go` | Sandbox seed |
| `web/src/lib/modules/quotes/*` | Manifest, types, `QuoteLineEditor`, `QuoteStatusBadge`, `AttachmentsPanel`, `ProjectCosts` |
| `web/src/routes/[org]/quotes/+page.svelte`, `new/+page.svelte`, `[id]/+page.svelte`, `[id]/edit/+page.svelte` | Staff quote screens |
| `web/src/routes/[org]/projects/[id]/+page.svelte` | Costs tab, null rate display |
| `web/src/routes/[org]/settings/+page.svelte` | Business profile section |
| `web/src/routes/portal/[org]/hub/+page.svelte`, `portal/[org]/projects/[id]/+page.svelte`, `portal/[org]/quotes/[id]/+page.svelte` | Portal quote views |
| `docs/ui-standards.md`, `README.md` | Ledger rows; README "Quotes" section |

---

### Task 1: Nullable hourly rate on fixed-price projects

**Files:**
- Create: `internal/db/migrations/0027_project_rate_nullable.sql`
- Modify: `internal/timesheets/model.go`, `projects.go`, `entries.go`, `routes.go`; `web/src/lib/modules/timesheets/ProjectDialog.svelte`, `types.ts`; `web/src/routes/[org]/projects/[id]/+page.svelte`
- Test: `internal/timesheets/projects_integration_test.go`, `internal/httpapi/testdata/cases/timesheets.yaml` (or the existing projects case file), `web/src/lib/test/editors/project-dialog.yaml`, `ProjectDialog.test.ts`

**Interfaces:**
- Produces: `Project.HourlyRate core.Cents` keeps its type; `0` means "no rate" and is stored as SQL `NULL`. `validateProject` allows `0` only when `BillingMethod == BillingFixed`. API `hourly_rate` is `number | null` in request and response.

- [ ] **Step 1: Write failing tests**

```go
func TestFixedProjectWithoutRate(t *testing.T) {
	// CreateProject(fixed, HourlyRate 0) succeeds; GetProject returns HourlyRate 0;
	// DB row hourly_rate IS NULL.
	// CreateProject(tm, HourlyRate 0) -> ErrInvalidInput "hourly_rate must be positive".
	// UpdateProject fixed->tm with HourlyRate 0 -> same error.
}

func TestFixedProjectNullRateReaders(t *testing.T) {
	// fixed project, rate 0, task without override, one 60-min entry.
	// ListEntries: entry Rate == 0, no scan error.
	// dashboardSection: no error, project absent (fixed excluded).
	// billing run preview: no lines for this project.
}
```

HTTP cases: `POST /projects` with `billing_method: fixed, hourly_rate: null` → 201 body `{hourly_rate: null}`; with `billing_method: tm, hourly_rate: null` → 422 `{error: "hourly_rate must be positive"}`.

- [ ] **Step 2: Run** `task test SUITE=integration` → FAIL (NOT NULL violation / validation).

- [ ] **Step 3: Migration**

```sql
ALTER TABLE projects ALTER COLUMN hourly_rate DROP NOT NULL;
ALTER TABLE projects ADD CONSTRAINT projects_rate_required_for_tm
    CHECK (billing_method = 'fixed' OR hourly_rate IS NOT NULL);
```

- [ ] **Step 4: Implement**
  - `scanProject`: scan into `*int64`, nil → 0.
  - Insert/update pass `NULLIF($n, 0)`.
  - `validateProject`: `HourlyRate < 0` invalid; `HourlyRate == 0 && BillingMethod != BillingFixed` → `hourly_rate must be positive`.
  - `entries.go:213`: `COALESCE(t.hourly_rate, p.hourly_rate, 0)`.
  - `routes.go`: `projectRequest.HourlyRate *int64`; `projectUpdateRequest.HourlyRate` becomes `json.RawMessage` (omitted → 422 `hourly_rate is required`, `null` → 0); `projectResponse.HourlyRate *int64` (nil when 0).
  - Web: `ProjectDialog` allows an empty Base rate when Billing is Fixed price (sends `null`), else shows `Enter an hourly rate.`; project page shows `—` for a null base rate.

- [ ] **Step 5: Run** `task test SUITE=integration && task web:test` → PASS.

- [ ] **Step 6: Commit** `feat(timesheets): allow fixed-price projects without an hourly rate`

---

### Task 2: Org business profile and logo

**Files:**
- Create: `internal/db/migrations/0028_org_profile.sql`, `internal/httpapi/testdata/cases/settings_profile.yaml`, `internal/core/testdata/licences.yaml`
- Modify: `internal/core/settings.go`, `internal/httpapi/settings_routes.go`, `internal/property/files_gc.go`, `web/src/routes/[org]/settings/+page.svelte`
- Test: `internal/core/settings_test.go`, `internal/httpapi/settings_integration_test.go`, settings page test

**Interfaces:**
- Produces:
  - `type Licence struct { Type, Number string }`
  - `OrgSettings` gains `Address, Phone, Email, QuoteTerms string; Licences []Licence; LogoFileID *int64` (LogoFileID read-only through `UpdateSettings`).
  - `core.SetOrgLogo(ctx, q Querier, orgID int64, fileID *int64) (old *int64, err error)`
  - `core.LogoPolicy = UploadPolicy{MaxBytes: 2 << 20, Allowed: {image/png, image/jpeg}}`
  - Routes: `PUT /api/v1/orgs/{org}/settings/logo` (multipart `file`, `org_admin`), `DELETE /api/v1/orgs/{org}/settings/logo` (`org_admin`), `GET /api/v1/orgs/{org}/settings/logo` (`invoices_read`, 404 when none).
  - Settings JSON adds `address, phone, email, quote_terms, licences: [{type, number}], has_logo`.

- [ ] **Step 1: Write failing tests**
  - `licences.yaml` table for `validateSettings`: max 5 licences; type and number each 1..60 chars after trim; blank row → `licences: type and number are required`; email must parse with `net/mail` or be empty (`email is invalid`); phone uses `phonePattern` or empty; address ≤ 300, quote_terms ≤ 4000 runes.
  - Integration: upload PNG logo → `has_logo: true`, GET streams it with `image/png`; replace logo deletes the old file via `DeleteFileIfUnreferenced`; DELETE clears it; a PDF upload → 422.
  - `settings_profile.yaml`: staff `PUT /settings/logo`→403 is multipart, so cover in Go; YAML covers `PUT /settings` with 6 licences → 422 and a foreign org GET logo → 404 exact.
  - `TestFileReferencesMatchSchema` (existing) fails until `files_gc.go` lists `orgs.logo_file_id`.

- [ ] **Step 2: Run** → FAIL.

- [ ] **Step 3: Migration**

```sql
ALTER TABLE orgs
    ADD COLUMN address text NOT NULL DEFAULT '',
    ADD COLUMN phone text NOT NULL DEFAULT '',
    ADD COLUMN email text NOT NULL DEFAULT '',
    ADD COLUMN quote_terms text NOT NULL DEFAULT '',
    ADD COLUMN licences jsonb NOT NULL DEFAULT '[]',
    ADD COLUMN logo_file_id bigint REFERENCES files;
```

- [ ] **Step 4: Implement** settings scan/update (licences marshalled as JSON), logo handlers using `module.HandleUpload` with `core.LogoPolicy` and `module.ServeFile(..., inline=true)`, `FileReferences` entry `{"orgs", "logo_file_id"}`. Settings page: new "Business profile" card with Address (textarea), Phone, Email, Licences (repeatable Type/Number rows, Add licence / Remove), Default quote terms (textarea), Logo upload/remove. Saves through the existing settings PUT.

- [ ] **Step 5: Run** `task test && task test SUITE=integration && task web:test` → PASS.

- [ ] **Step 6: Commit** `feat(settings): business profile, licences and logo`

---

### Task 3: Quotes module skeleton, schema and draft domain

**Files:**
- Create: `internal/db/migrations/0029_quotes.sql`, `internal/quotes/model.go`, `internal/quotes/quotes.go`, `internal/quotes/testdata/number.yaml`, `internal/quotes/quotes_integration_test.go`
- Modify: `internal/config/config.go`, `internal/config/config_test.go` (or `testdata`), `internal/auth/rbac.go`, `internal/auth/rbac_test.go`

**Interfaces:**
- Produces:

```go
const (StatusDraft="draft"; StatusSent="sent"; StatusAccepted="accepted"; StatusDeclined="declined"; StatusSuperseded="superseded"; DisplayExpired="expired")
const (KindLabour="labour"; KindMaterial="material")

type Line struct { ID int64; Kind, Description string; QuantityMilli int64; UnitAmount core.Cents; GSTCode core.GSTCode; GSTAmount, Amount core.Cents }
type Quote struct {
	ID, OrgID, ContactID int64; ProjectID *int64
	Number, Revision int; ParentID *int64
	Title, Terms, Status string
	IssueDate, ValidUntil time.Time
	Subtotal, GSTTotal, Total core.Cents
	PDFFileID *int64; SentAt, DecidedAt *time.Time; DecidedBy *int64
	Lines []Line
}
type Draft struct { ContactID int64; ProjectID *int64; Title, Terms string; IssueDate, ValidUntil time.Time; Lines []NewLine }
type NewLine struct { Kind, Description string; QuantityMilli int64; UnitAmount core.Cents; GSTCode string }

func FormatNumber(number, revision int) string            // "Q-0007", "Q-0007-R2"
func (q Quote) DisplayStatus(today time.Time) string        // expired when sent && ValidUntil.Before(today)
func CreateDraft(ctx, q core.Querier, orgID int64, d Draft) (Quote, error)
func UpdateDraft(ctx, q core.Querier, orgID, id int64, d Draft) (Quote, error) // 409 core.ErrInvalidState unless draft
func GetQuote(ctx, q core.Querier, orgID, id int64) (Quote, error)
type ListFilter struct { ContactID, ProjectID *int64; Status string; Limit, Offset int }
func ListQuotes(ctx, q core.Querier, orgID int64, f ListFilter) ([]Quote, int, error)
func DeleteDraft(ctx, q core.Querier, orgID, id int64) error
func Decline(ctx, tx core.Querier, orgID, userID, id int64, now time.Time) (Quote, error)  // sent only
func Revise(ctx, tx core.Querier, orgID, id int64, today time.Time) (Quote, error) // sent or declined; copies lines, terms, title; Revision+1; ParentID=id; IssueDate today; ValidUntil today + (old.ValidUntil - old.IssueDate)
```

- `config.Known` gains `"quotes"`; `Load` errors `module quotes requires timesheets` when `quotes` is on and `timesheets` is off.
- `auth.PermQuotesRead = "quotes_read"`, `auth.PermQuotesWrite = "quotes_write"`; Owner both, Staff both, Bookkeeper read.

- [ ] **Step 1: Write failing tests**
  - `number.yaml` table: `(7,1)→Q-0007`, `(7,2)→Q-0007-R2`, `(12345,1)→Q-12345`.
  - Config table case: `SHIGOTO_MODULES=quotes` → error `module quotes requires timesheets`.
  - `rbac_test.go`: Owner/Staff hold `quotes_write`; Bookkeeper holds `quotes_read` only; Tenant/Client hold neither.
  - Integration `TestCreateDraftTotals`: GST-registered org; labour 2.5h × $120 GST + material 1 × $180.00 GST → subtotal 48000, GST 4800, total 52800; lines keep kind and order.
  - `TestCreateDraftValidation`: no lines → `quote must have at least one line`; kind `other` → `line 0: kind must be labour or material`; GST on non-registered org → `line 0: org is not GST-registered`; tenant contact → `contact_id is not a client`; project of another client → `project belongs to another client`; `valid_until` before `issue_date` → `valid_until must not be before issue_date`; blank title → `title is required`.
  - `TestNumbering`: two drafts get 1 and 2; numbers per org; revision keeps number.
  - `TestUpdateDraftOnlyDraft`: update a sent quote (set status in SQL) → `core.ErrInvalidState`.
  - `TestRevise`: from sent → new draft Revision 2, ParentID set, old still `sent`; from draft or accepted → ErrInvalidState; a second Revise while a draft revision exists → ErrInvalidState `a draft revision already exists`.
  - `TestDecline`: sent → declined with DecidedBy/At; draft → ErrInvalidState.

- [ ] **Step 2: Run** → FAIL.

- [ ] **Step 3: Migration `0029_quotes.sql`**

```sql
CREATE TABLE quote_counters (org_id bigint PRIMARY KEY REFERENCES orgs, last_number int NOT NULL DEFAULT 0);

CREATE TABLE quotes (
    id           bigserial PRIMARY KEY,
    org_id       bigint NOT NULL REFERENCES orgs,
    contact_id   bigint NOT NULL REFERENCES contacts,
    project_id   bigint REFERENCES projects,
    number       int NOT NULL,
    revision     int NOT NULL DEFAULT 1 CHECK (revision >= 1),
    parent_id    bigint REFERENCES quotes,
    title        text NOT NULL,
    terms        text NOT NULL DEFAULT '',
    status       text NOT NULL DEFAULT 'draft'
                     CHECK (status IN ('draft','sent','accepted','declined','superseded')),
    issue_date   date NOT NULL,
    valid_until  date NOT NULL CHECK (valid_until >= issue_date),
    currency     char(3) NOT NULL DEFAULT 'AUD',
    subtotal     bigint NOT NULL DEFAULT 0,
    gst_total    bigint NOT NULL DEFAULT 0,
    total        bigint NOT NULL DEFAULT 0,
    pdf_file_id  bigint REFERENCES files,
    sent_at      timestamptz,
    decided_at   timestamptz,
    decided_by   bigint REFERENCES users,
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (org_id, number, revision)
);
-- At most one accepted revision, and one open draft revision, per quote number.
CREATE UNIQUE INDEX quotes_one_accepted ON quotes (org_id, number) WHERE status = 'accepted';
CREATE UNIQUE INDEX quotes_one_draft ON quotes (org_id, number) WHERE status = 'draft';
CREATE INDEX quotes_contact ON quotes (org_id, contact_id);
CREATE INDEX quotes_project ON quotes (org_id, project_id);

CREATE TABLE quote_lines (
    id              bigserial PRIMARY KEY,
    org_id          bigint NOT NULL REFERENCES orgs,
    quote_id        bigint NOT NULL REFERENCES quotes ON DELETE CASCADE,
    position        int NOT NULL,
    kind            text NOT NULL CHECK (kind IN ('labour','material')),
    description     text NOT NULL,
    quantity_milli  bigint NOT NULL,
    unit_amount     bigint NOT NULL,
    gst_code        text NOT NULL CHECK (gst_code IN ('GST','FRE','N-T')),
    gst_amount      bigint NOT NULL,
    amount          bigint NOT NULL
);

CREATE TABLE quote_attachments (
    id          bigserial PRIMARY KEY,
    org_id      bigint NOT NULL REFERENCES orgs,
    quote_id    bigint NOT NULL REFERENCES quotes ON DELETE CASCADE,
    file_id     bigint NOT NULL REFERENCES files,
    created_by  bigint NOT NULL REFERENCES users,
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (quote_id, file_id)
);
```

- [ ] **Step 4: Implement** `model.go` and `quotes.go`. Number via `quote_counters` upsert (`INSERT ... ON CONFLICT DO UPDATE SET last_number = last_number + 1 RETURNING`), mirroring `nextInvoiceNumber`. Description 1..500 runes; title 1..200; terms ≤ 4000. `UpdateDraft` replaces lines wholesale. `DeleteDraft` only for drafts; its attachments' files go through `property.DeleteFileIfUnreferenced` after commit (Task 6 adds the hook). Add `quote_attachments.file_id` and `quotes.pdf_file_id` to `FileReferences` here so the schema-FK test passes.

- [ ] **Step 5: Run** `task test && task test SUITE=integration` → PASS.

- [ ] **Step 6: Commit** `feat(quotes): schema, numbering and draft domain`

---

### Task 4: Quote PDF and send (freeze)

**Files:**
- Create: `internal/quotes/pdf.go`, `internal/quotes/send.go`, `internal/quotes/pdf_test.go`, `internal/quotes/send_integration_test.go`

**Interfaces:**
- Consumes: `core.GetSettings` (Task 2 profile fields), `core.GetContact`, `core.SaveFile`/`StageFile`, `storage.Store`.
- Produces:

```go
type PDFInput struct { Quote Quote; Org core.OrgSettings; Logo []byte; LogoMime string; Customer core.Contact; ProjectName string }
func WriteQuotePDF(w io.Writer, in PDFInput, loc *time.Location) error
func RenderPDF(ctx context.Context, q core.Querier, store storage.Store, orgID int64, qt Quote, loc *time.Location) ([]byte, error) // loads profile, logo, contact
func MarkSent(ctx context.Context, tx pgx.Tx, store storage.Store, orgID, id int64, now time.Time, loc *time.Location) (Quote, error)
```

- [ ] **Step 1: Write failing tests**
  - `pdf_test.go` `TestQuotePDFContent`: render with pdfcpu text extraction (as `certificate_test.go` does) and assert it contains `Quote Q-0007-R2`, org name, `ABN 51 824 753 556`, address, phone, email, `QBCC 1234567` and `Electrical 98765` (two licences), customer name, `Valid until 31 Dec 2026`, `Labour` and `Materials` section headings, line descriptions, `Subtotal $480.00`, `GST $48.00`, `Total $528.00`, the terms text. No attachment names appear.
  - `TestQuotePDFLongLines`: 60 lines paginate (≥2 pages) without error; non-Latin description falls back via `property.BaseLetter`.
  - `TestQuotePDFNoProfile`: empty profile, no logo → renders, no `ABN` label.
  - `send_integration_test.go`:
    - `TestMarkSentFreezesPDF`: send → status `sent`, `sent_at` set, `pdf_file_id` set, stored bytes start `%PDF`; then change org name and call staff PDF download → stored file unchanged (sha256 equal).
    - `TestMarkSentSupersedesParent`: R1 sent, Revise→R2, send R2 → R1 `superseded`, R2 `sent`.
    - `TestSendRevisionAfterAcceptRefused`: R1 sent, Revise→R2 draft, accept R1 (set via Task 5 or SQL), send R2 → `ErrInvalidState` with public message `another revision of this quote was accepted`; R1 still accepted.
    - Sending a non-draft → ErrInvalidState `quote is not a draft`.

- [ ] **Step 2: Run** → FAIL.

- [ ] **Step 3: Implement** `pdf.go` following `property/statement_pdf.go` (A4, `pdfMargin` 40, Go fonts, `BaseLetter` substitution, `fit`/`truncate`). Layout top-down: logo (fit in 160×60 pt box, keep aspect) and org block right; "Quote Q-…" heading; customer block; issue and valid-until dates; Labour table (Description, Hours, Rate, GST, Amount) then Materials table (Description, Qty, Unit, GST, Amount), each omitted when empty; totals; Terms. Dates as `2 Jan 2006` in `loc`. `MarkSent`: lock row `FOR UPDATE`, check draft, check no accepted sibling (`SELECT ... WHERE org_id AND number AND status='accepted'`), render, save via `core.SaveFile` with name `FormatNumber(...)+".pdf"`, update status, supersede parent if `sent|declined`.

- [ ] **Step 4: Run** → PASS.

- [ ] **Step 5: Commit** `feat(quotes): frozen PDF on send`

---

### Task 5: Accept a quote into a project

**Files:**
- Create: `internal/quotes/accept.go`, `internal/quotes/accept_integration_test.go`

**Interfaces:**
- Consumes: `timesheets.CreateProject`, `timesheets.CreateTask`, `timesheets.GetProject`, `timesheets.BillingFixed`, `timesheets.StatusPending/Completed/Cancelled`.
- Produces: `func Accept(ctx context.Context, tx pgx.Tx, orgID, userID, id int64, now time.Time) (Quote, timesheets.Project, error)`

- [ ] **Step 1: Write failing tests**
  - `TestAcceptCreatesProject`: GST-registered org, quote without project, lines labour "Rough-in" 8h @ $95, labour "Fit-off" 4h @ $95, material "Cable" → project named quote title, client = quote contact, `pending`, `fixed`, `HourlyRate 0`, `GSTCode GST`; tasks exactly `Rough-in` and `Fit-off`, each with `HourlyRate` override 9500; quote `accepted`, `project_id` set, `decided_by` = user. Non-GST org → project `GSTCode FRE`.
  - `TestAcceptExistingProject`: quote linked to a `tm` project at $120 → tasks added with override 9500; project billing method and rate unchanged.
  - `TestAcceptBlockedOnClosedProject`: linked project `completed` → ErrInvalidState `project is completed or cancelled`; same for `cancelled`; no tasks written.
  - `TestAcceptNameClashes`: org already has project named the quote title → new project `"<title> (Q-0007)"`; linked project already has task `Rough-in` → new task `Rough-in (Q-0007)`. Revision R2 uses `(Q-0007-R2)`.
  - `TestAcceptExpired`: sent with `valid_until` yesterday → accepts.
  - `TestAcceptStates`: draft, declined, superseded, accepted → ErrInvalidState `quote is not open`.
  - `TestAcceptConcurrentOnlyOnce`: two goroutines Accept the same quote in separate transactions → one succeeds; the other gets `quote is not open`; exactly one project and one set of tasks exist.
  - `TestAcceptOnlyMaterials`: no labour lines → project created (if none) with no tasks.

- [ ] **Step 2: Run** → FAIL.

- [ ] **Step 3: Implement** in one transaction: `SELECT ... FOR UPDATE` the quote; require `sent`; resolve or create project; for each labour line in position order create task, retrying once with the suffix on `ErrInvalidInput` "already exists" (check with an EXISTS query first rather than parsing errors); set `status='accepted'`, `project_id`, `decided_at`, `decided_by`. The `quotes_one_accepted` index backstops races; map its unique violation to `quote is not open`.

- [ ] **Step 4: Run** → PASS.

- [ ] **Step 5: Commit** `feat(quotes): accept quote into project and tasks`

---

### Task 6: Attachments

**Files:**
- Create: `internal/quotes/attachments.go`, `internal/quotes/attachments_integration_test.go`

**Interfaces:**
- Produces:

```go
type Attachment struct { ID, QuoteID, FileID, CreatedBy int64; CreatedAt time.Time; File core.File }
func AddAttachment(ctx, tx pgx.Tx, orgID, quoteID, userID int64, f core.File) (Attachment, error)
func ListAttachments(ctx, q core.Querier, orgID, quoteID int64) ([]Attachment, error)
func RemoveAttachment(ctx, tx pgx.Tx, orgID, quoteID, attachmentID int64) (fileID int64, err error)
```

- [ ] **Step 1: Write failing tests** (`testkit.Session.Upload`, routes from Task 7 or call domain directly):
  - Upload PDF, JPEG, PNG to a draft and to an accepted quote → listed newest first (attachments are internal records, allowed in any status).
  - Upload `.txt` → 422; >10 MiB → 413 (existing `HandleUpload` behaviour).
  - Same file twice → one attachment (UNIQUE), second call returns existing.
  - Remove → row gone and file deleted when unreferenced; file shared with an expense receipt is kept.
  - Attachment of a quote in another org → 404.
  - `DeleteDraft` removes attachments and GCs their files.

- [ ] **Step 2: Run** → FAIL. **Step 3: Implement.** **Step 4: Run** → PASS.

- [ ] **Step 5: Commit** `feat(quotes): internal attachments`

---

### Task 7: Staff HTTP routes, module registration and project costs

**Files:**
- Create: `internal/quotes/routes.go`, `internal/quotes/costs.go`, `internal/quotes/costs_integration_test.go`, `internal/httpapi/testdata/cases/quotes.yaml`
- Modify: `internal/httpapi/server.go` (module list), `internal/httpapi/casefile/casefile.go` (+ its test), `docs/testing.md` (seed kinds list)

**Interfaces:**
- Produces `func Module() module.Module { return module.Module{Name: "quotes", Routes: registerRoutes} }` and routes:

| Method + path | Perm | Notes |
|---|---|---|
| `GET /api/v1/orgs/{org}/quotes?contact_id&project_id&status&limit&offset` | `quotes_read` | `{quotes, total}` |
| `POST /api/v1/orgs/{org}/quotes` | `quotes_write` | 201 |
| `GET /api/v1/orgs/{org}/quotes/{id}` | `quotes_read` | includes lines, attachments, `display_status`, `number_label`, `revisions: [{id, number_label, status}]` |
| `PUT /api/v1/orgs/{org}/quotes/{id}` | `quotes_write` | draft only, 409 otherwise |
| `DELETE /api/v1/orgs/{org}/quotes/{id}` | `quotes_write` | draft only |
| `POST .../quotes/{id}/send` | `quotes_write` | |
| `POST .../quotes/{id}/decline` | `quotes_write` | |
| `POST .../quotes/{id}/revise` | `quotes_write` | 201 new draft |
| `POST .../quotes/{id}/accept` | `quotes_write` **and** `timesheets_admin` (403 `accepting a quote needs permission to create projects`) | returns `{quote, project}` |
| `GET .../quotes/{id}/pdf` | `quotes_read` | stored PDF when sent, else rendered draft; `Content-Disposition: attachment; filename="Q-0007.pdf"` |
| `POST .../quotes/{id}/attachments` | `quotes_write` | multipart `file` |
| `GET .../quotes/{id}/attachments/{attachmentId}` | `quotes_read` | stream |
| `DELETE .../quotes/{id}/attachments/{attachmentId}` | `quotes_write` | |
| `GET /api/v1/orgs/{org}/projects/{id}/costs` | `reports_read` | see below |

Request body: `{contact_id, project_id|null, title, terms, issue_date, valid_until, lines: [{kind, description, quantity_milli, unit_amount, gst_code}]}`. Omitted `terms` on create defaults to org `quote_terms`; omitted dates default to today and today+30.

- `func ProjectCosts(ctx, q core.Querier, orgID, projectID int64) (Costs, error)`:

```go
type Costs struct {
	QuotedLabour, QuotedMaterials, QuotedTotal core.Cents // ex-GST, sum over accepted quotes linked to the project
	Invoiced core.Cents                                   // sum invoice_lines.amount (ex-GST) on project, invoice status open|paid
	ExpensesTotal core.Cents                              // sum expense_lines.amount - gst_amount (amount is GST-inclusive)
	Expenses []CostLine                                   // spent_on, description, category, supplier, amount ex-GST, expense_id
}
```

Confirm `invoice_lines.amount` is ex-GST (`core.Totals` adds GST on top) before relying on it.

- [ ] **Step 1: Write failing tests**
  - `costs_integration_test.go` `TestProjectCosts`: two accepted quotes (labour 1000+2000, materials 500), one declined and one superseded quote (ignored), expense line $110 inc-GST GST-coded → 10000 ex, invoice open line 3000 + void invoice line 9999 (ignored) → `QuotedLabour 3000, QuotedMaterials 500, QuotedTotal 3500, Invoiced 3000, ExpensesTotal 10000`. Project in another org → `core.ErrNotFound`.
  - casefile: `quotes` seed kind `{org, contact, project?, status, title?, valid_until?, lines?}` (default one labour line 1h × 10000 GST); loader rejects unknown keys and a contact in another org.
  - `quotes.yaml` cases (seed: orgs acme+foreign, owner/staff/bookkeeper/client users, `modules: {timesheets: true, quotes: true}`):
    - bookkeeper `POST /quotes` → 403 exact; bookkeeper `GET /quotes` → 200.
    - staff `POST /quotes/{q_sent}/accept` → 403 `{error: "accepting a quote needs permission to create projects"}` exact, `unchanged: [quotes, projects, project_tasks]`.
    - owner GET foreign org quote → 404 exact.
    - client role `GET /api/v1/orgs/acme/quotes` → 403/404 per existing staff-route convention, exact.
    - `PUT` sent quote → 409 `{error: "quote is not a draft"}`.
    - create with `kind: other` → 422 `line 0: kind must be labour or material`.
    - `modules: {timesheets: true}` (quotes off) → `GET /quotes` 404.
    - `GET /quotes/{q_sent}` shows `display_status: expired` when seeded `valid_until` is past.
    - owner accept happy path (`isolate: true`) → 200 with `project.billing_method: fixed`.
    - `GET /projects/{pr}/costs` as bookkeeper → 200; as client → denied exact.

- [ ] **Step 2: Run** → FAIL. **Step 3: Implement** handlers in the shape of `timesheets/routes.go` (`d.Auth.RequireOrg(perm)`, `module.ReadJSON`, `module.WriteCoreError`, `module.RunInTx`), uploads via `module.HandleUpload` with `core.DocumentPolicy`, PDF via `module.ServeFile` (attachment) or rendered bytes. Register `quotes.Module()` in the `modules` list in `server.go`. Update `docs/testing.md` seed kinds.

- [ ] **Step 4: Run** `task test && task test SUITE=integration` → PASS.

- [ ] **Step 5: Commit** `feat(quotes): staff API and project costs`

---

### Task 8: Client portal quotes (API)

**Files:**
- Create: `internal/portal/quotes.go`, `internal/httpapi/testdata/cases/portal_quotes.yaml`
- Modify: `internal/portal/client.go` (register routes when `d.Cfg.Modules["quotes"]`)

**Interfaces:**
- Produces:
  - `GET /api/v1/portal/{org}/quotes?project_id=` → `{quotes: [{id, number_label, title, project_id, project_name, display_status, issue_date, valid_until, total}]}`, statuses `sent|accepted|declined|superseded` only (+ expired display), newest first.
  - `GET /api/v1/portal/{org}/quotes/{id}` → same item plus `lines` (kind, description, quantity_milli, unit_amount, gst_code, amount), `subtotal`, `gst_total`, `terms`. No attachments field.
  - `GET /api/v1/portal/{org}/quotes/{id}/pdf` → stored PDF, inline.
  - Domain helper in `internal/quotes`: `func ListPortalQuotes(ctx, q, orgID, contactID int64, projectID *int64) ([]Quote, error)` and `func GetPortalQuote(ctx, q, orgID, contactID, id int64) (Quote, error)` (ErrNotFound for drafts, other contacts, other orgs).

- [ ] **Step 1: Write failing cases** in `portal_quotes.yaml`: client sees own sent/accepted/declined/superseded quotes, not drafts; `GET /quotes/{q_draft}` → 404 exact; other contact's quote and its `/pdf` → 404 exact; foreign org → 404 exact; tenant role → 404 exact; quotes module off → 404; response for a quote has no `attachments` key (`exact` on a minimal item or assert absence through a body subset plus Go test); `?project_id=` filters.
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement.** **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `feat(portal): client quotes`

---

### Task 9: Staff quotes UI

**Files:**
- Create: `web/src/lib/modules/quotes/manifest.ts`, `types.ts`, `QuoteLineEditor.svelte`, `QuoteStatusBadge.svelte`, `AttachmentsPanel.svelte` (+ colocated tests); `web/src/routes/[org]/quotes/+page.svelte`, `new/+page.svelte`, `[id]/+page.svelte`, `[id]/edit/+page.svelte` (+ tests); `web/src/lib/test/editors/quote-new.yaml`, `quote-edit.yaml`
- Modify: `web/src/lib/modules/registry.ts` (`ModuleName` + `MODULE_ORDER` add `quotes` after `timesheets`), module manifest list, `web/src/lib/test/editors.registry.ts`, `docs/ui-standards.md` ledger

**Interfaces:**
- Consumes: Task 7 API. `ManualLine`-style payload extended with `kind`.
- Produces: `QuoteLineEditor` props `{ lines?: QuoteLine[]; busy?: boolean; onchange?: () => void; onSave: (lines: QuoteLine[]) => void }` where `QuoteLine = ManualLine & { kind: 'labour' | 'material' }`. Reuse `LineEditor`'s parsing (`parseCents`, `parseQuantityMilli`) and validation messages; for labour rows label Quantity as `Hours` and Unit price as `Rate ($/h)`. Do not change `LineEditor`'s invoice behaviour.

Screens:
- **List** (`/[org]/quotes`, nav "Quotes", group `projects`, perm `quotes_read`, icon reuse `invoices`): table Number, Title, Customer, Project, Status badge (Draft, Sent, Accepted, Declined, Superseded, Expired), Total, Valid until; filter by status; "New quote" button (`quotes_write`).
- **New / Edit** (dedicated routes per ui-standards): Customer select (client contacts, with New contact dialog as `invoices/new`), Project select (that customer's projects, optional), Title, Issue date, Valid until, Terms (prefilled from settings `quote_terms` on new), lines editor, Save draft / Cancel.
- **Detail** (`/[org]/quotes/[id]`): header with number label + status; customer, project link, dates, lines grouped Labour / Materials, totals, terms; revisions list; Attachments panel (upload/list/download/remove, `quotes_write` for changes); actions by status: Draft → Edit, Download PDF, Mark sent, Delete; Sent/Expired → Download PDF, Accept (only with `timesheets_admin`), Decline, Revise; Declined → Revise; Accepted → link to project. Accept confirm text: `Accept this quote? This creates the project and its tasks.`

- [ ] **Step 1: Write failing tests**
  - `quote-new.yaml` / `quote-edit.yaml` editor contract: all six contract keys checked; `preserves_input_on: 422`; request `POST /api/v1/orgs/acme/quotes` / `PUT .../quotes/7`.
  - `QuoteLineEditor.test.ts`: switching a row to Labour relabels fields `Hours` and `Rate ($/h)`; emitted lines carry `kind`; existing `lines` prop pre-fills rows.
  - Detail `+page.svelte.test.ts`: action visibility per status and perms (staff without `timesheets_admin` sees no Accept; bookkeeper sees no write actions); Accept posts once while busy; expired shows "Expired" badge and still offers Accept.
  - `registry.test.ts`: Quotes nav hidden when `quotes` module not enabled.
- [ ] **Step 2: Run** `task web:test` → FAIL. **Step 3: Implement.** **Step 4: Run** → PASS.
- [ ] **Step 5: Add ledger rows** for the four routes in `docs/ui-standards.md` with their proofs.
- [ ] **Step 6: Commit** `feat(web): staff quotes`

---

### Task 10: Project Costs tab and portal quote UI

**Files:**
- Create: `web/src/lib/modules/quotes/ProjectCosts.svelte` (+ test), `web/src/lib/modules/quotes/PortalQuotes.svelte` (+ test), `web/src/routes/portal/[org]/quotes/[id]/+page.svelte` (+ test)
- Modify: `web/src/routes/[org]/projects/[id]/+page.svelte` (tab union `'overview' | 'timesheet' | 'costs'`), `web/src/routes/portal/[org]/hub/+page.svelte`, `web/src/routes/portal/[org]/projects/[id]/+page.svelte`, `docs/ui-standards.md`

- **Costs tab**: shown only when `quotes` module enabled and user has `reports_read`; summary stats Quoted (labour, materials, total), Invoiced, Expenses, and "Remaining vs quote" = QuotedTotal − ExpensesTotal (negative shown as over budget, `text-error`); expenses table linking to `/[org]/expenses/{id}`. Label: `All amounts exclude GST.`
- **Portal**: Hub gets a "Quotes" section (after Projects, before Documents) listing `PortalQuotes`; portal project page lists that project's quotes; quote page shows lines, totals, terms, status badge, and "Download PDF". Superseded/declined/expired items greyed like withdrawn documents.

- [ ] **Step 1: Write failing tests**: Costs tab hidden without module or perm; over-budget renders negative in error style; stale response on tab switch ignored (existing pattern). Portal hub renders Quotes only when `quotes` in `/meta` modules; quote page has no attachments section and Download PDF hits `/api/v1/portal/acme/quotes/7/pdf`.
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement.** **Step 4: Run** → PASS.
- [ ] **Step 5: Ledger rows** for changed/new routes.
- [ ] **Step 6: Commit** `feat(web): project costs tab and portal quotes`

---

### Task 11: Sandbox, end-to-end and docs

**Files:**
- Create: `internal/sandbox/quotes.go`, `web/e2e/quotes.spec.ts`
- Modify: `internal/sandbox/populate.go`, `README.md`, `docker-compose.yml` / config docs if `SHIGOTO_MODULES` defaults are listed

- [ ] **Step 1: Sandbox** seeds, for the existing client contact: one draft, one sent (valid), one sent expired, one accepted (with project + tasks), one declined + its R2 sent revision, each with one attachment on the sent quote. Follow `internal/sandbox/timesheets.go` style; idempotent with existing reset.
- [ ] **Step 2: Playwright `quotes.spec.ts`**: owner creates a quote with one labour and one material line → marks sent → downloads PDF (response `application/pdf`) → accepts → lands on project showing task named from the labour line and a Costs tab with quoted totals; then client signs in by magic link, sees the quote on the Hub and opens its PDF.
- [ ] **Step 3: Run** `task web:e2e` → PASS.
- [ ] **Step 4: README**: "Quotes" section (module flag `quotes` needs `timesheets`; lifecycle; frozen PDF; accept creates fixed-price project; attachments internal; portal view-only) and Settings → Business profile.
- [ ] **Step 5: Full validation, sequentially**: `task lint`, `task test`, `task test SUITE=integration`, `task web:test`, `task web:e2e` → all PASS.
- [ ] **Step 6: Commit** `feat(quotes): sandbox data, e2e and docs`
