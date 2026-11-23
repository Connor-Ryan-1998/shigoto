# Dashboard Receivables, Cash Flow and Projects Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace dashboard invoice-count tiles with Receivables, add independently loaded Cash Flow, and redesign the timesheets card around active projects' unbilled hours and billing progress.

**Architecture:** Keep the existing dashboard response and module/permission assembly. Extend the timesheets summary and add a separate reconciliation cash-flow endpoint; its Svelte card reloads independently when the fiscal-year selector changes. Reuse existing API, money, permission and testing utilities.

**Tech Stack:** Go, PostgreSQL, Svelte 5, Tailwind/DaisyUI, LayerChart `2.11.0`, Vitest, Playwright, shared YAML HTTP/table harnesses.

**Spec:** `docs/superpowers/specs/2026-11-21-dashboard-receivables-cashflow-design.md`

## Global Constraints

- Read app and inherited `AGENTS.md`, `docs/ui-standards.md`, and `docs/testing.md` before implementation.
- LayerChart `2.11.0`, pinned exactly. No other new direct dependency, schema change, or general-purpose chart framework.
- Money is integer cents; render through existing `Money.svelte`. Durations use h:mm.
- Timesheet header totals and rows include only active (`pending` / `in_progress`), non-fixed-price projects; entries on draft invoices count as billed.
- Header totals cover all eligible projects; the list contains the top five with unbilled time.
- Fiscal years follow the organisation's `fy_start_month`. Cash flow is imported bank activity excluding ignored lines, not opening/closing cash balances.
- Use semantic DaisyUI colours, readable chart labels/text equivalents, light/dark themes and responsive layouts.
- Keep all requests through `lib/api.ts`; domain behaviour and access checks stay server-side in their owning modules.
- No changes to the existing core dashboard response fields or property/unmatched-lines card behaviour.
- Validation commands below run from `apps/shigoto`, sequentially, using `uv run --locked task`. Use `go-task` if that is the installed name.
- Never run browser tests against operator/production databases. Commit only task-owned files; preserve unrelated work. Commit steps are execution checkpoints, not permission to publish.

## Review Focus

1. Fiscal-year rollover, January-start years and timezone midnight must select the correct twelve calendar months (Task 1).
2. Duplicate accounting sources, ignored transactions and foreign-org bank lines must never inflate cash flow (Task 1).
3. Task-rate overrides, inactive tasks and draft-linked time must preserve existing billing-estimate semantics while filtering project eligibility (Task 2).
4. Org/period switches with reversed network responses must not show another selection's financial data (Task 4).
5. Zero balances, empty projects and long names must remain finite, accessible and usable at mobile widths and in both themes (Tasks 3–5).

## File map

Paths are relative to `apps/shigoto`.

| Files | Responsibility |
|---|---|
| `internal/recon/cashflow.go`, `cashflow_test.go`, `testdata/cashflow_periods.yaml` (new) | Fiscal-year selection, monthly bank aggregation and route handler |
| `internal/recon/cashflow_integration_test.go` (new) | Cash-flow store invariants |
| `internal/recon/routes.go` | Register the permission-gated cash-flow route |
| `internal/httpapi/testdata/cases/cashflow*.yaml` (new) | HTTP validation, module/role/organisation/portal boundaries |
| `internal/timesheets/routes.go` | Extend the existing dashboard builder and response |
| `internal/timesheets/dashboard_integration_test.go` (new) | Dashboard eligibility, ranking and amounts |
| `internal/httpapi/dashboard_integration_test.go` | Update any changed summary expectations |
| `web/src/lib/modules/core/DashboardCore.svelte`, `.test.ts` | Receivables presentation and invoice-create visibility |
| `web/src/lib/modules/timesheets/DashboardCard.svelte`, `.test.ts` (test new) | Projects presentation and response type |
| `web/src/lib/modules/reconciliation/CashFlowCard.svelte`, `.test.ts` (new) | Chart, period selection and independent request lifecycle |
| `web/src/routes/[org]/+page.svelte`, `+page.test.ts` | Compose cards and retain existing onboarding/module behaviour |
| `web/package.json`, `web/package-lock.json` | Exact chart dependency |
| `web/e2e/smoke.spec.ts`, `web/e2e/dashboard.spec.ts` (new) | Update existing selectors; verify browser rendering/layout |
| `docs/ui-standards.md` | Update existing dashboard screen-ledger entry |

## Task 1: Module-owned monthly cash-flow API

**Files:** Cash-flow Go files, tables and HTTP YAML cases from the file map; modify `internal/recon/routes.go`.

**Interfaces:**
- Consume `reports.OrgFYStartMonth(ctx, q, orgID)` and `reports.FinancialYear(fyEndYear, fyStartMonth)`.
- Add `cashFlowPeriod(today time.Time, fyStartMonth int, period string) (from, to time.Time, err error)` in `internal/recon/cashflow.go`.
- Add `buildCashFlow(ctx context.Context, q core.Querier, orgID int64, from, to time.Time) (cashFlowResponse, error)`.
- Add `cashFlowHandler(w http.ResponseWriter, r *http.Request, d module.Deps)`.
- Produce `GET /api/v1/orgs/{org}/cash-flow?period=current|previous`: `{from: "YYYY-MM-DD", to: "YYYY-MM-DD", months: [{month: "YYYY-MM-01", incoming: int64, outgoing: int64}]}`.

- [ ] Write table-backed `TestCashFlowPeriod` with pinned dates: July start on `2026-06-30` gives `2025-07-01..2026-06-30`; on `2026-07-01` gives `2026-07-01..2027-06-30`; previous subtracts one year. January start on `2026-11-21` gives `2026-01-01..2026-12-31`; April start gives `2026-04-01..2027-03-31`. Include leap-year end and invalid month/period errors.
- [ ] Run `uv run --locked task test PACKAGES=./internal/recon`; confirm the new test fails before implementation.
- [ ] Implement `cashFlowPeriod` using `reports.FinancialYear`, treating January-start years specially when finding the end-year. Default missing `period` to `current` in the handler; reject explicit empty, repeated or unknown values with HTTP 422 and an error-only body.
- [ ] Add `TestBuildCashFlow` integration coverage on `internal/testkit`: pinned range `2026-07-01..2027-06-30`, credits 12000 + 3000, debit -4000 gives incoming 15000/outgoing 4000; both boundary dates included; adjacent outside dates, ignored lines and foreign-org lines excluded; matched/unmatched both included. Linked payment/expense records must not double-count. Assert twelve ascending months with zeros and `months: []` is never used for an empty year.
- [ ] Run `uv run --locked task test SUITE=integration PACKAGES=./internal/recon`; confirm aggregation tests fail before implementation.
- [ ] Implement `buildCashFlow` with one org-scoped aggregate query over `bank_lines`, positive outgoing magnitudes, inclusive dates, and twelve zero-filled months. Return query/scan errors, never partial totals.
- [ ] Add YAML cases using the existing HTTP harness: owner/bookkeeper can read; anonymous, tenant/portal and foreign-org access denied with exact error-only bodies; invalid period is 422; reconciliation-disabled org gets the same unavailable-route response as existing disabled module routes. Use separate seeded case files where module configuration differs; do not extend seed plumbing just to create bank rows.
- [ ] Run `uv run --locked task test SUITE=integration PACKAGES=./internal/httpapi`; confirm new route cases fail before registering it.
- [ ] Register the handler in reconciliation's `registerRoutes` through `d.Auth.RequireOrg(auth.PermReportsRead)`. Resolve org ID from the principal, FY month from the database and today's calendar date in `d.Cfg.Location` (UTC fallback), following the existing module clock pattern. Add a pinned timezone-midnight handler test to prove it selects the intended fiscal year.
- [ ] Run unit and integration tasks for `PACKAGES='./internal/recon ./internal/httpapi'` sequentially; expect PASS.
- [ ] Commit task-owned files: `feat: add fiscal-year cash flow API`.

## Task 2: Active-project timesheet summaries

**Files:** Modify `internal/timesheets/routes.go`, update existing dashboard integration assertions as needed; create `internal/timesheets/dashboard_integration_test.go`.

**Interfaces:**
- Keep the existing `dashboardSection(ctx, q, orgID, today)` module contract.
- Extend `timesheetsDashboard` with `Projects []dashboardProject` (`json:"projects"`) and `ProjectCount int` (`json:"project_count"`).
- `dashboardProject`: `id`, `name`, `client_name`, `billed_minutes`, `unbilled_minutes` (existing ID/minute types).
- Preserve `unbilled_minutes` and `unbilled_estimate` names, with the approved active-only scope.

- [ ] Write `TestDashboardActiveProjects` using `internal/testkit` and the module dashboard callback: include pending/in-progress, completed/cancelled, fixed-price, fully billed, no-time and foreign-org projects. Only active non-fixed projects with unbilled time appear. Include unbilled time on an archived task under an active project: task archival does not erase logged work.
- [ ] Add `TestDashboardProjectBillingProgress`: project has 90 draft-linked minutes and 30 unbilled minutes; return 90/30 (75% at render time). Include issued-linked time too; neither invoice status changes nor task archival redefine `invoice_line_id` semantics.
- [ ] Add `TestDashboardProjectTotalsAndRanking`: at least six eligible projects, descending unbilled minutes, ID ascending as deterministic tie-breaker, top five only, `project_count = 6`; header sums all six. Pin task overrides and amounts to prove the estimate still rounds per task with `core.DivRound(minutes * effective_rate, 60)`. Empty results return `projects: []`, `project_count: 0`, zero totals.
- [ ] Run `uv run --locked task test SUITE=integration PACKAGES='./internal/timesheets ./internal/httpapi'`; confirm new assertions fail.
- [ ] Extend the existing builder minimally: scope project eligibility once, retain its existing effective-rate/per-task rounding rule, aggregate billed/unbilled minutes all-time and join client names with org guards. Never compute billed minutes from invoice currency amounts; do not cap header totals at five rows. No project-budget or expense additions.
- [ ] Update existing fixtures/assertions that intentionally included inactive projects, naming these new tests as their replacement owners. Keep existing core and module-permission tests unchanged in meaning.
- [ ] Run the same integration task, then `uv run --locked task test PACKAGES='./internal/timesheets ./internal/httpapi'`; expect PASS.
- [ ] Commit task-owned files: `feat: summarize active project billing on dashboard`.

## Task 3: Receivables and Projects card presentation

**Files:** Modify `DashboardCore.svelte`, `DashboardCore.test.ts`, timesheets `DashboardCard.svelte`; create its colocated `DashboardCard.test.ts`; update affected `[org]/+page.test.ts` fixtures/assertions.

**Interfaces:**
- `DashboardCore` keeps `{core: CoreDashboard, org: string}` and reads existing session permissions for `+ New`.
- Timesheets card accepts `{data: TimesheetsDashboard, org: string}`; its exported type matches Task 2.
- Use existing `Money`, `hhmm` from timesheets `types.ts` (not `format.minutes`, which outputs `18h 15m`), and component patterns. Use DaisyUI radial-progress for per-project rings; no extra chart abstraction.

- [ ] Replace tile tests with `rendersReceivablesSplit`: existing fixture 123456 open / 7890 overdue renders total `$1,234.56`, Current `$1,155.66`, Overdue `$78.90`, correct org-scoped links and no Drafts tile. Add `zeroBalanceHasFiniteBar`, `allOverdueBar`, and `newInvoiceRequiresWritePermission` with allowed/denied sessions.
- [ ] Add Projects rendering tests: 90 billed / 30 unbilled displays `75%`; header 1095 minutes displays `18:15`; client/project labels and org-scoped links render; `project_count = 6` with five rows shows View all projects. `project_count <= 5` hides it. Empty list renders explanatory text, not a percentage divide-by-zero. Values are supplied by the API; do not recalculate header sums from top-five rows.
- [ ] Run `uv run --locked task web:test`; confirm new assertions fail.
- [ ] Replace the three invoice tiles with the approved Receivables card, separated header/body, labeled split bar and numeric legend. Use existing create-invoice route and permission convention; zero total renders a neutral track without invalid widths. Keep `CoreDashboard` backward-compatible.
- [ ] Restyle Timesheets as **Projects** with **Unbilled Hours**, estimate and project rows, matching the supplied reference hierarchy. Display nearest whole billed percentage with numeric accessible text. Do not invent an interactive tab when only one metric exists.
- [ ] Update dashboard page tests that reference old tiles, and timesheets response fixtures to include the new fields. Preserve zero-invoice vs settled-org coverage; the removed draft-count UI test is intentionally retired because that UI was explicitly removed.
- [ ] Run `uv run --locked task web:test`; expect type checks and tests PASS.
- [ ] Commit task-owned files: `feat: redesign receivables and project dashboard cards`.

## Task 4: LayerChart cash-flow card and dashboard composition

**Files:** New `CashFlowCard.svelte` / `.test.ts`; modify web package files and `[org]/+page.svelte` / `.test.ts`.

**Interfaces:**
- `CashFlowCard` accepts `{org: string}`.
- Call `api<CashFlowResponse>('GET', '/api/v1/orgs/' + org + '/cash-flow?period=' + period)` through the existing client, using Task 1's response contract.
- Page renders the card only when its dashboard response contains the reconciliation section, not merely based on local feature flags.

- [ ] Add exact dependency with `npm install --save-dev --save-exact layerchart@2.11.0` in `web/`; inspect manifest/lockfile for unrelated changes. Read installed 2.11.0 API/types before choosing components; do not follow obsolete v1 examples or install separate D3 packages.
- [ ] Write `CashFlowCard` tests using shared `mockApi`, `deferred` and route/session setup: initial current-year request; previous-year selection changes only cash-flow requests; monthly totals 15000/4000 display `$150.00`, `$40.00`, net `$110.00`; outgoing-only period has negative net. Twelve zero months show a valid zero state, not an error.
- [ ] Add `failedRequestRetriesSelectedPeriod`, `latePeriodResponseIgnored`, `orgChangeClearsOldData`, and `lateOldOrgResponseIgnored`, including stale failures and unmount. Summaries/labels must always correspond to the current org and selected period; old data must not remain under a new heading.
- [ ] Add page composition tests: invoice-empty state does not hide Cash Flow or module cards; missing reconciliation section causes neither cash-flow card nor request; existing unmatched-lines and property content remain. Replace broad `apiMock.mockResolvedValue` setups in affected tests with URL-specific shared `mockApi` routes so new requests cannot silently receive dashboard payloads.
- [ ] Run `uv run --locked task web:test`; confirm new tests fail.
- [ ] Implement a responsive grouped monthly bar chart with LayerChart, success incoming/error outgoing, labeled months and money-valued tooltips. Render Incoming, Outgoing and Net alongside on desktop and below on mobile. Use server-returned dates for range labels; do not duplicate FY arithmetic in JS. Compute only display totals/net from the twelve returned monthly values.
- [ ] Include a semantic monthly values table (may be screen-reader-only), chart description and visible series labels. Own loading/error/retry states and period control label; keep this failure independent from other dashboard cards. Use a request-generation guard and cleanup for out-of-order requests.
- [ ] Mount Receivables first, full-width Cash Flow second, existing module grid third. Ensure dashboard load/org changes clear stale module visibility and data; preserve lazy module-card imports and current API-client/error patterns.
- [ ] Run `uv run --locked task web:test`, then `uv run --locked task build`; expect PASS with static production build. Resolve chart/browser API compatibility with minimal scoped test setup only if needed; don't mock away chart data or request behaviour.
- [ ] Commit task-owned files: `feat: add interactive dashboard cash flow chart`.

## Task 5: Browser verification and standards

**Files:** `web/e2e/smoke.spec.ts`, new `web/e2e/dashboard.spec.ts`, `docs/ui-standards.md`.

**Interfaces:** Use `startStack`, `signIn` and existing isolated fixtures from `web/e2e/stack.ts`. No real accounts or stored credentials.

- [ ] Update smoke's old Open invoices tile/count selectors to Receivables amounts and Current links, retaining its org-switch navigation coverage.
- [ ] Add dashboard browser coverage with imported bank activity and eligible project time: chart renders with a nonzero container and bars; period selector operates without reloading unrelated cards; project and invoice links navigate correctly. Use the existing API/import fixtures rather than adding production hooks.
- [ ] Add a narrow viewport (390px) and desktop check: no horizontal document overflow, no clipped controls, long project/client labels wrap or truncate accessibly. Exercise both existing themes; verify legends, ring labels and chart remain readable and resize correctly. Limit browser assertions to composition/layout, not duplicated HTTP permission or SQL edge cases.
- [ ] Run `uv run --locked task web:e2e`; first demonstrate changed/new browser checks fail against old behaviour where practical, then PASS against implementation. This builds and uses isolated test databases; stop if Docker/Chromium is unavailable and record the blocker.
- [ ] Update the existing dashboard screen-ledger entry with Receivables, independent Cash Flow, active Projects scope, chart accessibility and test ownership. Do not add a new screen route.
- [ ] Run final validation sequentially: `uv run --locked task build`; `uv run --locked task lint`; `uv run --locked task test`; `uv run --locked task test SUITE=integration`; `uv run --locked task web:test`; `uv run --locked task web:e2e-run`. Each must exit 0; record blocked checks explicitly rather than claiming success.
- [ ] Review `git diff --check` and scoped diff for generated files, credentials, unrelated lockfile churn and accidental behaviour changes. Confirm all spec requirements map to Tasks 1–5.
- [ ] Commit task-owned files: `test: cover dashboard charts and responsive layout`.

## Handoff / known limits

- Imported bank movement is not a bank balance and may include transfers between imported accounts; transfer elimination and import-history completeness are out of scope.
- Current Receivables links to the existing Open filter, which includes overdue invoices; no new invoice-filter semantics are introduced.
- Completed/cancelled projects' unbilled work deliberately disappears from dashboard totals, remaining available on Time/Projects pages.
- No product code, dependency install or database mutation is part of writing this plan.
