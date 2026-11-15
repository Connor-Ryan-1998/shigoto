# CSV Import for Invoices and Time Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Owner-only CSV import (upload → map columns → server preview → confirm) for invoices and time entries, so ERPNext history reaches Shigoto with time linked to the invoices that billed it.

**Architecture:** A generic header-based CSV table reader in `internal/importers`; invoice import in `internal/billing` (insert via a new `core.ImportInvoice`, never `Issue`); time import in `internal/timesheets` with its own insert SQL. Each import exposes `.../import/preview` (no writes) and `.../import` (all-or-nothing, advisory-locked). The web gets one generic `CsvImport` workflow component and two dedicated routes.

**Tech Stack:** Go (net/http, pgx), Postgres, SvelteKit + Tailwind + DaisyUI, Vitest, Playwright.

**Spec:** `docs/superpowers/specs/2026-11-15-erpnext-import-design.md`

## Global Constraints

- Owner only: new `auth.PermImport = "import"`, granted to `Owner` only.
- Upload cap 5 MiB (`5 << 20`), multipart fields `file` and `mapping` (JSON). Over cap → 413 `"file too large"`.
- Imported invoices never call `billing.Service.Issue`, the payment provider, or mail.
- Apply is one transaction; any problem → 422, nothing written.
- `external_ref` values: invoices `import:<invoice number>`; time entries `import:<hex sha256>`. Payments `provider_ref` = `import:<invoice number>`.
- Imported invoice: `source = 'manual'`, one line, description `Imported from ERPNext <number>`, quantity_milli 1000, GST code `GST` if GST > 0 else `FRE`, payment method `other`.
- Date formats: exactly the bank importer's `02/01/2006`, `2006-01-02`, `2 Jan 2006`.
- Matching is trimmed and case-insensitive; more than one match is a problem ("ambiguous").
- Validation via `uv run --locked task ...` sequentially; never against production/operator databases.

## Review Focus

1. **Excel-saved CSV** (UTF-8 BOM, CRLF, trailing blank lines) → BOM stripped, blank rows ignored, headers still match. Test: Task 2 `bom_crlf_blank_rows`.
2. **Duplicate or blank header names** → mapping to a duplicated header is a problem naming the header, not a silent first-match. Test: Task 2 `duplicate_header`.
3. **Org not GST-registered but rows carry GST** → per-row problem `GST charged but organisation is not GST-registered`. Test: Task 4 `TestInvoiceImportGSTNotRegistered`.
4. **Time file imported before the invoice file** → every row reports `invoice <n> has not been imported`; nothing written. Test: Task 6 `TestEntryImportBeforeInvoices`.
5. **Overlapping re-import** (same rows plus new ones) → existing counted and skipped, new imported; changed existing invoice is a problem. Test: Task 4 `TestInvoiceImportOverlap`.

---

### Task 1: Schema, permission and invoice reference field

**Files:**
- Create: `internal/db/migrations/0019_import_refs.sql`
- Modify: `internal/auth/rbac.go` (const block, `Owner` map)
- Modify: `internal/core/invoices.go` (`Invoice` struct, the column list/scan used by `GetInvoice` and list queries)
- Modify: `internal/billing/invoice_response.go` (`InvoiceResponse`, `InvoiceJSON`)
- Test: `internal/auth/rbac_test.go` (create if absent), `internal/billing/invoice_response_test.go`
- Check: `grep -rn "timesheets_admin" internal/httpapi/testdata web/src` — update any exact owner-perm lists to include `import`.

**Interfaces:**
- Produces: `auth.PermImport Perm = "import"`; `core.Invoice.ExternalRef string`; `billing.InvoiceResponse.ExternalRef string \`json:"external_ref"\`` (raw value, e.g. `import:ACC-SINV-1`; empty for normal invoices).

- [ ] **Step 1: Write failing tests**
  - `TestImportPermOwnerOnly`: `Can(Owner, PermImport)` true; false for `Staff`, `Bookkeeper`, `Tenant`, `Client`.
  - In `invoice_response_test.go`, extend the existing response-mapping test (or add `TestInvoiceJSONExternalRef`) so an invoice with `ExternalRef: "import:ACC-SINV-1"` yields `external_ref: "import:ACC-SINV-1"`.
- [ ] **Step 2: Run** `uv run --locked task test PACKAGES=./internal/auth/... ./internal/billing/...` — expect FAIL (undefined).
- [ ] **Step 3: Migration**

```sql
-- External references for records brought in by CSV import (ERPNext
-- history). NULL for records created in Shigoto.
ALTER TABLE invoices ADD COLUMN external_ref text;
ALTER TABLE invoices ADD CONSTRAINT invoices_org_external_ref_key UNIQUE (org_id, external_ref);
ALTER TABLE time_entries ADD COLUMN external_ref text;
ALTER TABLE time_entries ADD CONSTRAINT time_entries_org_external_ref_key UNIQUE (org_id, external_ref);
```

- [ ] **Step 4: Implement** the perm, struct field (scan `coalesce(external_ref,'')`), and response field.
- [ ] **Step 5: Run** unit tests, then `uv run --locked task test SUITE=integration` — expect PASS (fix any exact perm lists found in the check).
- [ ] **Step 6: Commit** `feat(import): external refs and owner import permission`

---

### Task 2: Header-based CSV table reader

**Files:**
- Create: `internal/importers/table.go`, `internal/importers/table_test.go`, `internal/importers/testdata/table.yaml`
- Modify: `internal/importers/csv.go` (export `ParseAmountCents`, add `ParseDate`, `DateFormats`; bank code calls the exported names)

**Interfaces:**
- Produces:

```go
// Problem is one row-numbered import problem. Row is 1-based counting the
// header row (row 1), 0 for whole-file problems.
type Problem struct {
	Row     int    `json:"row"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

// Mapping maps import field keys to CSV header names.
type Mapping struct {
	Columns    map[string]string `json:"columns"`
	DateFormat string            `json:"date_format"`
}

type Row struct {
	Num    int               // 1-based file row, header is 1
	Values map[string]string // field key -> trimmed cell; absent if unmapped
}

// ReadTable reads a header-row CSV, applies m for the given fields, and
// returns data rows (blank rows skipped) plus whole-file problems: unknown
// date format, a required field unmapped, a mapped header missing from the
// file or present more than once.
func ReadTable(r io.Reader, m Mapping, fields []Field) ([]Row, []Problem)

type Field struct { Key string; Required bool }

func ParseAmountCents(raw string) (core.Cents, error) // renamed from parseAmountCents
func ParseDate(format, raw string) (time.Time, error) // UTC midnight; ErrInvalidInput on bad format/value
var DateFormats = []string{"02/01/2006", "2006-01-02", "2 Jan 2006"}
```

Header matching is trimmed and case-insensitive. Use `stripBOM`, `FieldsPerRecord = -1`, `TrimLeadingSpace`. A row is blank when every cell trims to "".

- [ ] **Step 1: Write table cases** in `testdata/table.yaml` (loaded with `table.Load`), each with `csv`, `mapping`, `fields`, and expected `rows` / `problems`:
  - `maps_by_header_case_insensitive`: header `Invoice No, Customer`, mapping `{number: "invoice no"}` → row 2 `{number: ...}`.
  - `bom_crlf_blank_rows`: `"﻿A,B\r\n1,2\r\n\r\n,\r\n"` → one row, Num 2.
  - `duplicate_header`: header `A,A` mapping `{x: A}` → problem `{row: 0, field: x, message: 'header "A" appears more than once'}`.
  - `missing_header`: mapping to `Nope` → `{row: 0, field: x, message: 'header "Nope" not found'}`.
  - `required_unmapped`: → `{row: 0, field: x, message: "x must be mapped"}`.
  - `bad_date_format`: `date_format: "01/02/2006"` → `{row: 0, field: date_format, message: "unsupported date format"}`.
  - `parse_date_each_format`: one case per `DateFormats` entry.
- [ ] **Step 2: Run** `uv run --locked task test PACKAGES=./internal/importers/...` — FAIL.
- [ ] **Step 3: Implement** `table.go` and the `csv.go` exports.
- [ ] **Step 4: Run** — PASS, including existing bank CSV tests.
- [ ] **Step 5: Commit** `feat(importers): header-mapped CSV table reader`

---

### Task 3: `core.ImportInvoice`

**Files:**
- Modify: `internal/core/invoices.go`
- Test: `internal/core/invoices_integration_test.go`

**Interfaces:**
- Consumes: Task 1 `ExternalRef`.
- Produces:

```go
type ImportedInvoice struct {
	ContactID   int64
	ExternalRef string    // "import:<number>"
	IssueDate   time.Time
	DueDate     time.Time
	Description string
	Subtotal    Cents     // ex GST
	GST         Cents
	GSTCode     GSTCode   // GST or FRE
}

// ImportInvoice inserts an already-issued invoice: status 'open', the given
// dates, the next number from invoice_counters (same upsert IssueInvoice
// uses), a random payment reference, and one line (quantity 1000, unit =
// amount = Subtotal, gst_amount = GST). Totals are stored as given, not
// recomputed. Run inside the caller's transaction.
func ImportInvoice(ctx context.Context, q Querier, orgID int64, in ImportedInvoice) (Invoice, error)
```

Extract the counter upsert from `IssueInvoice` into `nextInvoiceNumber(ctx, q, orgID) (int, error)` and use it in both.

- [ ] **Step 1: Write failing tests**
  - `TestImportInvoiceKeepsDatesAndAmounts`: issue 2025-03-01, due 2025-03-15, subtotal 100000, GST 10000 → status `open`, `Total` 110000, one line with `GSTAmount` 10000, `ExternalRef` set, non-empty `PaymentReference`.
  - `TestImportInvoiceContinuesCounter`: issue one normal invoice (number 1) then import → number 2; then a normal issue → 3.
- [ ] **Step 2: Run** `uv run --locked task test SUITE=integration PACKAGES=./internal/core/...` — FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** — PASS (existing `IssueInvoice` tests too).
- [ ] **Step 5: Commit** `feat(core): insert imported issued invoices`

---

### Task 4: Invoice import domain

**Files:**
- Create: `internal/billing/import.go`, `internal/billing/import_table_test.go`, `internal/billing/testdata/invoice_rows.yaml`, `internal/billing/import_integration_test.go`

**Interfaces:**
- Consumes: Task 2 `importers.Row/Problem/Field/ParseAmountCents/ParseDate`; Task 3 `core.ImportInvoice`; `core.RecordPayment`; `core.Audit`.
- Produces:

```go
var InvoiceImportFields = []importers.Field{
	{Key: "number", Required: true}, {Key: "customer", Required: true},
	{Key: "issue_date", Required: true}, {Key: "due_date", Required: true},
	{Key: "subtotal", Required: true}, {Key: "gst", Required: true},
	{Key: "total", Required: true}, {Key: "outstanding", Required: true},
	{Key: "paid_date"},
}

type InvoiceRow struct {
	Row                               int
	Number, Customer                  string
	IssueDate, DueDate                time.Time
	PaidDate                          *time.Time
	Subtotal, GST, Total, Outstanding core.Cents
}

// ParseInvoiceRows converts mapped rows; pure.
func ParseInvoiceRows(rows []importers.Row, dateFormat string) ([]InvoiceRow, []importers.Problem)

type ImportResult struct {
	New        int                 `json:"new"`
	Existing   int                 `json:"existing"`
	TotalCents int64               `json:"total_cents"` // sum of Total over new rows
	Problems   []importers.Problem `json:"problems"`    // never nil
}

// ImportInvoices validates rows against orgID's data and, when apply is
// true and there are no problems, writes them. The caller owns the
// transaction (and the advisory lock for apply).
func ImportInvoices(ctx context.Context, q core.Querier, orgID int64, actor *int64, rows []InvoiceRow, apply bool) (ImportResult, error)
```

Row rules (problem `field` = field key; messages exactly as below):
- unparseable money/date → `invalid <field>`; `total` ≠ `subtotal + gst` → `total must equal subtotal plus GST`; `outstanding` < 0 or > total → `outstanding must be between 0 and total`; `due_date` < `issue_date` → `due date is before issue date`; outstanding < total and no paid date → `paid date is required when anything has been paid`; repeated number in file → `invoice number appears more than once`.
- `customer`: no `client` contact with that name → `no client contact named "<x>"`; more than one → `more than one client contact named "<x>"`.
- `gst` > 0 and org not GST-registered → `GST charged but organisation is not GST-registered`.
- Existing `import:<number>`: counted `Existing`; if issue/due dates, subtotal, GST, total or outstanding (total − sum of payments) differ → `field: number`, `already imported with different values`.

Apply per new row, in issue-date then file order: `core.ImportInvoice`; if paid = total − outstanding > 0, `core.RecordPayment{Amount: paid, PaidOn: *PaidDate, Method: "other", ProviderRef: "import:<number>"}`; `core.Audit(..., "invoice", id, "import", map{"external_ref":...})`.

- [ ] **Step 1: Write table cases** (`invoice_rows.yaml`) for `ParseInvoiceRows`: valid row, `$1,234.56` money, each message above that's row-local (total mismatch, outstanding range, due before issue, paid date required, duplicate number, invalid date).
- [ ] **Step 2: Write integration tests** (call `ImportInvoices` directly inside a tx):
  - `TestInvoiceImportPaidPartAndOpen`: 3 rows (paid in full, part-paid 500 of 1100, unpaid) → statuses `paid`, `open`, `open`; payments 1100 and 600 with `paid_on` = paid date; numbers ascend by issue date.
  - `TestInvoiceImportPreviewWritesNothing`: apply=false → `New` 3, `Count("invoices")` unchanged.
  - `TestInvoiceImportUnknownCustomer` and an ambiguous-customer case.
  - `TestInvoiceImportGSTNotRegistered`.
  - `TestInvoiceImportOverlap`: import 2, re-import same 2 + 1 new → Existing 2, New 1; then change one total → problem `already imported with different values`.
  - `TestInvoiceImportNoProvider`: handler built with the fake provider records zero calls (use `e.Handler(testkit.Billing(...))` pattern from billing tests) — or assert no `provider_ref` set.
- [ ] **Step 3: Run** unit + integration for `./internal/billing/...` — FAIL.
- [ ] **Step 4: Implement** `import.go`.
- [ ] **Step 5: Run** — PASS.
- [ ] **Step 6: Commit** `feat(billing): invoice CSV import domain`

---

### Task 5: Invoice import endpoints

**Files:**
- Create: `internal/importers/http.go` (shared multipart reader), `internal/httpapi/import_routes.go`
- Modify: `internal/httpapi/core_routes.go` (register), `internal/testkit/` session file (add `UploadForm`)
- Create: `internal/httpapi/testdata/cases/imports.yaml`, `internal/httpapi/import_integration_test.go`

**Interfaces:**
- Produces:

```go
// ReadUpload enforces the 5 MiB cap and returns the file bytes' reader and
// the decoded mapping. Errors: *UploadError{Status, Msg}: 413 "file too
// large", 400 "invalid multipart body", 422 "file is required",
// 422 "mapping is required", 422 "invalid mapping".
func ReadUpload(w http.ResponseWriter, r *http.Request) (io.Reader, Mapping, error)

func (s Session) UploadForm(path, filename string, data []byte, fields map[string]string) Response // testkit
```

Routes (core, always on), both `RequireOrg(auth.PermImport)`:
- `POST /api/v1/orgs/{org}/invoices/import/preview` → 200 `ImportResult` (problems included).
- `POST /api/v1/orgs/{org}/invoices/import` → in `runInTx`: `SELECT pg_advisory_xact_lock(hashtextextended('import:' || $1::text, 0))` with org id, then `ImportInvoices(apply=true)`; problems → 422 `{"error": "import has problems; preview again", "problems": [...]}` (rollback); else 200 `ImportResult`.
- Whole-file problems from `ReadTable` → treated as problems in both endpoints.

- [ ] **Step 1: Write YAML cases** (`imports.yaml`; seed owner, staff, bookkeeper in `acme`, owner in `other`): staff and bookkeeper POST both paths → 403; `other` owner → 404 `exact: true`; all with `unchanged: [invoices, payments, audit_log]`.
- [ ] **Step 2: Write integration tests** via `UploadForm`: preview returns counts without writing; apply writes and returns 200; apply with a bad row → 422 with `problems`, `Snapshot("invoices","payments")` unchanged; 6 MiB file → 413; missing mapping → 422.
- [ ] **Step 3: Run** `uv run --locked task test SUITE=integration PACKAGES=./internal/httpapi/...` — FAIL.
- [ ] **Step 4: Implement.**
- [ ] **Step 5: Run** — PASS.
- [ ] **Step 6: Commit** `feat(api): invoice import endpoints`

---

### Task 6: Time import domain and endpoints

**Files:**
- Create: `internal/timesheets/import.go`, `internal/timesheets/import_table_test.go`, `internal/timesheets/testdata/entry_rows.yaml`, `internal/timesheets/import_integration_test.go`
- Modify: `internal/timesheets/routes.go` (register), `internal/httpapi/testdata/cases/imports.yaml`, `internal/httpapi/testdata/cases/modules_disabled.yaml`

**Interfaces:**
- Consumes: Task 2 reader/parsers; Task 5 `importers.ReadUpload`, the same advisory-lock SQL and 422 shape; Task 1 `time_entries.external_ref`.
- Produces:

```go
var EntryImportFields = []importers.Field{
	{Key: "date", Required: true}, {Key: "hours", Required: true},
	{Key: "project", Required: true}, {Key: "task", Required: true},
	{Key: "person", Required: true}, {Key: "invoice", Required: true},
	{Key: "note"},
}

type EntryRow struct {
	Row                                 int
	WorkDate                            time.Time
	Minutes                             int
	Project, Task, Person, Invoice, Note string
	Ref                                 string // "import:<hex sha256>"
}

// ParseEntryRows is pure. Ref = "import:" + hex(sha256(
// "<YYYY-MM-DD>|<minutes>|<lower project>|<lower task>|<lower person>|<invoice>|<note>|<n>"))
// where n is the occurrence index of identical preceding rows.
func ParseEntryRows(rows []importers.Row, dateFormat string, today time.Time) ([]EntryRow, []importers.Problem)

type EntryImportResult struct {
	New          int                 `json:"new"`
	Existing     int                 `json:"existing"`
	TotalMinutes int                 `json:"total_minutes"`
	Problems     []importers.Problem `json:"problems"`
}

func ImportEntries(ctx context.Context, q core.Querier, orgID int64, actor *int64, rows []EntryRow, apply bool) (EntryImportResult, error)
```

Rules (messages exact): hours not a decimal → `invalid hours`; minutes = round(hours×60) outside 1–1440 → `hours must be between 0.02 and 24`; date after today → `date is in the future`; note > 1000 runes → `note must be at most 1000 characters`; blank invoice → `invoice number is required`; no invoice with `external_ref = 'import:'||<invoice>` → `invoice <x> has not been imported`; that invoice has ≠ 1 line → `invoice <x> is not an imported single-line invoice`; project not found / ambiguous → `no project named "<x>"` / `more than one project named "<x>"`; task within project likewise `no task named "<x>" in <project>`; person: no user with that email holding a membership in the org → `no member with email "<x>"`.

Apply inserts `time_entries (org_id, task_id, user_id, work_date, minutes, note, invoice_line_id, external_ref)` directly (inactive projects/tasks allowed) and audits each (`time_entry`, id, `import`). Rows whose `Ref` exists count as `Existing`.

Routes (timesheets module), `RequireOrg(auth.PermImport)`: `POST /api/v1/orgs/{org}/time-entries/import/preview`, `POST /api/v1/orgs/{org}/time-entries/import`; `today` = the module's `today(d)`.

- [ ] **Step 1: Write table cases** (`entry_rows.yaml`): `7.5` → 450; `0.25` → 15; `8:30` → `invalid hours`; `25` → range message; future date; long note; blank invoice; two identical rows get different `Ref`s; same row in two parses gets the same `Ref`.
- [ ] **Step 2: Write integration tests**:
  - `TestEntryImportLinksToInvoiceLine`: import an invoice (Task 4), then entries on a `completed` project → entries have `invoice_line_id` = that invoice's line.
  - `TestEntryImportBillingRunIgnoresImported`: afterwards `PreviewBilling` over the range returns no projects.
  - `TestEntryImportBeforeInvoices`.
  - `TestEntryImportOtherUser`: person = a staff member's email → entry `user_id` is theirs.
  - `TestEntryImportRerunNoop`: same file twice → second `New` 0, `Existing` n.
  - HTTP: preview/apply via `UploadForm`; 422 rollback leaves `time_entries` unchanged.
- [ ] **Step 3: Add YAML cases**: staff/bookkeeper 403, other-org 404 for both time paths; in `modules_disabled.yaml`, time import paths → 404 `exact: true`.
- [ ] **Step 4: Run** — FAIL. **Step 5: Implement.** **Step 6: Run** `uv run --locked task test` and `uv run --locked task test SUITE=integration` — PASS.
- [ ] **Step 7: Commit** `feat(timesheets): time entry CSV import`

---

### Task 7: Web import workflow component

**Files:**
- Create: `web/src/lib/imports/csv.ts`, `web/src/lib/imports/csv.test.ts`, `web/src/lib/imports/CsvImport.svelte`, `web/src/lib/imports/CsvImport.test.ts`
- Modify: `web/src/lib/api.ts` (`ApiError` gains `problems?: ImportProblem[]`, read from `data.problems`)

**Interfaces:**
- Produces:

```ts
export type ImportProblem = { row: number; field?: string; message: string };
export type ImportField = { key: string; label: string; required: boolean };
export type ImportSummary = { new: number; existing: number; total_cents?: number; total_minutes?: number; problems: ImportProblem[] };

// Header row plus up to `limit` sample rows; handles quotes, BOM, CRLF.
export function readSample(text: string, limit = 5): { headers: string[]; rows: string[][] };
// Initial mapping: field label or key equal (case-insensitive, trimmed) to a header.
export function guessMapping(fields: ImportField[], headers: string[]): Record<string, string>;
```

`CsvImport.svelte` props: `{ fields: ImportField[]; previewPath: string; applyPath: string; backHref: string; noun: string; summaryLabel: (s: ImportSummary) => string }`.

Behaviour:
- Steps: file input (`accept=".csv,text/csv"`, label `CSV file`) → one `Select` per field (options: `— not mapped —` + headers) and a date-format `Select` (the three formats, labels `DD/MM/YYYY`, `YYYY-MM-DD`, `D Mon YYYY`) → **Preview** button → summary plus a problems table (Row, Field, Problem) → **Import** button, disabled while problems exist or the preview is stale. Changing the file or the mapping marks the preview stale.
- Both calls send `FormData` with `file` and `mapping` = `JSON.stringify({columns, date_format})` via `api('POST', …)`.
- Guards: `useUnsavedChanges(() => dirty, () => busy)`; `busy` blocks duplicate submits; errors go to a danger `Toast` with input preserved; a 422 with `problems` replaces the problem list. Cancel (`variant="secondary"`) uses `allowLeave()` then `goto(backHref)`. After success: show counts and a `Back to <noun>` link; never auto-retry.

- [ ] **Step 1: Write tests**
  - `csv.test.ts`: quoted comma, escaped quote, BOM, CRLF, `limit`; `guessMapping` matches `Invoice number` header to label `Invoice number`.
  - `CsvImport.test.ts` (use `fireEvent.change(input, { target: { files: [new File([...], 'x.csv', { type: 'text/csv' })] } })` as in `expenses/+page.svelte.test.ts:192`): mapping dropdowns list headers; Preview posts FormData to `previewPath`; Import disabled with problems; second Import click while busy sends one request; a 500 keeps the file and mapping and shows a toast; changing a mapping after preview disables Import until previewed again; Cancel navigates to `backHref`.
- [ ] **Step 2: Run** `uv run --locked task web:test` — FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** — PASS.
- [ ] **Step 5: Commit** `feat(web): generic CSV import workflow`

---

### Task 8: Web routes, header buttons, invoice reference, docs

**Files:**
- Create: `web/src/routes/[org]/invoices/import/+page.svelte`, `web/src/routes/[org]/time/import/+page.svelte`, `web/e2e/imports.spec.ts`
- Modify: `web/src/routes/[org]/invoices/+page.svelte:115-123`, `web/src/routes/[org]/time/+page.svelte:147-175`, `web/src/routes/[org]/invoices/[id]/+page.svelte:266` (+ its test), `docs/ui-standards.md` ledger, `docs/design.md:102`

**Interfaces:**
- Consumes: Task 7 `CsvImport`; Task 1 `external_ref`; endpoints from Tasks 5–6.

Details:
- `canImport = session.currentOrg?.slug === page.params.org && (session.currentOrg?.perms.includes('import') ?? false)`; time page also requires `session.modules.includes('timesheets')` (already implied by being on it).
- Header buttons: `<Button variant="secondary" onclick={() => goto(`/${org}/invoices/import`)}>Import</Button>` before "New invoice"; on Time, same before "+ New Log Entry".
- Invoices fields (labels): Invoice number, Customer, Issue date, Due date, Amount ex GST, GST, Total, Outstanding, Paid date. Time fields: Date, Hours, Project, Task, Person (email), Invoice number, Note.
- Pages: `PageHeader` title `Import invoices` / `Import time`, back to the list.
- Invoice detail: when `inv.external_ref` starts with `import:`, show `Imported as <rest>` under the header.
- Ledger rows:
  - `| \`[org]/invoices/import/+page.svelte\` | Dedicated guarded import workflow (shared CsvImport, server-checked preview) | \`lib/imports/CsvImport.test.ts\`, \`e2e/imports.spec.ts\` |`
  - `| \`[org]/time/import/+page.svelte\` | Dedicated guarded import workflow (shared CsvImport, server-checked preview) | \`lib/imports/CsvImport.test.ts\`, \`e2e/imports.spec.ts\` |`
- `design.md:102` → `| History | Start clean. No Ailo import. ERPNext invoices and time come in through the owner-only CSV import. |`

- [ ] **Step 1: Write tests**: invoices list page test — Import shown for owner perms, hidden for staff; time page likewise; invoice detail test shows `Imported as ACC-SINV-1`. `e2e/imports.spec.ts`: seed client contact, project and task via `startStack` seed; upload an invoice CSV with `setInputFiles`, preview, import; upload a time CSV referencing it; assert invoice list shows it and the billing-run dialog has no unbilled time.
- [ ] **Step 2: Run** `uv run --locked task web:test` — FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `uv run --locked task web:test`, then `uv run --locked task web:e2e` — PASS.
- [ ] **Step 5: Run** full `uv run --locked task verify` — PASS.
- [ ] **Step 6: Commit** `feat(web): invoice and time import screens`
