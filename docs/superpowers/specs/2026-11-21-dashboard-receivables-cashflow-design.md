# Dashboard: receivables, cash flow and project billing

Approved 2026-11-21, including the final amendment that both timesheet totals and project rows count only active projects.

## Intent

Replace the dashboard's Open invoices, Overdue and Drafts tiles with an accounting-style overview: what's owed, how cash moved, and which projects have unbilled work. Follow the supplied screenshots' card headers, summary hierarchy and chart layout while retaining Shigoto's components and themes.

## Goals

### Total Receivables

- Heading: **Total Receivables**. Amount label: **Total Unpaid Invoices**.
- Total uses existing `open_outstanding`; Current is `open_outstanding - overdue_outstanding`.
- Horizontal bar splits Current (primary) and Overdue (warning), with labeled currency totals.
- Current links to `/{org}/invoices?status=open` (the existing open filter includes overdue invoices); Overdue links to `/{org}/invoices?status=overdue`.
- `+ New` links to invoice creation only for users with `invoices_write`.
- Preserve the existing no-invoices empty state for this card only. Settled organisations with invoice history still see the zero-balance card.

### Cash Flow

- Full-width card with monthly incoming and outgoing, and Incoming (+), Outgoing (−), Net (=) totals.
- Source: imported `bank_lines`, excluding `status = 'ignored'`. Include both matched and unmatched lines; do not also add linked payments or expenses.
- Incoming is positive credits; outgoing is the positive magnitude of debits; net is incoming minus outgoing.
- Period selector: **This fiscal year** (default), **Previous fiscal year**.
- Server resolves periods using the organisation's `fy_start_month`, reusing `reports.FinancialYear`.
- Return every month in the fiscal year, including zero months. No opening/closing cash balances.
- Show only when the reconciliation module is enabled and the caller has `reports_read`.
- Explain that figures represent imported bank activity, not a verified bank balance.

### Timesheets / Projects

- Card heading **Projects**, with **Unbilled Hours** in h:mm and the existing dollar estimate beneath.
- Both header totals include only active (`pending` / `in_progress`), non-fixed-price projects. Completed/cancelled projects are excluded even when they have unbilled time.
- List only those eligible projects with unbilled minutes; exclude fully billed projects and projects with no time.
- Each row links to its project and shows project name, client name and a ring displaying `billed_minutes / (billed_minutes + unbilled_minutes)`.
- Use all-time minutes. Entries linked to draft invoices count as billed, matching the existing `invoice_line_id IS NOT NULL` rule.
- Sort by unbilled minutes descending; show at most five projects and a **View all projects** link when more exist.
- Header totals cover every eligible project, not merely the displayed five.

## Approaches

- **A, rejected:** add a period parameter to the dashboard endpoint. Changing the period would reload unrelated cards.
- **B, chosen:** preserve `GET /dashboard` summaries, extend its timesheets section, and add a separate module-owned cash-flow endpoint. Its card has independent fetching, loading, error and retry states.
- **C, rejected:** fetch paginated bank lines and aggregate in the browser.

### Interfaces

- Dashboard core fields stay unchanged.
- Timesheets section retains `unbilled_minutes` / `unbilled_estimate` with the new active-only scope and gains:
  - `projects: [{id, name, client_name, billed_minutes, unbilled_minutes}]` (top five).
  - `project_count` (all qualifying projects).
- `GET /api/v1/orgs/{org}/cash-flow?period=current|previous` returns `{from, to, months: [{month, incoming, outgoing}]}`. Dates use `YYYY-MM-DD`, `month` is the month's first date, and money is integer cents.
- Route lives in reconciliation, gated by its module registration and `PermReportsRead`.

## Layout and constraints

Receivables first, then full-width Cash Flow, then the existing module-card grid with the redesigned Projects card. Preserve property cards and the existing reconciliation unmatched-lines card. Invoice onboarding must not hide module content.

Use LayerChart `2.11.0`, pinned exactly, with Svelte 5 and semantic DaisyUI colours. Accessible text/table equivalents must carry chart values; percentages and statuses must not depend on colour. Support both themes and narrow layouts. Keep requests in the existing `lib/api.ts` client and domain logic in its owning module; preserve organisation and portal isolation.

Follow `docs/ui-standards.md`, update its dashboard screen ledger, and use `docs/testing.md`'s existing harnesses. Run validation sequentially through `uv run --locked` Taskfile tasks. No production/operator databases.

## Non-goals

Payables, Income & Expense, Top Expenses, Accrual/Cash toggle, Unbilled Expenses, opening-balance storage, project budgets, the Drafts tile, and dashboard visibility of closed projects' unbilled time. No new schema or general-purpose chart framework.

## Builder decisions

Exact SQL, LayerChart component selection, month-label formatting, skeleton shapes and test decomposition remain implementation details.
