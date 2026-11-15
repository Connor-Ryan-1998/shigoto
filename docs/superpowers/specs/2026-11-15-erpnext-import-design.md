# ERPNext timesheet and invoice import

## Intent

The operator is moving their consulting work off ERPNext (ERP2) and onto Shigoto, once. Shigoto should hold the
history: every submitted timesheet row, every sales invoice that billed it, and the payments already received, so that

- billed time is never billed again by a Shigoto billing run (the operator billed all ERPNext time, so every imported
  time entry arrives billed),
- unpaid ERPNext invoices appear as open (and overdue where they are) and can be reconciled manually later,
- GST and income reports cover the period before the move.

This is a one-off move, not a sync. After the import, time is logged only in Shigoto.

## Goals

- A CLI command that pulls the relevant ERPNext documents into a local snapshot file.
- A CLI command that imports a snapshot into one Shigoto organisation: dry run by default, `--apply` to write.
- Every imported record carries its ERPNext identity, so rerunning the import is a no-op.

## Operator facts (confirmed)

- All ERPNext time was billed. A time row with no `sales_invoice` link means it was billed on an invoice without the
  timesheet link, never that it is unbilled.
- ERPNext contains no non-billable time.
- Clients have no portal access; invoices go out by email only. Imported invoices need no portal changes. If portal
  access is granted later, imported invoices will appear there with their Shigoto number and the ERPNext reference.
- ERP2 runs ERPNext v16 (pinned in `ansible/roles/erpnext/defaults/main.yml`).

## Non-goals

- Creating projects, tasks, contacts or users. The operator creates projects and tasks (and the client contacts and
  staff users they need) by hand before importing. The importer only matches against them.
- Ongoing or incremental sync, webhooks, or writing anything back to ERPNext.
- A web screen for the import.
- Importing quotations, sales orders, expenses, purchase invoices, items or the chart of accounts.
- Automatic bank reconciliation. Payment references on imported invoices are random, like any Shigoto invoice; the
  operator reconciles manually.

## Anti-goals (failure even if it "works")

- Anything reaches a client: no Stripe push, no email, no reissued invoices. Imported invoices never go through
  `billing.Service.Issue` (it re-dates the invoice and sends it to the payment provider).
- Time that ERPNext already invoiced shows up as unbilled in Shigoto.
- A partial import: some invoices or time written and others not.
- Imported invoice dates, amounts or GST differ from what the client was actually billed.
- API credentials or the snapshot (client data) committed to the repository or printed in logs.

## Constraints from the current code

- Time entries hang off tasks (`time_entries.task_id NOT NULL`), and billed state is `time_entries.invoice_line_id`.
- `timesheets.CreateEntry` rejects time on inactive tasks/projects (`checkTaskForEntry`, `internal/timesheets/entries.go:59`).
  Historical time usually belongs to completed projects, so the importer inserts entries with its own SQL. Past
  `work_date`s are already allowed; minutes must stay within 1-1440 and notes within 1000 characters.
- Invoice numbers are integers from the gap-free `invoice_counters` sequence; `core.IssueInvoice` assigns them and also
  stamps today's issue date, so the importer assigns numbers itself under the same counter lock.
- `core.RecordPayment` (`internal/core/payments.go:42`) marks an invoice paid once payments reach its total and is
  idempotent on `provider_ref`, so the importer records payments through it.
- Money is integer cents; line quantity is `quantity_milli`; GST codes are `GST`/`FRE` per line.
- The property module may already have issued Shigoto invoices, so the counter may not start at zero.

## Approaches considered

1. **CLI pulling from the ERPNext REST API into a snapshot, then importing the snapshot. Chosen.** The API returns
   the real links (timesheet row → sales invoice, invoice → payment entries), which is what the billed flag depends
   on. The saved snapshot makes dry runs repeatable without touching ERP2 and records exactly what was imported.
   Cost: an ERPNext API key and a small amount of one-use HTTP code.
2. CLI reading CSV exports from ERPNext report views. No credentials, but ERPNext flattens child tables awkwardly and
   the timesheet-row → invoice link is easy to lose, putting the billed flag at risk.
3. Settings upload screen with a preview. A new route, ledger entry, editor contracts and HTTP cases for a tool used
   once; the CLI dry run gives the same preview.

## Design

### Commands

```
shigoto admin erpnext-fetch  --out <snapshot.json> [--company <name>]
shigoto admin erpnext-import --org <slug> --snapshot <snapshot.json> [--map <map.yaml>] [--apply]
```

`erpnext-fetch` reads `ERPNEXT_URL`, `ERPNEXT_API_KEY` and `ERPNEXT_API_SECRET` from the environment (never flags, so
secrets stay out of shell history and process lists) and authenticates with `Authorization: token <key>:<secret>`.
It needs no database. It writes the snapshot with mode 0600 and refuses to overwrite an existing file.

`erpnext-import` loads config and the database like the other admin commands. Without `--apply` it validates and
prints the report, then rolls back. With `--apply` it does the same work and commits only if there are no blocking
problems.

### Snapshot contents

Submitted documents only (`docstatus = 1`), for the chosen company when `--company` is given, with child tables:

- **Project:** name, project_name, customer.
- **Task:** name, subject, project.
- **Employee:** name, employee_name, user_id, company_email, personal_email.
- **Customer:** name, customer_name.
- **Timesheet:** name, employee, and `time_logs` (name, activity_type, from_time, hours, project, task, description,
  is_billable, sales_invoice).
- **Sales Invoice:** name, customer, posting_date, due_date, is_return, grand_total, net_total, total_taxes_and_charges,
  outstanding_amount, status, `items` (name, item_name, description, qty, rate, amount, net_amount, project),
  `taxes` (rate, tax_amount, item_wise_tax_detail) and `timesheets` (time_sheet, timesheet_detail, billing_hours,
  billing_amount).
- **Payment Entry:** name, posting_date, mode_of_payment, `references` (reference_doctype, reference_name,
  allocated_amount).

The snapshot keeps ERPNext's raw field values plus a header (ERPNext URL, company, fetched-at time). The fetch lists
names via `/api/resource/<DocType>` with paging, then reads each document via `/api/resource/<DocType>/<name>` to get
its child tables.

### Matching

All matching is exact (after trimming whitespace), case-insensitive, and scoped to the target organisation. An optional
YAML map file overrides any name that differs between the systems:

```yaml
customers:   { "ERPNext customer name": "Shigoto contact name" }
projects:    { "ERPNext project_name": "Shigoto project name" }
tasks:       { "ERPNext project_name / task subject or activity type": "Shigoto task name" }
employees:   { "HR-EMP-00001": "person@example.com" }
billed_by:   { "<timesheet name or timesheet detail name>": "<sales invoice name>" }
```

`billed_by` assigns the invoice for time rows ERPNext has no link for (time billed on a manually entered invoice). A
timesheet name applies to all of its unlinked rows; a detail name applies to that one row.

- **Customer → contact:** contact `name` of kind `client`.
- **Project → project:** Shigoto project `name` equals ERPNext `project_name`.
- **Time row → task:** within the matched project, Shigoto task `name` equals the ERPNext task's `subject`, or the row's
  `activity_type` when the row has no task.
- **Employee → user:** user email equals the employee's `user_id`, else `company_email`, else `personal_email`. The user
  must have a membership in the organisation.

### Time entries

One Shigoto entry per ERPNext time row:

- `work_date` = the date part of `from_time`, in ERPNext's local time (no timezone conversion).
- `minutes` = `round(hours × 60)`, which must be within 1-1440.
- `note` = `description`, which must be at most 1000 characters.
- `user_id` = the matched user.
- `task_id` = the matched task.

Billed state. Every imported entry is billed; the importer never creates an unbilled entry.

- **Linked:** a row's invoice is its `sales_invoice`, or failing that its `billed_by` map entry. It gets a line on that
  invoice as its `invoice_line_id` (see the line rule below).
- **No invoice:** a row with neither a link nor a `billed_by` entry is a blocking problem, listed by timesheet and row,
  so the operator can add a `billed_by` entry.
- **Non-billable:** a row with `is_billable = 0` is a blocking problem (the operator confirmed none exist).
- **Invoice not imported:** a row whose invoice is not in the snapshot or is not imported is a blocking problem.

### Invoices

One Shigoto invoice per submitted ERPNext sales invoice, inserted directly (never drafted and issued). Fields:

- `contact_id`: the matched contact.
- `source`: `timesheet` if the invoice has `timesheets` rows, else `manual`.
- `issue_date`: `posting_date`.
- `due_date`: `due_date`.
- `currency`: `AUD`.
- `payment_reference`: random, generated the normal way.
- `status`: `open`. Payments then move it to `paid` (see below).

Lines: one Shigoto line per ERPNext item.

- `description` = `item_name` and `description`, with the ERPNext item text kept.
- `quantity_milli` = `qty × 1000`.
- `unit_amount` = `rate` in cents.
- `amount` = `net_amount` in cents.
- `project_id` = the item's matched project, when it has one.
- `gst_code` = `GST` when the invoice carries tax, else `FRE`.
- `gst_amount` = the line's GST as ERPNext recorded it (see below).

Per-line GST (builder's check against v16, operator-approved fallback):

- Fetch `item_code` with each item. Verify where ERPNext v16 stores per-item tax. In v15 it was the
  `item_wise_tax_detail` JSON keyed by item code, which merges lines sharing an item code; v16 may use a child table.
- If v16 gives an unambiguous per-line amount, use it.
- Otherwise, split the invoice's `total_taxes_and_charges` across its taxed lines in proportion to `net_amount`. Round
  each line to the cent and put the remaining cent(s) on the last line. The sum always equals ERPNext's tax total
  exactly.

The BAS report allocates payments across lines by `gst_amount`, so the invoice total must be exact; per-line allocation
only needs to be sensible.

ERPNext's stored amounts are what the client was billed and what the BAS must report, so they are stored as-is, not
recomputed. The dry run compares them with Shigoto's own computation (`core.LineAmount`, `core.GSTOnExclusive`) and
lists any differences as warnings. Invoice `subtotal`, `gst_total` and `total` must equal ERPNext's `net_total`,
`total_taxes_and_charges` and `grand_total`; any difference blocks.

**Time row → line rule:** a billed time row links to the first line on its invoice whose `project_id` matches the row's
project. If no line matches, it links to the invoice's first line. The link only marks the time as billed; line amounts
come from ERPNext, not from minutes × rate.

**Numbering:** imported invoices take numbers from `invoice_counters`, in `posting_date` then ERPNext-name order,
continuing the organisation's existing sequence under the same row lock `IssueInvoice` uses. The next invoice issued
normally continues after them.

**Excluded documents:**
- Draft and cancelled invoices: never fetched.
- Credit notes (`is_return = 1`) and invoices a credit note was raised against: blocking problems in this version,
  listed by name.
- Invoices that ERPNext wrote off, i.e. those whose `outstanding_amount` is not explained by payment entries: blocking
  problems, listed by name.

### Payments

For each Payment Entry reference to an imported invoice, record a payment through `core.RecordPayment`:

- `amount` = `allocated_amount`.
- `paid_on` = `posting_date`.
- `method`: `bank_transfer`, or `cash` when `mode_of_payment` is Cash.
- `provider_ref` = `erpnext:<payment entry name>:<invoice name>`.

`RecordPayment` marks the invoice paid once payments reach its total. After all payments are recorded, each invoice's
outstanding amount (total minus payments) must equal ERPNext's `outstanding_amount`. Any difference blocks.

Imported payments have no bank line, so bank reconciliation will treat them like manually recorded payments.

### Tracking and idempotency

Migration `0019_erpnext_import.sql` adds a nullable `external_ref text` to `invoices` and `time_entries`, with
`UNIQUE (org_id, external_ref)` on each. Values:

- invoices: `erpnext:<sales invoice name>`
- time entries: `erpnext:<timesheet detail name>`

Payments use their existing unique `provider_ref`.

**Reruns:** a record whose `external_ref` (or `provider_ref`) already exists is skipped and counted as "already
imported". It is not updated. Rerunning the same snapshot after a successful apply writes nothing.

A newer snapshot may follow an earlier apply (a test import, then a final fetch at cutover). New records are imported
normally. For records that already exist:

- An existing imported invoice whose ERPNext amounts or dates differ is a blocking problem.
- An existing imported time entry whose invoice in the new snapshot differs from the one it is linked to is a blocking
  problem.

Since every entry is imported billed, there is no unbilled → billed transition to handle.

**Single transaction:** the whole apply runs in one transaction, holding `pg_advisory_xact_lock` on a fixed import key
for the organisation. Any blocking problem, or any error, rolls everything back.

**Visibility:** the invoice detail page shows the ERPNext reference (for example `ACC-SINV-2025-00042`) next to the
Shigoto number, so old invoices can be cross-referenced. The invoice response gains an `external_ref` field.

**Audit:** one `audit_log` row per imported invoice (action `erpnext_import`), with the ERPNext name and the time entry
count. The actor is null, as for other system actions.

### Report

The dry run and the apply print the same report:

- counts to create and already imported, for invoices, lines, time entries and payments, plus total minutes and dollars
- blocking problems, grouped by kind and listing the ERPNext names:
  - unmatched customer, project, task or employee
  - a time row outside the 1-1440 minute range
  - a note that is too long
  - a link to an invoice that isn't imported
  - a time row with no invoice link and no `billed_by` entry
  - a non-billable row
  - a credit note
  - a total or outstanding-amount mismatch
  - an already-imported record that differs in the new snapshot
- warnings:
  - line computation differences
  - invoices whose per-line GST was split proportionally

The exit status is non-zero when anything is blocking.

## Code placement

- New package `internal/erpnext`:
  - snapshot types
  - the API client used by fetch
  - the matcher
  - the planner, which turns a snapshot plus database lookups into an import plan and report with no writes
  - the writer, which applies the plan in a transaction

  The planner and writer both take `core.Querier`, like other domain code. Insert SQL for imported invoices and entries
  lives in this package because it deliberately bypasses the normal draft/issue and active-task paths.
- `cmd/shigoto/main.go`: two new `admin` subcommands following `create-owner` / `populate-sbx`.
- `internal/billing/invoice_response.go` and the web invoice detail view: show `external_ref`.
- Docs:
  - `docs/design.md`: the History row records the ERPNext import alongside "No Ailo import".
  - `plans/erp/shigoto_production_plan.md` (parent repo): replace "No automatic data import" with a pointer to this
    command.
  - `README`: a short operator runbook covering creating an ERPNext API key for a read-only user, fetch, dry run, apply,
    and deleting the snapshot afterwards.

## Testing

Following `docs/testing.md`, each behaviour in the layer that owns it:

- **YAML table tests** (`internal/erpnext/testdata/*.yaml`):
  - hours → minutes rounding
  - ERPNext money → cents
  - map-file overrides
  - employee email fallback order
  - time row → line rule and `billed_by` resolution
  - proportional GST split with remainder cents
  - credit-note and write-off detection
- **Fetch:** against an `httptest` server serving fixture JSON, covering paging, child-table reads, the auth header,
  the 0600 file mode and refusing to overwrite.
- **Go integration tests** (testkit, compose Postgres), using a fixture snapshot:
  - a full import producing correct invoices, lines, payments, statuses, dates and numbers
  - every imported entry is billed, including time on completed projects and `billed_by` rows
  - a following Shigoto billing run offers none of the imported time
  - dry run writes nothing
  - rerun is a no-op; a newer snapshot adds new records and blocks on changed ones
  - a blocking problem rolls everything back
  - counter continuation when invoices already exist
  - other organisations untouched
  - no payment-provider call (fake provider records zero calls)
- **Web:** the invoice detail shows the ERPNext reference (existing invoice detail test layer).

Run Taskfile validation sequentially through `uv run --locked`. Never run the import against the production database as
part of automated verification. Snapshot fixtures use invented data.

## Left to the builder

- Snapshot JSON shape beyond "raw ERPNext fields plus a header".
- Report formatting.
- Exact internal type and function names.
- Fetch concurrency and paging size.
