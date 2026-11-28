# Development and operations

Detailed setup, configuration and workflow reference. Start with the
[README](../README.md) for an overview and quickstart.

See [Interface standards](ui-standards.md) before extending the UI. It covers
shared components, data interactions, layouts, accessibility, and portal boundaries.

## Tenant portal

Tenants invited from Members sign in by emailed link. With the property module on they get:

- **Rent**: summary, running-balance history, printable payment receipts, and statements for a financial year or custom range as PDF or CSV. Everyone on the lease sees the whole lease account. Staff download the same statement from the lease page.
- **Repairs**: report a repair from a phone with photos, video and voice notes; urgent repairs show the org's emergency phone (Settings → Repairs). Tenants follow progress on the ticket page, offer times that suit, confirm the booked visit, and grant or withdraw entry permission for that visit only. Rescheduling clears consent.

Staff manage repairs on each property's Maintenance tab; open repairs also appear on the dashboard. No notifications are sent yet: the ticket timeline is the record.

## Client portal

Client contacts invited from Members sign in by emailed link and land on a **Hub**:

- **Welcome notes**: staff-written communication rules (channels, response times).
- **Client action checklist**: what staff need before kickoff, with owner and due date. Staff tick items once they've confirmed them; clients only read.
- **Projects** (timesheets module): status cards. A project page shows the client-facing summary, goals and stakeholders, its documents (To sign, Signed, Discovery, Shared) and its invoices.
- **Documents**: everything shared with or sent to the client, across projects plus general documents such as an MSA.

Staff manage this from the client page (Contacts → client name) and the project page. Upload a PDF as **needs signature** or **shared only**, then Send/Share it. Withdraw or replace a sent document; the client still sees the old one marked Withdrawn/Replaced. Signed documents can't be changed.

**Signing.** The studio signs the PDF before uploading it. The client opens it, types their full legal name and ticks a consent box. Shigoto records name, email, time, IP, browser and the SHA-256 of the exact file, and stores a separate signing-certificate PDF; the uploaded file is never modified. With SMTP configured the client is emailed when a document needs signing, and owners plus the uploader when it's signed.

## Quotes

With the `quotes` module (it needs `timesheets`; both are on by default) staff build quotes under **Quotes**:

- **Lines**: each line is **labour** (hours at an hourly rate) or **material** (quantity at a unit price), with a GST code. GST is worked out per line, as on invoices.
- **Customer and project**: a quote is for a client contact and can be linked to one of their open projects.
- **Attachments**: a scope of work, site photos or the customer's evidence can be attached for internal records. They never appear on the PDF or in the portal.
- **Lifecycle**: draft → sent → accepted or declined. Only drafts can be edited. **Mark sent** saves the PDF once, so the customer's copy never changes, even if the business profile changes later. A sent quote past its valid-until date shows as **Expired**; staff can still accept or decline it. **Revise** copies a sent or declined quote into a new draft (`Q-0007-R2`); sending it supersedes the earlier revision.
- **Accept** (needs permission to create projects): with no linked project it creates a pending, fixed-price project with no hourly rate, named after the quote. Each labour line becomes a task at its quoted rate; material lines stay on the quote. Name clashes get the quote number appended. Invoicing stays manual.

A project's **Costs** tab (quotes module plus `reports_read`) compares the accepted quotes with what has been invoiced and the expenses tagged to the project, all ex GST.

Clients see their sent quotes (never drafts) on the portal hub and project pages, and can download the PDF. Quotes are view-only in the portal.

**Settings → Business profile** holds the address, phone, email, logo, licences (free-form type and number, for example `QBCC` / `1234567`) and default quote terms printed on quotes.

## Requirements

Docker plus [go-task](https://taskfile.dev), with Go on the host. Node 24 is
only needed to build or develop the web UI (`task web:*`); `go build`/`go
test` work without it (`internal/webui` serves a built-in placeholder page
when `internal/webui/dist` has no build in it).

On a machine where Docker build traffic is TLS-intercepted (a corporate
laptop CA), pass it as a build secret:

```bash
docker build --secret id=laptop_ca,src=/opt/homebrew/etc/ca-certificates/cert.pem -t shigoto .
```

## Quickstart

```bash
docker compose up -d --build

# create the first org and its owner (password is read from stdin)
read -rs -p "Owner password: " OWNER_PASSWORD; printf '\n'
printf '%s\n' "$OWNER_PASSWORD" | docker compose exec -T app /shigoto admin create-owner \
  --org-slug me --org-name "Me" --email me@example.com
unset OWNER_PASSWORD

# log in and call the API with the session cookie
# Prefer signing in through http://localhost:8080.
# API clients must keep session cookies private and use the CSRF header below.
```

### API notes

- Every unsafe request (`POST`, `PUT`, `PATCH`, `DELETE`) to `/api/` must
  send `X-Shigoto-CSRF: 1`, or it is rejected with 403. If the request has
  an `Origin` header, it must match `SHIGOTO_BASE_URL`'s origin. `/webhooks/`
  is exempt. A browser form on another site can't set custom headers, so
  this blocks cross-site request forgery.
- Every error body is `{"error": "<message>"}`.
- Paged list endpoints return the page plus the total match count (ignoring
  `limit`/`offset`): `GET /api/v1/orgs/{org}/invoices` returns
  `{"invoices": [...], "total": n}` and `GET /api/v1/orgs/{org}/bank-lines`
  returns `{"lines": [...], "total": n}`. `limit` defaults to 50, max 200.

## Usage

```bash
task test                    # unit tests (SUITE=unit, default)
task test SUITE=integration  # brings up db via docker compose, then integration tests
task build                   # builds bin/shigoto (includes task web:build)
task dev                     # runs db in docker compose, then the server locally
task lint                    # gofmt -l, go vet, and task web:check
```

The database is PostgreSQL 18 Alpine with pgvector, built from
`deploy/postgres/Dockerfile` (CI publishes it as
`ghcr.io/connor-ryan-1998/shigoto-postgres`). Keep `PGVECTOR_VERSION` in that
Dockerfile and in `.github/workflows/validate.yml` in step. After changing
either pin, rebuild locally with `docker compose build db`.

```bash
task web:install              # npm ci in web/
task web:build                # builds the SPA into internal/webui/dist
task web:test                 # svelte-check plus vitest (--run)
task web:dev                  # vite dev server, proxying /api and /webhooks to task dev's backend
task web:e2e                  # Playwright smoke test against a real binary + Postgres
task verify                   # sequential build, lint, race/integration, frontend and browser checks
task audit                    # networked Go + full npm vulnerability checks; findings fail the task
```

Before the first browser run, install its browser with
`task web:install` then `(cd web && npx playwright install chromium)`.
Linux CI may also need Playwright's `--with-deps` system packages. On hosts where
the binary is named `go-task`, use that instead of `task`.

`task verify` uses local Compose Postgres/Mailpit, fresh integration-test schemas,
and dedicated `shigoto_e2e*` databases which browser tests recreate. Never point
this harness at an operator/production database. Do not run verification tasks
concurrently: `npm ci` replaces dependencies, builds share output, and browser
fixtures recreate their databases. Integration tests have a 10-minute per-package
timeout, with Go stack traces on timeout; CI preserves failure logs and browser
artifacts. When sharing a local test Compose stack between worktrees, set
`COMPOSE_PROJECT_NAME` to that stack's project name to avoid host-port conflicts.

### Pre-deployment risks

The hardening pass adds regressions for settings audit redaction, login limiter
accounting, report allocation overflow and sending an invoice after a concurrent
void, full or partial payment. Provider sends now refuse invoices with any
recorded payment rather than sending their original full balance. Real-browser
checks also cover financial forms, expiry/re-login,
time billing, lease/rent-run idempotency, reconciliation and CSV reporting.

**A green `verify` is not a clean vulnerability audit.** As of 2026-11-18,
`task audit` reports high-severity ReDoS advisory GHSA-fc5q-39xc-vjpf in
`picomatch@4.0.7`, used only by build and test tooling (vite, vitest,
svelte-check, tinyglobby). 4.0.7 is the registry's latest version and is
affected. Those tools match repository-controlled globs, not runtime request
input, and picomatch is not in the shipped bundle or Go binary; this is not proof
that every build-tool input is safe. On 2026-11-18 the operator approved a scoped
exception for this one advisory on `picomatch`, expiring 2026-12-18. `task audit`
runs `web/scripts/audit.mjs`, which fails on any other advisory and on the
exception once it expires. Re-review it then, and remove it once an upstream fix
exists. The earlier deepmerge exception (GHSA-gcqx-w48m-w2h4) was removed once
the advisory no longer appeared.

Historical settings audit records have **not** been rewritten and may still
contain bank details. Existing limitations also remain: credit notes/refunds and
overpayments, billed-period financial adjustments/lease gaps, receipt rollback cleanup, bounded
memory/concurrency under abuse, aggregate/extreme-input arithmetic, and complete
review of all property/reconciliation paths. Payment reconciliation after an
invoice was already sent, provider idempotency expiry and provider/DB commit
recovery also still need acceptance/review. Live OIDC/Stripe acceptance,
failure-injection/load testing and container
image vulnerability scanning are still required as applicable.

Rent changes, charges and terminations now save with advisory warnings and invoice
links when their dates overlap existing non-void rent invoice periods (including
drafts). Existing invoices are never recalculated by these lease actions. Charge
warnings use conservative date overlap, not a calculation of the exact adjustment.
The panel reflects invoices visible during the save and is not retained across a
full page reload; it does not provide concurrent scheduler/provider reconciliation,
automatic credit notes or refunds.

### Web UI dev workflow

Run the backend and the web dev server side by side:

```bash
task dev       # terminal 1: Postgres + the Go server on :8080
task web:dev   # terminal 2: Vite on :5173, proxying /api and /webhooks to :8080
```

Open `http://localhost:5173`. The dev proxy rewrites the proxied request's
`Origin` header to the backend's own origin, so the CSRF check in
`internal/httpapi/csrf.go` still passes even though the browser's real
origin is Vite's port, not the backend's.

In production the SPA is built (`task web:build`) into
`internal/webui/dist` and embedded in the Go binary
(`internal/webui/webui.go`), served for any path that isn't `/api/`,
`/webhooks/` or `/healthz`. `go build`/`go test` don't need Node: only
`internal/webui/dist/.gitkeep` is committed (everything else under `dist`
is gitignored, since `task web:build` empties the directory before writing
to it), and `webui.Handler()` serves a built-in "Web UI not built" page
when `index.html` is missing.

## Configuration

### Published container releases

After the existing verification and audit jobs pass, CI builds linux/amd64 and
linux/arm64 images. Non-PR `main` builds publish
`ghcr.io/connor-ryan-1998/shigoto:sha-<full-commit>` and `:latest`; version tags
such as `v0.1.0` additionally publish `:v0.1.0` and `:0.1`. Prereleases do not
update the `:0.1` alias. PRs only build. Tag releases from a reviewed main commit.
The PostgreSQL companion image also gets the exact version tag.
The source repository remains `Connor-Ryan-1998/shigoto` (spelling intentional).
Make the GHCR package public or provide protected read-only pull credentials on
the deployment host. Never bypass a failing audit to publish a release.

For local evaluation, `deploy/compose.images.yml` replaces only the app build
with the public `:latest` image; PostgreSQL still builds from the pinned
Dockerfile:

```bash
docker compose -f docker-compose.yml -f deploy/compose.images.yml pull app
docker compose -f docker-compose.yml -f deploy/compose.images.yml up -d --build --wait db mailpit app
```

Requires Docker Compose 2.24.4+ for `!reset`. To evaluate a specific release,
use `docker pull ghcr.io/connor-ryan-1998/shigoto:v0.1.0` after that release is
published, then select that image in a local Compose override. Production should
use reviewed immutable SHA tags and separate protected configuration.

Production deployment selects a reviewed published SHA tag, not `latest`, and
runs a dedicated private Postgres alongside the app. Do not use development
Compose credentials, exposed ports or Mailpit in production. Persist both the
database and `SHIGOTO_FILES_DIR`; startup applies migrations, so an older image
alone cannot undo database changes. Owner bootstrap is a one-time manual action,
not a deployment task. The scratch image has no shell-based healthcheck.

The infrastructure repository's `ansible/roles/shigoto/README.md` describes ERP2
deployment, protected configuration and first-owner commands. That initial role
does **not** add Shigoto to backups or register an Authentik application. Do not
admit real data until backup/recovery is addressed. Changing Postgres's initial
password file does not rotate the password of an initialized database.

The server is configured by environment variables, all prefixed `SHIGOTO_`:

| Variable | Required | Default | Description |
|---|---|---|---|
| `SHIGOTO_LISTEN` | no | `:8080` | Address to listen on. |
| `SHIGOTO_DATABASE_URL` | yes | | Postgres connection URL. |
| `SHIGOTO_MODULES` | no | all of `property,timesheets,reconciliation,quotes` | Comma list of modules to enable. `quotes` requires `timesheets`. |
| `SHIGOTO_FILES_DIR` | no | `./data/files` | Directory for uploaded files. |
| `SHIGOTO_UPLOAD_MAX_BYTES` | no | `1073741824` (1 GiB) | Per-file cap for maintenance photo/video/voice uploads; `0` = unlimited. Document uploads stay capped at 10 MiB. |
| `SHIGOTO_UPLOAD_MIN_FREE_BYTES` | no | `5368709120` (5 GiB) | Uploads are refused with 507 when the files volume has less free space than this. |
| `SHIGOTO_BASE_URL` | yes | | Public base URL, used for webhooks and links. |
| `SHIGOTO_TIMEZONE` | no | `Australia/Brisbane` | Timezone invoice dates (issue_date, due_date) are calculated in. |
| `SHIGOTO_SCHEDULER` | no | `on` | `on` or `off`. Enables the in-process hourly scheduler (rent runs and session pruning). |
| `SHIGOTO_STRIPE_<ORGSLUG>_SECRET_KEY` | no | | Stripe secret key for the org identified by slug. |
| `SHIGOTO_STRIPE_<ORGSLUG>_WEBHOOK_SECRET` | no | | Stripe webhook signing secret for the org. |
| `SHIGOTO_SMTP_HOST` | no | | SMTP server host. Unset disables email: `POST /api/v1/orgs/{org}/invites` still returns `201 {invite_id, link, emailed:false}` and `.../portal-invite` still returns `200 {emailed:false, link?}` (the staff invite link is always handed back for manual sharing; the portal-invite link only when the person belongs to no other org); only `POST /api/v1/auth/magic` returns `422 "email is not configured"`, since that flow has no other way to hand back a usable link. |
| `SHIGOTO_SMTP_PORT` | no | `587`/`465`/`1025` by `SHIGOTO_SMTP_TLS` | SMTP server port. |
| `SHIGOTO_SMTP_USERNAME` | no | | SMTP auth username. |
| `SHIGOTO_SMTP_PASSWORD` | no | | SMTP auth password. |
| `SHIGOTO_SMTP_FROM` | yes, if `SHIGOTO_SMTP_HOST` is set | | `From` address for outgoing mail. |
| `SHIGOTO_SMTP_TLS` | no | `starttls` | `starttls`, `tls` (implicit TLS, e.g. port 465) or `none`. `none` is only accepted for host `localhost`, `127.0.0.1`, `::1` or `mailpit`. |
| `SHIGOTO_TRUSTED_PROXY` | no | | CIDR of a reverse proxy to trust. When set and a request's `RemoteAddr` is inside it, the right-most `X-Forwarded-For` entry that isn't itself inside the CIDR is used as the client IP for login rate limiting. |
| `SHIGOTO_OIDC_ISSUER` | no | | OIDC issuer URL for staff SSO (e.g. an Authentik application's issuer). Setting this, `SHIGOTO_OIDC_CLIENT_ID` or `SHIGOTO_OIDC_CLIENT_SECRET` requires all three together - there's no partially-configured OIDC. |
| `SHIGOTO_OIDC_CLIENT_ID` | no | | OIDC client ID. |
| `SHIGOTO_OIDC_CLIENT_SECRET` | no | | OIDC client secret. |
| `SHIGOTO_OIDC_SCOPES` | no | `openid email profile` | Space-separated OIDC scopes to request. |

`<ORGSLUG>` is the org slug upper-cased with `-` replaced by `_`. Point each
org's Stripe webhook at `https://<SHIGOTO_BASE_URL>/webhooks/stripe/<org-slug>`
(the plain, lower-cased slug, not the env var form).

### Repository setup and releases

Maintainers can enable Discussions and private vulnerability reporting with
GitHub CLI (repository admin permissions required):

```bash
gh repo edit Connor-Ryan-1998/shigoto --enable-discussions
gh api --method PUT repos/Connor-Ryan-1998/shigoto/private-vulnerability-reporting
```

Upload `docs/social-preview.png` in **Settings → General → Social preview**.
Keep the About description/topics accurate; add a homepage only when a public
docs site or demo exists. Choose real, bounded contributor tasks before opening
issues labelled `good first issue`.

For a release, merge and push the reviewed change to `main`, wait for its
verification/audit/image jobs to pass, then tag that exact commit:

```bash
git tag v0.1.0 <reviewed-commit>
git push origin v0.1.0
```

Wait for the tag-triggered workflow to pass and both image jobs to publish.
Then create the public release:

```bash
gh release create v0.1.0 --verify-tag --generate-notes --title "v0.1.0"
```

Use the next unused version for later releases; never move a published tag.
The first generated changelog may need trimming. Include known limitations,
upgrade/backup instructions and the exact container tag in release notes.

### OIDC staff sign-in

Setting `SHIGOTO_OIDC_ISSUER`, `SHIGOTO_OIDC_CLIENT_ID` and
`SHIGOTO_OIDC_CLIENT_SECRET` (plus, optionally, `SHIGOTO_OIDC_SCOPES`)
enables "Sign in with SSO" on the
login page, for staff (owner/staff/bookkeeper) accounts only - portal
(tenant/client) users always sign in via magic link, never SSO. The redirect
URI to register with the identity provider is always
`<SHIGOTO_BASE_URL>/api/v1/auth/oidc/callback`.

OIDC never auto-provisions an account. Signing in links an existing shigoto
user by email: a user must already exist (created by accepting a staff
invite) for OIDC sign-in to work, and the ID token's `email_verified` claim
must be `true`. An unrecognised email is rejected with "No shigoto account
for this email — ask an owner to invite you." Linking clears the account's
local password, if it had one: SSO has just proven ownership of that email
from the IdP's side, so the account should only be reachable through SSO
from then on.

Any one of `SHIGOTO_OIDC_ISSUER`, `SHIGOTO_OIDC_CLIENT_ID` or
`SHIGOTO_OIDC_CLIENT_SECRET` set without all three (`SHIGOTO_OIDC_SCOPES` is
optional, defaulting as above) fails config loading at startup rather than
starting with SSO half-configured.

With [Authentik](https://goauthentik.io/), create an OAuth2/OpenID Provider
with client type "Confidential" and the redirect URI above, an Application
pointing at it, and use the provider's issuer URL, client ID and client
secret for the three required variables. No secret belongs in this
repository or its docs - keep it in the same vault-backed secret store as
every other credential.

### Email in development

`docker compose up` also starts [mailpit](https://mailpit.axllent.org/) as a
local SMTP catcher, with its SMTP port (1025) and web UI (8025) bound to
`127.0.0.1` only. The `app` service is pre-configured to send through it
(`SHIGOTO_SMTP_HOST=mailpit`, `SHIGOTO_SMTP_TLS=none`); open
`http://localhost:8025` to see mail sent by the running app. `task dev` runs
the Go server on the host rather than in compose, so point it at mailpit the
same way if you need outgoing mail locally:

```bash
docker compose up -d --wait db mailpit
SHIGOTO_SMTP_HOST=localhost SHIGOTO_SMTP_PORT=1025 \
  SHIGOTO_SMTP_FROM=noreply@shigoto.local SHIGOTO_SMTP_TLS=none \
  task dev
```

## Commands

```bash
bin/shigoto serve                     # start the server (default)
bin/shigoto admin create-owner \
  --org-slug <slug> --org-name <name> --email <email>  # password read from stdin
bin/shigoto admin populate-sbx        # replace owned local sandbox data
```

### Populate local testing data

Build/start the local stack, then populate the existing development database:

```bash
docker compose up -d --build
docker compose exec -T app /shigoto admin populate-sbx
```

Local Compose stores uploaded and sandbox media in the `files` named volume,
which survives container rebuilds alongside the `pgdata` database volume.

**Development only. Rerunning replaces all records and tester edits inside the
command's sandbox organisations, and rotates the generated staff passwords.**
Unrelated development organisations and accounts remain unchanged. Population
refuses existing slug/email collisions and external dependencies instead of
adopting or deleting unrelated data. A failed population rolls back the reset.
Do not point this command at an operator/production database.

The command creates `sbx-property` and `sbx-consulting`, with properties, active
and ended leases, rent changes/charges and ledgers, projects/tasks, billed and
unbilled time, expenses, bank imports and matched/unmatched lines. More than 60
invoices cover drafts, upcoming/overdue balances, partial/full payments and voids.
Dates are relative to the population day in `SHIGOTO_TIMEZONE`.

In `sbx-property`, **River Cottage → Maintenance** contains new, viewed,
assigned, scheduled, completed and cancelled repairs. Scheduled examples include
unconfirmed visits, tenant-home access, permitted entry, withdrawn permission
and a request for a different time. Each has availability and a fictional
supplier; reviewed repairs contain shared messages and staff-only notes.
Garden Terrace and `sbx-consulting` have separate tenant repair examples.
Empty tenant accounts stay empty.

Sign in as `tenant@example.test` to exercise these examples under **Repairs**.
The new **Kitchen tap leaking** request includes an illustrated PNG, a tiny MP4
and a spoken demo voice note. These fictional fixtures are bundled in the
binary and written into `SHIGOTO_FILES_DIR`; population needs no media downloads
or local encoders. The emergency phone is a fictional example, not an emergency
service; **do not call it**.

Sign in as `client@example.test` (in `sbx-property`) to exercise the client
portal. The **Hub** shows welcome notes, a five-item checklist (two confirmed),
project cards and a "needs your signature" banner. **Reporting setup** has a
summary, goals, stakeholders, a **Statement of Work** to sign, discovery notes
and a draft change request staff can see but the client can't. Documents also
include a signed **Master Services Agreement** (with its signing certificate)
and a withdrawn NDA. `client-beta@example.test` has one document to sign in
`sbx-consulting`; `client-empty@example.test` sees an empty hub. Staff manage all
of this from **Contacts → Client name** and the project page. The PDFs are
generated fictional documents with no legal effect.

River Cottage's **Rent** account also includes a written-off rent invoice and
a separate write-off with partial recovery. Existing partial/full payments
provide printable receipts and PDF/CSV statement examples.

After success, the command prints local URLs and fresh owner/staff/bookkeeper
passwords. The owner can switch between both organisations. Portal accounts have
**no password**: open the normal login page, request a magic link for one of the
printed tenant/client emails, and open its message at
<http://localhost:8025>. Use a separate browser profile or private window to test
different roles. Empty tenant/client accounts exercise empty portal states;
other tenants exercise same-organisation and cross-organisation isolation.

Population sends no mail and calls no payment providers. Keep local SMTP pointed
at Mailpit and do not configure live payment credentials for either sandbox
organisation. Uploaded files (including bundled demo media) lose their database
references on reset; old bytes are not deleted from disk by this command.
If a new population fails before commit, it rolls back database changes and
discards that attempt's media, preserving the previous sandbox. If a database
connection fails during commit, the outcome may be unknown: media is retained
to avoid broken file references. Rerun `populate-sbx` to restore known access details.

#### Strata assistant in the sandbox

`sbx-property`'s Garden Terrace Body Corporate holds the chairperson and
treasurer positions, so its Strata tab shows both assistants and the Documents
card. The assistant is off until a model is configured: put the settings in a
`.env` file next to `docker-compose.yml` (git-ignored; never commit keys), then
`docker compose up -d app`:

```bash
SHIGOTO_LLM_BASE_URL=https://openrouter.ai/api/v1
SHIGOTO_LLM_API_KEY=<your key>
SHIGOTO_LLM_MODEL=deepseek/deepseek-v4.1-flash
# Optional semantic search (the endpoint must honour "dimensions"):
SHIGOTO_EMBED_BASE_URL=https://generativelanguage.googleapis.com/v1beta/openai/
SHIGOTO_EMBED_API_KEY=<your key>
SHIGOTO_EMBED_MODEL=gemini-embedding-2
```

Uploaded PDFs are sent to these providers and each upload and question costs
real API usage. Upload fictional documents only. The indexer runs with the
scheduler (on by default in Compose). Rerunning `populate-sbx` deletes the
sandbox's strata documents and everything read from them.

## Money and GST rounding

All money is represented in cents as `int64`. GST is calculated per invoice or expense line and rounded half away from zero to the cent. The rounding rule for n/d is: for n≥0, use (2n+d)/(2d); for n<0, negate, compute, and negate the result. This ensures that 0.5 cents rounds away from zero in both directions.

Rent, charge and `opening_arrears` amounts on a lease are entered GST-exclusive. Residential rent is input-taxed (`INP`), so no GST is added. For non-residential property, GST is added on top when the org is GST-registered (`GST`); otherwise the line is GST-free (`FRE`). A non-residential rent of $500.00 therefore bills $550.00 for a registered org.

## Rental property tax reports

Each property has a page (pencil icon on the Properties list) with Details,
Bills, Depreciation and Notes tabs. Bills are the expenses tagged to the
property; there is no separate bill record. Depreciation items hold the yearly
amounts from a quantity surveyor's report (or the owner's own figures); Shigoto
records them and never calculates depreciation.

Reports → Rental property tax produces, per financial year (named by its end
year, following the org's `fy_start_month`), an Accountant report and an
Item 21 (ATO rental schedule) report, each as a printable page or CSV. Rent is
counted when received; bond payments are excluded. Expenses land on the Item 21
label set on their category (Reports → Item 21 labels); unmapped categories
count as Sundry with a warning. Expense lines in `capital` categories are never
deductions: claim them through depreciation items, which feed Capital
allowances (plant and equipment) or Capital works deductions. Amounts assume
100% ownership.
