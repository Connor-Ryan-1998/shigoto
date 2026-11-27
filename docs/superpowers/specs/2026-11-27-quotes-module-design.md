# Quotes module — design

Date: 2026-11-27 · App: `apps/shigoto` · Status: draft for approval

## Intent

A friend who works as a tradie will use Shigoto for small scoping jobs. A job
starts with a quote, which Shigoto cannot produce today. This adds a small
quoting module: staff build an itemised quote for a customer (optionally
against an existing project), keep supporting files with it for their own
records, download a PDF to send, and, once the customer says yes, turn it
into a project (= job) with tasks for the labour. Project pages gain a Costs
tab so actual spend can be compared to what was quoted.

The same module must suit a consulting studio (a statement of work as the
attachment) and a tradie (photos or customer evidence that justify the
price).

## Goals

1. **Quotes**: a quote belongs to one client contact and optionally one
   project in the same org. It has a title, its own number series (separate
   from invoices), an issue date, a valid-until date, terms/notes, line
   items and totals.
2. **Line items**: each line is `labour` (hours × hourly rate) or
   `material` (quantity × unit price), with a description and GST code.
   GST is computed per line with the same rules and rounding as invoice
   lines (`GST` only when the org is GST-registered).
3. **Attachments**: any number of files per quote, stored with the existing
   `files` storage. Internal only: never on the PDF, never in the portal.
4. **PDF**: generated on demand with `gopdf`, downloadable by staff and by
   the customer in the portal. Shows the org profile (below), customer,
   quote number and revision, dates, lines grouped or marked by kind, GST
   and totals, and terms.
5. **Lifecycle**: `draft → sent → accepted | declined | expired`, plus
   `superseded`.
   - Only drafts are editable.
   - Staff mark a quote sent (after downloading and sending it themselves),
     accepted or declined.
   - A sent quote past its valid-until date shows as expired. Expiry is a
     nudge for the customer, not a lock: staff can still accept or decline
     an expired quote at their discretion.
   - Revising a sent, expired or declined quote creates a new draft copy
     numbered as a revision (`Q-0007-R2`) and marks the old one superseded
     when the revision is sent. Accepted quotes can't be revised (changes
     after acceptance are variations, a non-goal). Only one revision of a
     quote can be accepted.
   - When a quote is marked sent, its PDF is rendered once and stored in
     `files`. Staff and the portal always download that stored PDF, so a
     later change to the org profile or logo never alters what the customer
     received. Drafts render on demand.
6. **Accept → project**: accepting a sent or expired quote, in one
   transaction:
   - If the quote has no project: creates a project for the quote's
     customer, named from the quote title, status `pending`, billing method
     `fixed`, no hourly rate, GST code from the org's registration, and
     links the quote to it.
   - If it has a project: uses that project. Accept is blocked when that
     project is `completed` or `cancelled`. The project's billing method
     and rate are left unchanged.
   - Creates one task per **labour** line, named from the line description,
     with the line's rate as the task's hourly rate override. Quoted hours
     are not copied: tasks have no estimate field, and the Costs tab
     carries the quoted-vs-actual comparison. If the name clashes
     with an existing task on that project, the quote number is appended
     ("Rough-in (Q-0007)").
   - Material lines create nothing; they stay on the quote.
   - Marks the quote accepted with who and when.
7. **Nullable project hourly rate**: `projects.hourly_rate` becomes
   nullable, only for fixed-price projects. The database enforces
   `billing_method = 'fixed' OR hourly_rate IS NOT NULL`. The API, project
   editor and every reader of the rate (time entry views, unbilled estimate,
   billing run) handle null. Fixed projects with no rate show no hourly
   estimate.
8. **Project Costs tab**: on the staff project page. Shows expense lines
   tagged to the project (`expense_lines.project_id`, already exists),
   with totals, against the accepted quote totals (labour and materials
   separately) and the amount invoiced for the project. Read-only.
   - Quoted totals are the sum of every accepted quote linked to the
     project (superseded and declined quotes never count). A project can
     have several accepted quotes for separate scopes.
   - All figures are ex-GST, so quotes, expenses and invoices compare on
     the same basis.
9. **Org profile**: new org settings for business address, phone, email,
   logo (PNG/JPEG, uploaded to `files`), a list of licences (free-form type +
   number, e.g. "QBCC" / "1234567"), and default quote terms. New quotes
   pre-fill terms from the default. Every licence prints on the quote PDF.
10. **Client portal**: a Quotes section on the client Hub and on portal
    project pages, listing quotes that have been sent (sent, accepted,
    declined, expired, superseded, each labelled). Drafts never show. The
    customer can open a quote and download its PDF. Scoping follows portal
    documents: a client contact sees only quotes for their own contact in
    that org.
11. **Permissions**: `quotes_read` and `quotes_write`, granted to roles the
    same way invoice permissions are. Accepting also requires
    `timesheets_admin` because it creates a project and tasks.

## Non-goals

- Converting a quote into an invoice. Invoicing stays manual.
- Customers accepting or declining in the portal.
- Emailing the PDF from the app.
- Showing attachments to customers or embedding them in the PDF.
- Per-org module switching. Modules stay deployment-wide; that needs its own
  design before a second business shares the deployment.
- Deposits, progress claims, price variations.
- Changing what the project's existing invoice or time features do beyond
  handling a null rate.

## Anti-goals

- Quote amounts must never reach invoices, payments, recon, P&L or BAS.
  Quotes live in their own tables, not in `invoices`.
- A T&M project must never end up with no rate.
- Customers must never see drafts, attachments, or another contact's quotes.

## Constraints

- Follow `AGENTS.md` and `docs/ui-standards.md`: reuse existing Svelte,
  Tailwind and DaisyUI components; dedicated routes for the quote editor;
  failed-input preservation, duplicate-submit protection, explicit cancel,
  dirty-edit protection. Update the screen ledger for new routes.
- Money in integer cents; hours as `quantity_milli` like invoice lines.
- Every row carries `org_id`; all queries scoped by org.
- New migrations start at `0027`.
- Tests in the owning layer using the shared YAML harnesses
  (`docs/testing.md`).
- PDF style matches `internal/property/statement_pdf.go` and
  `internal/clients/certificate.go` (Go fonts via `gopdf`).

## Approaches considered

- **A. New `quotes` module (chosen).** Own package, routes, tables
  (`quotes`, `quote_lines`, `quote_attachments`, `quote_counters`) and a
  `quotes` entry in `SHIGOTO_MODULES`, which requires `timesheets`.
  Accept calls into `internal/timesheets` to create the project and tasks.
  The org profile goes in core settings, since invoices will want it later.
  More new code, but quotes stay fully separate from money.
- **B. Quotes as invoices** (`source = 'quote'`). Fewest tables, but every
  invoice, Stripe, recon, P&L and BAS query would need to exclude quotes.
  Rejected: one missed filter puts quoted amounts into the BAS.
- **C. Inside the timesheets module.** Same tables in
  `internal/timesheets`. Avoids module wiring, but makes that package
  bigger and ties quotes to the name "timesheets". Rejected.

## Decided with the user

- Approach A.
- Only labour lines become tasks on accept.
- Accepting creates a project when the quote has none, else adds tasks to
  the linked project. Invoices are sent manually later.
- Project hourly rate becomes nullable (guarded to fixed-price projects).
- Attachments are internal record-keeping only and travel separately.
- Licences are free-form type + number to suit other industries.
- Org profile: address, phone, email, logo, licences, default terms.
- Quotes appear in the client portal, view and download only.
- Staff may accept an expired quote; valid-until exists to push the
  customer, not to block acceptance.
- Revisions, the permission names, valid-until/expired and the Costs tab as
  described above.

## Calls made by Claude (check these)

- Accepted quotes can't be revised; sent, expired and declined ones can.
- The PDF is frozen (stored) when a quote is marked sent.
- Accept onto a completed or cancelled project is blocked.
- Quoted hours aren't copied to tasks.
- Costs tab sums all accepted quotes on the project, ex-GST.

## Left to the builder

- Exact table columns, indexes, and the quote number and revision format
  (keep `Q-0007` and `-R2` unless there's a reason not to).
- Whether `expired` is stored or derived from valid-until at read time.
- How `LineEditor` gains the kind toggle and the hours/rate labels for
  labour lines without breaking invoice editing.
- PDF layout details, and how the logo scales.
- How the Costs tab attributes invoiced amounts (via `invoice_lines.project_id`).
- Which roles receive the new permissions, matching invoice permissions.
- Sandbox seed data for quotes, following `internal/sandbox`.
