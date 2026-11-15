# CSV import for invoices and time (ERPNext data parity)

## Intent

The operator is moving their consulting work off ERPNext, once. They want Shigoto to hold the same invoices and time
records ERPNext had, for data parity: GST/income reports cover the earlier period, unpaid invoices show as open, and
historical time is visible against the projects the operator recreates by hand. Fidelity below invoice totals is not
needed.

Rather than an ERPNext-specific tool, Shigoto gets a generic owner-only CSV import on the Invoices and Time screens.
The operator exports CSVs from ERPNext, uploads them, maps columns to fields, previews, and confirms.

## Goals

- An Import action beside "New invoice" on Invoices and beside "+ New Log Entry" on Time.
- Upload a CSV, map its columns to fields, see a server-checked preview, confirm.
- Imported time links to the imported invoice it was billed on, so no imported time is ever offered by a billing run.
- Re-importing the same file, or an overlapping one, never duplicates records.

## Non-goals

- ERPNext API access, CLI commands, or ongoing sync.
- Importing invoice line items: each invoice arrives as one line at its totals.
- Creating contacts, projects, tasks or users. The operator creates these by hand first; the import only matches.
- Saving column mappings between imports.
- Credit notes, write-offs, unbilled time.

## Anti-goals

- Anything reaches a client: imported invoices never go through `billing.Service.Issue` (which re-dates and pushes to
  the payment provider) and nothing is emailed.
- A partial import: a confirmed import writes every row or none.
- Imported time appearing as unbilled.
- Amounts or dates differing from the file.

## Operator facts (confirmed)

- All ERPNext time was billed; there is no non-billable time.
- Clients have no portal access; invoices went out by email.
- Payment references on imported invoices may be random; reconciliation is manual.
- No ERPNext invoice mixes taxable and GST-free items, so one line per invoice with one GST code is exact.
- Later payments on imported invoices are recorded by hand, not by re-importing.

## Approaches considered

1. **CLI pulling from the ERPNext API (dropped).** Exact links and per-line detail, but more code than parity needs.
2. **Owner-only CSV import pages with column mapping and server preview. Chosen.** Small, generic, reuses the existing
   screens; the operator controls the export.
3. Import dialogs instead of pages. The UI standards put multi-step contextual operations with mapping and preview on a
   dedicated workflow; a dialog would be cramped. Dedicated routes chosen (builder's call, confirmed in conversation).

## Flow (both imports)

Routes `/[org]/invoices/import` and `/[org]/time/import`, reached from an "Import" secondary button in each page header.
Owners only (the button is hidden for others; the API enforces it).

1. **Upload.** Choose a CSV (max 5 MiB, UTF-8, BOM stripped, header row required). The browser parses the header row and
   first few rows only to populate the mapping dropdowns; the server re-parses everything.
2. **Map.** One dropdown per field listing the file's headers; required fields must be mapped. Optional fields may be
   left unmapped. Date format is chosen from the same choices the bank import offers.
3. **Preview.** The browser sends the file and mapping to a preview endpoint. The server parses and validates every row
   without writing and returns counts (new, already imported), totals, and row-numbered problems. Confirm is disabled
   while any problem exists.
4. **Confirm.** The browser sends the same file and mapping to the apply endpoint. The server repeats all checks and
   writes everything in one transaction, or returns the problems and writes nothing. On success the page shows counts
   and a link back to the list.

Editors follow the UI standards: failed input is preserved, duplicate submits are blocked, Cancel returns to the list,
and the import is never auto-repeated after a failed navigation or refresh.

## Invoice import

Fields:

| Field | Required | Notes |
|---|---|---|
| Invoice number | yes | ERPNext number, e.g. `ACC-SINV-2025-00042`; stored as the reference |
| Customer | yes | Matched to a `client` contact by name, case-insensitive, trimmed |
| Issue date | yes | |
| Due date | yes | Must not precede the issue date |
| Amount ex GST | yes | Decimal dollars, may contain `$` and `,` |
| GST | yes | Decimal dollars; 0 allowed |
| Total | yes | Must equal ex GST + GST to the cent |
| Outstanding | yes | 0 to total |
| Paid date | when paid | Required on rows with outstanding < total; one date per invoice. Reports are cash basis, so this sets the BAS/P&L period. The operator adds this column to the export by hand |

Each row becomes:

- An invoice with `source = 'manual'`, `status = 'open'`, the file's issue and due dates, a random payment reference
  (generated the normal way), and the next number from `invoice_counters` (locked the same way `IssueInvoice` locks it),
  assigned in issue-date then file order.
- One line: description `Imported from ERPNext <number>`, quantity 1, unit amount and amount = ex GST, `gst_code` GST when
  GST > 0 else FRE, `gst_amount` = the file's GST. Totals are stored as given.
- If paid amount (total − outstanding) > 0, a payment through `core.RecordPayment`: that amount, the paid date, method
  `other`, `provider_ref` `import:<invoice number>`. `RecordPayment` marks the invoice paid when outstanding is 0.
- An audit row (`invoice`, `import`).

The invoice detail page shows the reference beside the Shigoto number.

Blocking problems: missing or unparseable values, unmatched customer, total mismatch, outstanding out of range, due
before issue, missing paid date on a paid or part-paid row, an invoice number repeated within the file.

Already imported: a row whose invoice number already exists as an invoice reference in the org is skipped and counted.
If its amounts, outstanding or dates differ from the stored invoice, that is a blocking problem (builder's call:
later payments are recorded by hand). A customer name matching more than one client contact is a blocking problem.

## Time import

Fields:

| Field | Required | Notes |
|---|---|---|
| Date | yes | Must not be in the future |
| Hours | yes | Decimal; minutes = round(hours × 60), 1-1440 |
| Project | yes | Matched to a project by name |
| Task | yes | Matched to a task of that project by name |
| Person | yes | Email of a user with a membership in the org |
| Invoice number | yes | Must match an imported invoice's reference in the org |
| Note | no | At most 1000 characters |

Each row becomes a time entry for the matched user and task, linked (`invoice_line_id`) to the single line of the matched
invoice, so it is billed. Inactive (completed/cancelled) projects and tasks are allowed; the import inserts with its own
SQL rather than `CreateEntry`, which rejects them. An audit row per import (`time_entries`, `import`) lists the count.

Blocking problems: missing or unparseable values, unmatched project/task/person, an invoice number that is blank or not
imported, minutes out of range, note too long, future date.

Already imported: each row's reference is `import:` + sha256 of the normalised row (date, minutes, project, task, person
email, invoice number, note) plus its occurrence index among identical rows in the file, as the bank import does. A row
whose reference already exists is skipped and counted.

## Data changes

Migration `0019_import_refs.sql` adds nullable `external_ref text` to `invoices` and `time_entries`, each with
`UNIQUE (org_id, external_ref)`. Invoices store the file's invoice number prefixed `import:` (e.g.
`import:ACC-SINV-2025-00042`); the UI shows the part after `import:`.

The invoice response gains `external_ref`.

## API

- `POST /api/v1/orgs/{org}/invoices/import/preview` and `/invoices/import`
- `POST /api/v1/orgs/{org}/time-entries/import/preview` and `/time-entries/import`

Multipart: `file` plus `mapping` (JSON: field → header name, and date format). 5 MiB cap. Owner only: a new
`PermImport` held only by `owner` in `internal/auth/rbac.go`. Module gating as the owning module's other routes. Apply
takes a per-org advisory transaction lock so two imports can't race.

Responses: preview and apply both return `{new, existing, total_cents or total_minutes, problems: [{row, field,
message}]}`; apply returns 422 with problems when any exist.

## Code placement

- Shared CSV reading (header row, BOM, column lookup by header name, money and date parsing) in `internal/importers`,
  next to the bank CSV code, reusing its helpers.
- Invoice import planning and writing in `internal/billing`; time import in `internal/timesheets`.
- Web: `lib/api.ts` calls; a shared `CsvImport` workflow component (upload, mapping, preview, confirm) parameterised by
  field list and endpoints; the two routes; header buttons.
- Update `docs/ui-standards.md` screen ledger for both routes, and `docs/design.md` history row.

## Testing

Per `docs/testing.md`:

- YAML table tests: money and hours parsing, date formats, header mapping, row reference hashing.
- HTTP YAML cases: owner allowed, staff/bookkeeper/client denied, other-org isolation, module gating, 422 on problems
  with tables unchanged.
- Go integration tests: invoice import creates correct invoices, numbers continuing the counter, payments and statuses;
  time import links entries to invoice lines, allows completed projects; a billing run then offers none of the imported
  time; re-import is a no-op; any problem rolls back; no payment-provider call.
- Web: editor contract YAML for the import workflow (preserve input on failure, duplicate-submit protection, cancel),
  and a Playwright happy path.

## Left to the builder

Component and function names, exact preview layout, number of sample rows shown, problem-list paging.
