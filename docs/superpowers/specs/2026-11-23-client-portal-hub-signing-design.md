# Client portal: welcome hub, projects and document signing — design

Date: 2026-11-23 · App: `apps/shigoto` · Status: draft for approval

## Intent

Turn the client portal from "see your invoices" into the home for a client's
whole engagement, in the spirit of Ignition's agreement-led onboarding but
lean. After signing in by magic link, a client lands on a hub that tells them
how to work with us, which actions we need from them before kickoff (the
checklist; outstanding invoices are not on the hub), where each of their
projects stands, and which agreements they still need to sign. Signing is
done in the app with a typed name and consent; the studio has already signed
the PDF outside the app before uploading it.

This is a document-signing feature, not a proposal-to-billing engine.

## Goals

1. **Client hub** (`/portal/{org}` for client portal users), in this order:
   1. A "documents need your signature" banner when any are outstanding.
   2. **Welcome**: a staff-written notes block for communication rules
      (which channel to use, expected response times) and a client action
      checklist of up to 5 items, each with an action, owner and due date.
   3. **Projects**: one card per project for the client, showing status and a
      "to sign" count.
   4. **Documents**: every document shared with the client, across all
      projects plus general documents (MSA, NDA), filterable by To sign,
      Signed and Shared.
2. **Project page** (`/portal/{org}/projects/{id}`): summary, goals, dates,
   primary stakeholders; then To sign, Signed and Discovery documents; then
   the project's invoices.
3. **Signing**: client views the PDF, types full legal name, ticks consent,
   signs. Shigoto records evidence and produces a signing certificate PDF.
4. **Staff management**: staff write the welcome notes, maintain the
   checklist and tick items when they have confirmed the client did them,
   add project summary/goals/stakeholders, and upload documents for signing
   or sharing.
5. **Email**: client is emailed when a document needs signing; staff are
   emailed when a client signs. Sent through the existing mailer; skipped
   silently when SMTP is unset.

## Non-goals (this release)

- Proposals, pricing, packages, acceptance-driven invoicing or payments.
- Meeting cadence (weekly check-ins, executive reviews): managed separately.
- Drawn signatures, multi-party signing, signing order, countersigning in the
  app. Staff sign before upload.
- Merging the certificate into the PDF (approach B). Can be added later from
  the same records.
- Clients ticking checklist items or uploading files against them.
- More than one portal login per client contact (existing model: one login
  per contact).
- Any change to the tenant portal's Rent/Repairs experience.
- Changing project statuses. The existing `pending`, `in_progress`,
  `completed`, `cancelled` are used as-is.

## Anti-goals (failure even if it "works")

- A client seeing any document, project, checklist or invoice of another
  contact or org. Portal isolation stays absolute; denials stay 404.
- A signature that can't be tied to the exact bytes the client saw (file
  swapped, replaced or edited after signing).
- A signed document that can be deleted, replaced or edited by staff.
- A client able to sign twice, sign a withdrawn/superseded document, or sign
  on another contact's behalf.
- Staff-only notes leaking into the portal.

## Constraints (codebase and repo rules)

- Go stdlib `net/http`, embedded Postgres migrations (next is `0025_…`),
  Svelte 5 + Tailwind/DaisyUI SPA with shared `lib/components`. API calls go
  through `web/src/lib/api.ts`. Read `docs/ui-standards.md` first.
- Portal routes use `auth.Auth.RequirePortal` and filter every query by the
  principal's `OrgID` and `ContactID` in the query layer (`internal/portal`).
- Files go through `internal/storage.Store` and the `files` table
  (`org_id`, `sha256`, `mime`, `size_bytes`, `original_name`; unique per org
  by hash). Uploads reuse existing PDF sniffing and the 10 MiB document cap.
- PDF generation uses `github.com/signintech/gopdf` (already used for rent
  statements in `internal/property/routes_statement.go`).
- Email uses `internal/mail`; when `SHIGOTO_SMTP_HOST` is unset, sending is
  skipped and nothing errors.
- CSRF header rule applies to signing POSTs. `SHIGOTO_TRUSTED_PROXY` governs
  which client IP is trusted for the evidence record.
- Editors need failed-input preservation, duplicate-submit protection,
  explicit cancel, dirty-edit preservation and busy-dismissal blocking.
- Tests in the owning layer using the shared YAML harnesses in
  `docs/testing.md`; new routes go in the ui-standards coverage ledger.

## Approaches considered

| | Approach | Trade-off |
|---|---|---|
| **A (chosen)** | Keep the uploaded PDF untouched. On signing, store an evidence record (typed name, email, time, IP, user agent, SHA-256 of the exact file) and generate a separate one-page certificate PDF linked to the document. | Two files (agreement + certificate) instead of one merged PDF. No new dependencies; reuses storage, gopdf and portal guards. |
| B | Same record, but append the certificate to a copy of the PDF. | One file for the client, but PDF page import/rewrite is fragile on unusual PDFs and doubles file/hash bookkeeping. |
| C | External e-sign (Documenso, DocuSign) via webhooks. | Stronger legal evidence, but another service to run or pay for, signing leaves the portal, webhook handling. Too heavy for typed-name consent. |

The user chose **A**.

## Design decisions (agreed)

- **Statuses**: projects keep `pending | in_progress | completed |
  cancelled`. The portal labels them Pending, In progress, Completed,
  Cancelled; cancelled projects are hidden behind a "Show cancelled (n)"
  toggle.
- **Signing method**: typed full legal name plus a consent checkbox ("I agree
  this is my electronic signature and I am authorised to sign for
  {client name}"). Whoever is signed in as the client contact signs.
- **Staff sign first**: the uploaded PDF already carries the studio's
  signature. The app never records a staff signature.
- **Welcome content and checklist**: staff-authored and staff-maintained.
  Clients read only. Staff tick an item once they have confirmed it was
  actioned.
- **Invoices on a project**: invoices with at least one line on that project
  (`invoice_lines.project_id`). An invoice spanning several projects appears
  on each. Only invoices the client portal already exposes are shown (same
  visibility rules as `portal.ListInvoices`, e.g. no drafts).

## Calls made by the designer (check these)

- **Ownership**: welcome notes and checklist belong to the client contact,
  not the org or a project.
- **Checklist size**: up to 5 *open* items shown prominently; completed items
  remain visible, ticked. Builder may cap total items at a sensible number
  (e.g. 20).
- **Project details**: add `summary`, `goals` and `stakeholders` to
  `projects`. Existing `description` stays staff-facing; `summary` is the
  client-facing text. Goals and stakeholders are plain text lists
  (stakeholders: name + role, free text). No link to contacts/users.
- **Visibility to the client**: a project appears in the portal only once it
  has portal-visible content; builder's call whether that is "always" or a
  per-project "show in portal" flag (default: show all of the client's
  projects).
- **Document kinds**: each document is `signature` (needs the client's
  signature) or `shared` (read only: discovery notes, reports). Discovery is
  a `shared` document on a project with category `discovery`; others are
  `general`.
- **Document scope**: either general (no project, e.g. MSA, NDA) or attached
  to one project. General signed documents also appear on each project's
  Signed list, labelled "applies to all projects".
- **Lifecycle of a signature document**: `draft` (staff only) → `sent`
  (client can sign; email goes out) → `signed`. Staff may `withdraw` a sent,
  unsigned document. Replacing = withdraw + upload a new version; the old one
  is kept as `superseded` and linked. Signed documents are immutable and
  cannot be withdrawn or deleted.
- **Must view before signing**: the Sign button enables after the client has
  opened the PDF in the viewer. This is a UI affordance only; the server
  does not track whether the file was viewed.
- **Lifecycle of a shared document**: same `draft` → `sent` states (staff
  action labelled "Share"); no email on share. Staff may withdraw a shared
  document. Shared documents can be deleted only while `draft`.
- **Withdrawn and replaced stay visible**: once a client has been able to see
  a document (it reached `sent`), it never silently disappears. Withdrawn
  documents stay listed as "Withdrawn" and replaced ones as "Replaced", both
  greyed, PDF still viewable, no Sign action. Draft documents are never
  visible.
- **Who can receive documents**: welcome notes, checklist and documents
  attach only to contacts with `kind = 'client'`. Portal routes serve them
  only to `client` memberships linked to that contact; a `tenant` membership
  never sees them.
- **Email recipient and signer email**: the "needs signing" email goes to the
  email of the portal user linked to the contact (the login they sign in
  with), not `contacts.email`. Send is blocked with "Invite this client to
  the portal first" when the contact has no client portal login. The
  evidence record and certificate show the signed-in user's email.
- **Email**: one email to the client's portal user on `sent` for signature
  documents (link to the signing page); one email to org owners (and the
  uploading staff user) on `signed`. No reminders.
- **Certificate timezone**: UTC plus the app-wide `SHIGOTO_TIMEZONE`.
- **Files**: new file references are registered with the existing file
  garbage collection so deleted drafts don't leak blobs; signed documents
  and certificates are never collected.
- **Module gating**: hub sections and routes are for `client` portal
  memberships when the `timesheets` module is enabled (projects live there).
  General documents and welcome content still work for clients without
  projects. Tenants keep their current portal.

## Data model (proposed, builder may refine)

- `projects`: add `summary text NOT NULL DEFAULT ''`,
  `goals text NOT NULL DEFAULT ''` (one per line),
  `stakeholders text NOT NULL DEFAULT ''` (one "Name — role" per line).
- `client_welcome` (one row per client contact): `org_id`, `contact_id`,
  `notes text`, `updated_at`, `updated_by`.
- `client_checklist_items`: `org_id`, `contact_id`, `position`, `action`,
  `owner text`, `due_date date NULL`, `done_at timestamptz NULL`,
  `done_by bigint NULL`.
- `client_documents`: `org_id`, `contact_id`, `project_id NULL`, `title`,
  `kind` (`signature|shared`), `category` (`general|discovery`), `file_id`
  (→ `files`), `file_sha256` (copied at upload for evidence), `status`
  (`draft|sent|signed|withdrawn|superseded`), `supersedes_id NULL`,
  `sent_at`, `created_by`, timestamps. Project, if set, must belong to the
  same contact (enforce in the query layer and with a check where practical).
- `document_signatures` (at most one per document): `document_id` unique,
  `user_id`, `typed_name`, `email` (copied), `consent_text` (exact wording
  shown), `signed_at`, `ip`, `user_agent`, `file_sha256` (re-hashed from
  storage at signing time and compared to the stored hash), `certificate_file_id`.
- All changes write `core.Audit` entries.

## Signing flow

1. Client opens a `sent` signature document; the viewer streams the PDF via a
   portal route scoped to their contact.
2. Client types name, ticks consent, presses Sign (CSRF header required).
3. Server, in one transaction: locks the document row; requires status
   `sent`, matching org/contact, no existing signature; re-reads the stored
   file and verifies its SHA-256 equals `file_sha256`; inserts the signature
   with trusted client IP and user agent; generates the certificate PDF
   (document title, file name, SHA-256, signer name and email, client name,
   org name, UTC and org-timezone timestamps, IP, consent text, signature
   id); stores it as a file; sets status `signed`; audits.
4. Duplicate or racing submits get 409 "already signed"; client sees the
   signed state.
5. After commit, email staff. Mail failure never rolls back the signature.

Certificate generation failure fails the whole signing (no half-signed
state).

## API surface (sketch)

Portal (all `RequirePortal`, client role, contact-scoped):

- `GET /api/v1/portal/{org}/hub` → welcome notes, checklist, project cards
  (status, to-sign count), document counts.
- `GET /api/v1/portal/{org}/projects/{id}` → project detail, documents
  grouped (to sign / signed incl. general / discovery / shared), invoices.
- `GET /api/v1/portal/{org}/documents` → all visible documents.
- `GET /api/v1/portal/{org}/documents/{id}/file` and
  `/certificate` → PDF streams.
- `POST /api/v1/portal/{org}/documents/{id}/sign` `{typed_name, consent:true}`.

Draft documents are invisible to the client. Documents that reached `sent`
and were later withdrawn or superseded remain listed ("Withdrawn" /
"Replaced") with their PDF viewable and no Sign action.

Staff (existing `RequireOrg` with `PermContactsWrite` to write,
`PermTimesheetsRead`/`PermContactsWrite` to read as fits existing patterns):

- `GET/PUT …/contacts/{id}/welcome`
- `GET/POST/PUT/DELETE …/contacts/{id}/checklist[/{itemId}]`, plus tick/untick.
- `GET/POST …/contacts/{id}/documents`, `POST …/documents/{id}/send`,
  `…/withdraw`, `…/replace`; file and certificate downloads.
- Project update gains `summary`, `goals`, `stakeholders`.

## Screens

Portal (new, client only; follow the portal shell rules):

- `portal/[org]/+page.svelte` (client hub) — currently the org landing; for
  client users render the hub. Mockup: banner, Welcome, Projects, Documents.
- `portal/[org]/projects/[id]/+page.svelte` — project page.
- `portal/[org]/documents/+page.svelte` — full documents list (hub shows a
  preview; builder may fold into the hub if simpler).
- Signing: a dedicated route (`portal/[org]/documents/[id]/+page.svelte`)
  with PDF viewer and sign panel; it's a guarded editor (failed-input
  preservation, duplicate-submit protection, Cancel).
- Portal nav for clients: Hub, Invoices.

Staff:

- A client page for welcome notes, checklist and documents. The contacts
  list has no detail route today; builder decides between a new
  `[org]/contacts/[id]` route for client contacts or a tab on an existing
  screen, following ui-standards layout rules (dialogs for short editors,
  dedicated route for upload/send).
- Project detail (`[org]/projects/[id]`) gains summary/goals/stakeholders
  editing and a Documents section for that project.

Mockup reference: `.superpowers/brainstorm/*/content/client-hub.html`
(local, untracked); meeting cadence in that mockup is out of scope.

## Testing

- Go integration: isolation (other contact, other org, tenant role, staff
  role all 404); signing happy path; double-sign/race → 409; signing a
  draft/withdrawn/superseded/signed document refused; file hash mismatch
  refuses to sign; signed document cannot be withdrawn/replaced/deleted;
  certificate contents (hash, name, timestamps) present; invoices-per-project
  query respects portal visibility.
- HTTP YAML cases for portal and staff routes (`internal/httpapi/testdata/cases`).
- Vitest + editor-contract YAML for the sign panel and staff editors.
- Playwright: invite client → staff uploads and sends → client signs →
  staff sees signed + certificate.
- Update the ui-standards coverage ledger for every new route.

## Left to the builder

- Exact table/column names, indexes, and whether checklist ordering uses
  `position` or due date.
- Whether the documents list is its own route or part of the hub.
- Staff client-page placement (new route vs tab), within ui-standards.
- Certificate layout.
- Email copy.
- Whether the Documents hub tabs are tabs, filters or grouped sections.
