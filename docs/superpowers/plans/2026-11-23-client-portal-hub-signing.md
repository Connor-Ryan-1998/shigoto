# Client Portal Hub and Document Signing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give client portal users a hub (welcome notes, checklist, projects, documents) and let them sign staff-uploaded PDFs in the app with a typed name and consent, producing a signing certificate.

**Architecture:** A new always-on Go package `internal/clients` owns welcome notes, checklist items, client documents, signatures and certificate PDFs, plus the staff API. `internal/portal` gains client-only routes that call `clients` with the principal's org and contact. Projects (in `internal/timesheets`) gain client-facing summary/goals/stakeholders. The Svelte SPA gets a staff client page, project-page additions, and portal hub/project/documents/sign routes.

**Tech Stack:** Go stdlib `net/http`, pgx/Postgres embedded migrations, `github.com/signintech/gopdf`, Svelte 5 + Tailwind/DaisyUI, Vitest, Playwright.

**Spec:** `docs/superpowers/specs/2026-11-23-client-portal-hub-signing-design.md`

## Global Constraints

- Read `docs/ui-standards.md` and `docs/testing.md` before UI or test work; add every new route to the ui-standards coverage ledger.
- Portal denials are 404 (never 403). Every portal query filters by principal `OrgID` **and** `ContactID`.
- Only `contacts.kind = 'client'` contacts get welcome notes, checklists and documents; only `client` memberships see them.
- Statuses stay `pending | in_progress | completed | cancelled`; portal labels Pending, In progress, Completed, Cancelled.
- Document `kind`: `signature | shared`. `category`: `general | discovery`. `status`: `draft | sent | signed | withdrawn | superseded`.
- Uploads use `core.DocumentPolicy` but PDF only (`application/pdf`), 10 MiB.
- Consent text (exact): `I agree this is my electronic signature and I am authorised to sign for {client name}.`
- Send blocked message (exact): `Invite this client to the portal first`.
- Signed documents and certificates are immutable: no withdraw, replace, delete.
- Email via `mail.Mailer`; nil mailer skips silently; mail failure never rolls back DB work.
- Certificate timestamps: UTC and `config.Config.Location` (`SHIGOTO_TIMEZONE`).
- Client IP via `auth.ClientIP(r, cfg.TrustedProxy)`.
- Editors: failed-input preservation, duplicate-submit protection, explicit Cancel, dirty-edit preservation, busy-dismissal blocking.
- API calls only through `web/src/lib/api.ts` (`api()` helper).
- Run tasks sequentially: `task test`, `task test SUITE=integration`, `task web:test`, `task lint`, `task web:e2e` (use `go-task` if that is the binary). Never point tests at operator/production databases.

## Review Focus

1. **Racing double sign** (two tabs press Sign): exactly one signature row, second gets 409 `already signed`. → Task 4 test `TestSignConcurrentOnlyOnce`.
2. **Stored blob altered after upload** (hash mismatch at signing): signing refused with 409 `document file has changed`, nothing written. → Task 4 test `TestSignRefusesHashMismatch`.
3. **Client of org A guessing document/project IDs of another contact or org**, including file/certificate streams: 404. → Task 5 YAML cases `portal_client.yaml`.
4. **Project linked to a different contact than the document's contact**: upload rejected 422 `project belongs to another client`. → Task 3 test `TestUploadRejectsOtherClientsProject`.
5. **Typed name blank or whitespace, or consent false**: 422, no signature, editor keeps the typed value. → Task 4 test `TestSignValidation` and Task 7 editor YAML `portal-sign.yaml`.

---

## File map

| Path | Responsibility |
|---|---|
| `internal/db/migrations/0025_client_portal.sql` | All new tables and project columns |
| `internal/clients/welcome.go` | Welcome notes + checklist domain |
| `internal/clients/documents.go` | Document upload/send/withdraw/replace/delete/list |
| `internal/clients/sign.go` | Signing transaction |
| `internal/clients/certificate.go` | Certificate PDF |
| `internal/clients/notify.go` | Emails |
| `internal/clients/routes.go` | Staff HTTP routes |
| `internal/clients/*_integration_test.go` | Domain tests |
| `internal/portal/client.go` | Portal client routes (hub, project, documents, sign) |
| `internal/timesheets/projects.go`, `model.go`, `routes.go` | Project summary/goals/stakeholders |
| `internal/property/files_gc.go` | Register new file references |
| `internal/module/module.go`, `internal/httpapi/server.go` | `Mailer` in `module.Deps`; register `clients` routes |
| `internal/httpapi/casefile/casefile.go` | Seed support for client documents |
| `internal/httpapi/testdata/cases/clients.yaml`, `portal_client.yaml` | HTTP cases |
| `web/src/lib/modules/clients/*` | Types, API wrappers, dialogs, panels |
| `web/src/routes/[org]/contacts/[id]/+page.svelte` | Staff client page |
| `web/src/routes/[org]/projects/[id]/+page.svelte` | Project details + documents |
| `web/src/routes/portal/[org]/hub`, `projects/[id]`, `documents`, `documents/[id]` | Portal screens |
| `web/e2e/client-portal.spec.ts` | End-to-end |

---

### Task 1: Schema, project fields and file references

**Files:**
- Create: `internal/db/migrations/0025_client_portal.sql`
- Modify: `internal/timesheets/model.go` (`Project`), `internal/timesheets/projects.go` (`scanProject`, `CreateProject`, `UpdateProject`, `validateProject`), `internal/timesheets/routes.go` (project JSON in/out)
- Modify: `internal/property/files_gc.go` (`FileReferences`)
- Test: `internal/timesheets/projects_integration_test.go`, `internal/httpapi/testdata/cases/timesheets.yaml`

**Interfaces:**
- Produces: tables `client_welcome`, `client_checklist_items`, `client_documents`, `document_signatures` (schema below); `timesheets.Project` fields `Summary, Goals, Stakeholders string`; JSON `summary`, `goals`, `stakeholders`.

Schema (exact):

```sql
ALTER TABLE projects
    ADD COLUMN summary text NOT NULL DEFAULT '',
    ADD COLUMN goals text NOT NULL DEFAULT '',
    ADD COLUMN stakeholders text NOT NULL DEFAULT '';

CREATE TABLE client_welcome (
    org_id      bigint NOT NULL REFERENCES orgs,
    contact_id  bigint NOT NULL REFERENCES contacts,
    notes       text NOT NULL DEFAULT '',
    updated_at  timestamptz NOT NULL DEFAULT now(),
    updated_by  bigint REFERENCES users,
    PRIMARY KEY (org_id, contact_id)
);

CREATE TABLE client_checklist_items (
    id          bigserial PRIMARY KEY,
    org_id      bigint NOT NULL REFERENCES orgs,
    contact_id  bigint NOT NULL REFERENCES contacts,
    position    int NOT NULL,
    action      text NOT NULL CHECK (length(btrim(action)) > 0),
    owner       text NOT NULL DEFAULT '',
    due_date    date,
    done_at     timestamptz,
    done_by     bigint REFERENCES users,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX client_checklist_contact ON client_checklist_items (org_id, contact_id, position);

CREATE TABLE client_documents (
    id            bigserial PRIMARY KEY,
    org_id        bigint NOT NULL REFERENCES orgs,
    contact_id    bigint NOT NULL REFERENCES contacts,
    project_id    bigint REFERENCES projects,
    title         text NOT NULL CHECK (length(btrim(title)) > 0),
    kind          text NOT NULL CHECK (kind IN ('signature', 'shared')),
    category      text NOT NULL DEFAULT 'general' CHECK (category IN ('general', 'discovery')),
    file_id       bigint NOT NULL REFERENCES files,
    file_sha256   bytea NOT NULL,
    status        text NOT NULL DEFAULT 'draft'
                  CHECK (status IN ('draft', 'sent', 'signed', 'withdrawn', 'superseded')),
    supersedes_id bigint REFERENCES client_documents,
    sent_at       timestamptz,
    created_by    bigint REFERENCES users,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    CHECK (kind = 'signature' OR status <> 'signed')
);
CREATE INDEX client_documents_contact ON client_documents (org_id, contact_id);
CREATE INDEX client_documents_project ON client_documents (project_id);

CREATE TABLE document_signatures (
    id                  bigserial PRIMARY KEY,
    org_id              bigint NOT NULL REFERENCES orgs,
    document_id         bigint NOT NULL UNIQUE REFERENCES client_documents,
    user_id             bigint NOT NULL REFERENCES users,
    typed_name          text NOT NULL,
    email               text NOT NULL,
    consent_text        text NOT NULL,
    signed_at           timestamptz NOT NULL DEFAULT now(),
    ip                  text NOT NULL,
    user_agent          text NOT NULL DEFAULT '',
    file_sha256         bytea NOT NULL,
    certificate_file_id bigint NOT NULL REFERENCES files
);
```

- [ ] **Step 1: Write failing tests.** In `projects_integration_test.go` add `TestProjectClientFieldsRoundTrip`: create a project with `Summary: "Rebuild site"`, `Goals: "Bookings live\nFast mobile"`, `Stakeholders: "Priya Nair — Sponsor"`; `GetProject` returns the same three values; `UpdateProject` with new values persists them. In `timesheets.yaml` add a case: owner PUTs a project with `summary`/`goals`/`stakeholders`, response echoes them. The existing GC test that compares `FileReferences` to schema foreign keys must fail until `files_gc.go` is updated.
- [ ] **Step 2: Run** `task test SUITE=integration PACKAGES=./internal/timesheets/... ./internal/property/... ./internal/httpapi/...` — expect FAIL (unknown columns / missing references).
- [ ] **Step 3: Implement.** Add the migration. Add the three fields through `Project`, scan, create, update, and the route request/response structs (max 4000 chars each; 422 `summary is too long` etc.). Append to `FileReferences`: `{"client_documents", "file_id"}`, `{"document_signatures", "certificate_file_id"}`.
- [ ] **Step 4: Run** the same command — expect PASS.
- [ ] **Step 5: Commit** `feat(clients): schema and client-facing project fields`.

---

### Task 2: Welcome notes and checklist (domain + staff API)

**Files:**
- Create: `internal/clients/welcome.go`, `internal/clients/routes.go`, `internal/clients/welcome_integration_test.go`
- Modify: `internal/httpapi/server.go` (call `clients.RegisterRoutes(mux, md)` next to `portal.RegisterRoutes`)
- Test: `internal/httpapi/testdata/cases/clients.yaml`

**Interfaces:**
- Consumes: Task 1 tables.
- Produces (package `clients`):
  - `var ErrNotClient = errors.New("contact must be a client")`
  - `func RequireClient(ctx context.Context, q core.Querier, orgID, contactID int64) (core.Contact, error)` — `core.ErrNotFound` if missing/other org, `ErrNotClient` if kind ≠ `client`.
  - `type Welcome struct { Notes string; UpdatedAt *time.Time }`
  - `func GetWelcome(ctx, q, orgID, contactID int64) (Welcome, error)` — empty `Welcome` when no row.
  - `func PutWelcome(ctx, q, orgID, contactID, userID int64, notes string) (Welcome, error)` — upsert, max 4000 chars, audits `client_welcome`/`update`.
  - `type ChecklistItem struct { ID int64; Position int; Action, Owner string; DueDate *time.Time; DoneAt *time.Time }`
  - `func ListChecklist(ctx, q, orgID, contactID int64) ([]ChecklistItem, error)` — ordered by `position, id`.
  - `func CreateChecklistItem(ctx, q, orgID, contactID, userID int64, it ChecklistItem) (ChecklistItem, error)` — appends at max position+1; max 20 items per contact → 422 `checklist is full`.
  - `func UpdateChecklistItem(ctx, q, orgID, contactID, userID, id int64, it ChecklistItem) (ChecklistItem, error)`
  - `func SetChecklistDone(ctx, q, orgID, contactID, userID, id int64, done bool) (ChecklistItem, error)` — sets/clears `done_at`, `done_by`.
  - `func DeleteChecklistItem(ctx, q, orgID, contactID, userID, id int64) error`
  - `func RegisterRoutes(mux *http.ServeMux, d module.Deps)`
- Staff routes (`RequireOrg(auth.PermContactsWrite)` for writes, `RequireOrg(auth.PermInvoicesRead)` for reads):
  - `GET|PUT /api/v1/orgs/{org}/contacts/{id}/welcome` — body/response `{"notes": string, "updated_at": string|null}`
  - `GET|POST /api/v1/orgs/{org}/contacts/{id}/checklist`
  - `PUT|DELETE /api/v1/orgs/{org}/contacts/{id}/checklist/{itemId}`
  - `POST /api/v1/orgs/{org}/contacts/{id}/checklist/{itemId}/done` body `{"done": bool}`
  - Item JSON: `{"id","position","action","owner","due_date" (YYYY-MM-DD|null),"done_at" (RFC3339|null)}`
  - Non-client contact → 422 `contact must be a client`; other org/missing → 404.

- [ ] **Step 1: Write failing tests.** `welcome_integration_test.go`: `TestWelcomeUpsert` (get empty, put, get returns notes); `TestWelcomeRejectsNonClient` (tenant contact → `ErrNotClient`); `TestChecklistOrderingAndDone` (create 3, order by position; mark done sets `DoneAt`, undo clears it); `TestChecklistFull` (21st create errors); `TestChecklistCrossContact` (update item id via another contact → `core.ErrNotFound`). `clients.yaml`: owner CRUD happy path; bookkeeper PUT welcome → 403; staff on beta org → 404; tenant contact → 422 `contact must be a client`; client portal user calling staff route → 404.
- [ ] **Step 2: Run** `task test SUITE=integration PACKAGES=./internal/clients/... ./internal/httpapi/...` — expect FAIL.
- [ ] **Step 3: Implement** the functions and routes above in `welcome.go` and `routes.go`; register in `server.go`.
- [ ] **Step 4: Run** the same command — expect PASS.
- [ ] **Step 5: Commit** `feat(clients): staff welcome notes and checklist`.

---

### Task 3: Client documents (domain, staff API, send email)

**Files:**
- Create: `internal/clients/documents.go`, `internal/clients/notify.go`, `internal/clients/documents_integration_test.go`
- Modify: `internal/clients/routes.go`, `internal/module/module.go` (add `Mailer *mail.Mailer` to `Deps`), `internal/httpapi/server.go` (pass `d.Mailer` into `md`), `internal/property/files_gc.go` (export `deleteFileIfUnreferenced` as `DeleteFileIfUnreferenced`), `internal/httpapi/casefile/casefile.go` (seed `client_documents`: `{org, contact, project?, kind, status, title}` with a tiny generated PDF)
- Test: `internal/httpapi/testdata/cases/clients.yaml`

**Interfaces:**
- Consumes: `RequireClient` (Task 2); `core.StageFile`, `core.SaveFile`, `core.GetFile`, `module.HandleUpload`, `module.ServeFile`.
- Produces:
  - `var PDFPolicy = core.UploadPolicy{MaxBytes: 10 << 20, Allowed: map[string]bool{"application/pdf": true}}`
  - `type Document struct { ID, ContactID int64; ProjectID *int64; Title, Kind, Category, Status string; FileID int64; FileSHA256 []byte; SupersedesID *int64; SentAt *time.Time; CreatedAt time.Time; Signature *Signature }`
  - `type Signature struct { TypedName, Email string; SignedAt time.Time; CertificateFileID int64 }`
  - `type NewDocument struct { ContactID int64; ProjectID *int64; Title, Kind, Category string }`
  - `func CreateDocument(ctx, q, orgID, userID int64, nd NewDocument, f core.File) (Document, error)` — status `draft`; copies `f.SHA256` to `file_sha256`; project must belong to org and have `client_contact_id = nd.ContactID` else `ErrProjectOtherClient` (`"project belongs to another client"`).
  - `func GetDocument(ctx, q, orgID, id int64) (Document, error)` (staff, includes `Signature`)
  - `func ListDocuments(ctx, q, orgID, contactID int64, projectID *int64) ([]Document, error)` (staff; all statuses)
  - `func SendDocument(ctx, q, orgID, userID, id int64) (Document, error)` — draft→sent, sets `sent_at`; signature kind requires a client portal login for the contact else `ErrNoPortalLogin` (`"Invite this client to the portal first"`); other states → `ErrBadState` (`"document can't be sent in its current state"`).
  - `func WithdrawDocument(ctx, q, orgID, userID, id int64) (Document, error)` — sent→withdrawn only (`signed` → `ErrSigned` `"signed documents can't be changed"`).
  - `func ReplaceDocument(ctx, q, orgID, userID, id int64, f core.File) (Document, error)` — old must be `sent`; old→`superseded`; new row copies contact/project/title/kind/category, `supersedes_id = old.id`, status `draft`.
  - `func DeleteDraft(ctx, q, store storage.Store, orgID, userID, id int64) error` — only `draft`; then deletes the file if unreferenced (reuse the `property` GC helper by exporting `property.DeleteFileIfUnreferenced`).
  - `func PortalLoginEmail(ctx, q, orgID, contactID int64) (string, bool, error)` — email of the user with a `client` membership on this contact.
  - `func NotifySent(ctx context.Context, m *mail.Mailer, baseURL, orgSlug, to, title string, docID int64) error` — subject `Document to sign: {title}`, link `{baseURL}/portal/{orgSlug}/documents/{docID}`.
  - All state changes audit `client_document`/`{create|send|withdraw|replace|delete}`.
- Staff routes:
  - `GET /api/v1/orgs/{org}/contacts/{id}/documents` (optional `?project=`)
  - `POST /api/v1/orgs/{org}/contacts/{id}/documents` multipart: `file`, `title`, `kind`, `category`, `project_id?` → 201 Document JSON
  - `POST /api/v1/orgs/{org}/documents/{docId}/send|withdraw`; `POST …/replace` (multipart `file`); `DELETE /api/v1/orgs/{org}/documents/{docId}`
  - `GET /api/v1/orgs/{org}/documents/{docId}/file` and `/certificate`
  - Document JSON: `{"id","contact_id","project_id","title","kind","category","status","sent_at","created_at","supersedes_id","file":{FileResponse},"signature":{"typed_name","email","signed_at"}|null}`
  - Errors map: `ErrProjectOtherClient`→422, `ErrNoPortalLogin`→422, `ErrBadState`/`ErrSigned`→409.
  - Send: after commit, if `d.Mailer != nil` and kind is `signature`, call `NotifySent`; log on error.

- [ ] **Step 1: Write failing tests.** `documents_integration_test.go`: `TestCreateDraftCopiesHash`; `TestUploadRejectsOtherClientsProject`; `TestSendRequiresPortalLogin` (no login → `ErrNoPortalLogin`; after `PortalUser` → sent); `TestSharedSendNeedsNoLogin`; `TestWithdrawOnlySent`; `TestReplaceSupersedes` (old `superseded`, new `draft` with `SupersedesID`); `TestDeleteOnlyDraft` (sent → `ErrBadState`; draft deleted and file row gone). `clients.yaml`: upload non-PDF → 415 (existing upload mapping); send emails captured via `testkit.Mailer` fake with expected subject and link; bookkeeper upload → 403.
- [ ] **Step 2: Run** `task test SUITE=integration PACKAGES=./internal/clients/... ./internal/httpapi/...` — expect FAIL.
- [ ] **Step 3: Implement** the above. Add `Mailer` to `module.Deps` and wire it.
- [ ] **Step 4: Run** the same command plus `task test` — expect PASS.
- [ ] **Step 5: Commit** `feat(clients): staff document upload, send, withdraw and replace`.

---

### Task 4: Signing and certificate

**Files:**
- Create: `internal/clients/sign.go`, `internal/clients/certificate.go`, `internal/clients/sign_integration_test.go`, `internal/clients/certificate_test.go`
- Modify: `internal/clients/notify.go`

**Interfaces:**
- Consumes: Task 3 types; `core.OpenFile`, `core.SaveFile`; gopdf pattern from `internal/property/statement_pdf.go` (reuse its font setup).
- Produces:
  - `type SignInput struct { UserID int64; Email, TypedName, IP, UserAgent string; Consent bool }`
  - `func ConsentText(clientName string) string` → `"I agree this is my electronic signature and I am authorised to sign for " + clientName + "."`
  - `func Sign(ctx context.Context, pool *pgxpool.Pool, store storage.Store, loc *time.Location, orgID, contactID, docID int64, in SignInput) (Document, error)`
  - Errors: `ErrNameRequired` (`"type your full name"`), `ErrConsentRequired` (`"tick the consent box to sign"`), `ErrAlreadySigned` (`"already signed"`), `ErrFileChanged` (`"document file has changed"`), `ErrBadState`, `core.ErrNotFound`.
  - `type CertificateData struct { DocumentTitle, FileName string; FileSHA256 []byte; SignerName, SignerEmail, ClientName, OrgName, IP, UserAgent, ConsentText string; SignedAt time.Time; SignatureID int64 }`
  - `func WriteCertificatePDF(w io.Writer, c CertificateData, loc *time.Location) error`
  - `func NotifySigned(ctx context.Context, m *mail.Mailer, to []string, baseURL, orgSlug, clientName, title string) error` — subject `{clientName} signed {title}`.
  - `func SignedRecipients(ctx, q, orgID int64, uploaderID *int64) ([]string, error)` — owners' emails plus uploader, deduped.

Signing transaction (order matters):
1. Trim name; validate name (1–200 chars) and consent before any DB work.
2. `SELECT … FROM client_documents WHERE org_id=$1 AND contact_id=$2 AND id=$3 FOR UPDATE`; missing → `core.ErrNotFound`; kind `shared` → `core.ErrNotFound`; status `signed` → `ErrAlreadySigned`; status ≠ `sent` → `ErrBadState`.
3. Open blob via `core.OpenFile`, stream SHA-256, compare to `file_sha256` → `ErrFileChanged`.
4. Reserve signature id (`nextval`), build `CertificateData`, render PDF to buffer, `core.SaveFile` with name `"{title} - signing certificate.pdf"`.
5. Insert `document_signatures` (unique `document_id` turns a race into `ErrAlreadySigned`), set status `signed`, `updated_at`; audit `client_document`/`sign`.
6. Commit; on any error abort the staged file.

Certificate must include, as text: document title, file name, hex SHA-256, signer name, signer email, client name, org name, signed time in UTC and in `loc` (with zone abbreviation), IP, user agent, consent text, signature id.

- [ ] **Step 1: Write failing tests.** `sign_integration_test.go`: `TestSignHappyPath` (status `signed`, signature row fields, certificate file exists and is PDF); `TestSignValidation` (blank/"   " name → `ErrNameRequired`; consent false → `ErrConsentRequired`; no rows written); `TestSignStates` (draft, withdrawn, superseded → `ErrBadState`; shared → `ErrNotFound`; other contact → `ErrNotFound`); `TestSignConcurrentOnlyOnce` (two goroutines; one success, one `ErrAlreadySigned`; `Count("document_signatures") == 1`); `TestSignRefusesHashMismatch` (overwrite `file_sha256` in DB to a different value → `ErrFileChanged`, status still `sent`); `TestSignedImmutable` (withdraw/replace/delete on signed → `ErrSigned`/`ErrBadState`). `certificate_test.go` (unit): `TestCertificateContainsEvidence` — render with fixed data, extract text (search raw PDF bytes for hex hash and signer name, as `statement_pdf` tests do) and assert presence of hash, name, email, IP, consent text and both timestamps.
- [ ] **Step 2: Run** `task test PACKAGES=./internal/clients/...` and `task test SUITE=integration PACKAGES=./internal/clients/...` — expect FAIL.
- [ ] **Step 3: Implement** `sign.go`, `certificate.go`, `NotifySigned`, `SignedRecipients`.
- [ ] **Step 4: Run** both commands — expect PASS.
- [ ] **Step 5: Commit** `feat(clients): typed-name signing with certificate PDF`.

---

### Task 5: Portal client API

**Files:**
- Create: `internal/portal/client.go`
- Modify: `internal/portal/routes.go` (call `registerClientRoutes(mux, d)`), `internal/portal/portal.go` (add project-invoice query)
- Test: `internal/httpapi/testdata/cases/portal_client.yaml`, `internal/portal/portal_integration_test.go`

**Interfaces:**
- Consumes: `clients.*` (Tasks 2–4), `timesheets.ListProjects/GetProject`, `ListInvoices` visibility rules.
- Produces:
  - Helper `requireClient(w, r, d) (auth.Principal, bool)` — `RequirePortal` principal must have role `client` and a `ContactID`, else 404.
  - `func ListProjectInvoices(ctx, q core.Querier, bill *billing.Service, orgID, contactID, projectID int64) ([]billing.InvoiceResponse, error)` — invoices visible via `ListInvoices` for this contact that have any `invoice_lines.project_id = projectID`.
  - `clients.ListPortalDocuments(ctx, q, orgID, contactID int64, projectID *int64) ([]Document, error)` — excludes `draft`; for a project, also includes general (`project_id IS NULL`) **signed** documents flagged `AppliesToAll bool`.
- Routes (all via `RequirePortal` + `requireClient`):
  - `GET /api/v1/portal/{org}/hub` → `{"welcome":{"notes"}, "checklist":[item…], "projects":[{"id","name","status","start_date","end_date","to_sign"}], "documents":{"to_sign":n,"signed":n,"shared":n}}`. Projects empty when timesheets module is off.
  - `GET /api/v1/portal/{org}/projects/{id}` → `{"id","name","status","summary","goals":[…lines],"stakeholders":[…lines],"start_date","end_date","documents":{"to_sign":[…],"signed":[…],"discovery":[…],"shared":[…],"inactive":[…withdrawn/superseded]},"invoices":[…]}`. 404 unless project's `client_contact_id` = principal contact. Route only registered when timesheets module is enabled.
  - `GET /api/v1/portal/{org}/documents` → `[Document…]` (portal shape: no `created_by`, adds `project_name`, `applies_to_all`)
  - `GET /api/v1/portal/{org}/documents/{id}` → document + `consent_text` (for sent signature docs)
  - `GET /api/v1/portal/{org}/documents/{id}/file` (inline PDF), `/certificate` (signed only)
  - `POST /api/v1/portal/{org}/documents/{id}/sign` body `{"typed_name": string, "consent": bool}` → 200 document; errors: name/consent 422, `ErrAlreadySigned`/`ErrBadState`/`ErrFileChanged` 409, not found 404. After commit, `NotifySigned` (log errors).
  - Change the single-org redirect target in Task 8, not here.

- [ ] **Step 1: Write failing tests.** `portal_client.yaml` seeds acme (client `c1` with login, client `c2` with login, tenant, beta client) plus projects and documents in every status. Cases: hub for c1 shows only c1 projects/counts and checklist; drafts never listed; withdrawn/superseded listed as inactive; c2 GET c1 project/document/file/certificate → 404; tenant GET hub → 404; beta client with acme slug → 404; staff session on portal hub → 404; sign happy path then second sign → 409 `already signed`; sign blank name → 422; sign shared doc → 404; certificate before signing → 404; project invoices include an invoice with a line on the project and exclude draft invoices and other projects; `modules: {timesheets: false}` → hub projects `[]`, project route 404. Integration test `TestPortalSignEmailsStaff` with fake mailer asserts subject `Client One signed MSA`.
- [ ] **Step 2: Run** `task test SUITE=integration PACKAGES=./internal/portal/... ./internal/httpapi/...` — expect FAIL.
- [ ] **Step 3: Implement** `client.go` and the queries.
- [ ] **Step 4: Run** the same command — expect PASS.
- [ ] **Step 5: Commit** `feat(portal): client hub, project, documents and signing API`.

---

### Task 6: Staff UI — client page and project additions

**Files:**
- Create: `web/src/lib/modules/clients/types.ts`, `api.ts` (wrappers over `lib/api.ts`), `WelcomeDialog.svelte`, `ChecklistItemDialog.svelte`, `ChecklistPanel.svelte`, `DocumentUploadDialog.svelte`, `DocumentsTable.svelte` (+ `.test.ts` for each component with logic)
- Create: `web/src/routes/[org]/contacts/[id]/+page.svelte` (+ `+page.svelte.test.ts`) — client contacts only; others show "Not a client" empty state
- Modify: `web/src/routes/[org]/contacts/+page.svelte` (client rows link to the new page), `web/src/routes/[org]/projects/[id]/+page.svelte` and `web/src/lib/modules/timesheets` project dialog (summary/goals/stakeholders fields; Documents section using `DocumentsTable` filtered by project)
- Create: `web/src/lib/test/editors/client-welcome.yaml`, `client-checklist-item.yaml`, `client-document-upload.yaml`; update `project-dialog.yaml`
- Modify: `docs/ui-standards.md` ledger

**Interfaces:**
- Consumes: Task 2/3 staff routes and JSON shapes exactly as listed.
- Produces: `ClientDocument`, `ChecklistItem`, `Welcome` TS types reused by Task 7.

Layout decisions: client page uses the standard detail layout with three sections (Welcome, Checklist, Documents). Welcome and checklist items edit in dialogs. Upload is a dialog (title, kind, category, project select limited to this client's projects, PDF file). Row actions: Send/Share (draft), Withdraw and Replace (sent), Delete (draft), download file/certificate. Send failure `Invite this client to the portal first` shows inline with a link to Settings → Members. Signed rows show signer name and date.

- [ ] **Step 1: Write failing tests.** Editor YAML contracts for the three new dialogs (failed-input preservation, duplicate submit, cancel, dirty dismissal). Component tests: `DocumentsTable` shows only actions valid for each status; signed row has no Withdraw/Replace/Delete; `ChecklistPanel` toggles done via `POST …/done` and reverts on error. Page test: non-client contact renders "Not a client".
- [ ] **Step 2: Run** `task web:test` — expect FAIL.
- [ ] **Step 3: Implement** components, route, project additions; update ledger rows for `[org]/contacts/[id]` and the changed project page.
- [ ] **Step 4: Run** `task web:test` and `task lint` — expect PASS.
- [ ] **Step 5: Commit** `feat(web): staff client page and project client details`.

---

### Task 7: Portal UI — hub, project, documents, signing

**Files:**
- Create: `web/src/lib/modules/clients/portal.ts` (portal API wrappers + types)
- Create: `web/src/routes/portal/[org]/hub/+page.svelte`, `projects/[id]/+page.svelte`, `documents/+page.svelte`, `documents/[id]/+page.svelte` (each with `+page.svelte.test.ts`)
- Modify: `web/src/routes/portal/+page.svelte` (single-org redirect: role `client` → `/portal/{slug}/hub`, else existing `/invoices`), `web/src/routes/portal/+layout.svelte` (client nav: Hub, Documents, Invoices; tenant nav unchanged)
- Create: `web/src/lib/test/editors/portal-sign.yaml`
- Modify: `docs/ui-standards.md` ledger

**Interfaces:**
- Consumes: Task 5 portal routes and shapes.

Screen rules (from spec + mockup, meeting cadence excluded):
- Hub: signature banner when `documents.to_sign > 0` linking to Documents filtered To sign; Welcome notes (plain text, line breaks preserved, no HTML); checklist table (action, owner, due, done tick read-only); project cards with status badge and "✍ n to sign"; cancelled hidden behind "Show cancelled (n)"; Documents preview (latest 5) linking to Documents.
- Project page: summary, goals list, stakeholders list, dates; sections To sign, Signed (general signed shown with "Applies to all projects"), Discovery, Shared, and greyed Withdrawn/Replaced; Invoices table linking to existing invoice detail.
- Documents page: filter tabs All / To sign / Signed / Shared.
- Sign page: inline PDF (`<iframe>` of `/file`), sign panel with full-name input, consent checkbox showing server `consent_text`, Sign (disabled until PDF loaded, name non-blank, consent ticked), Cancel back to previous page. Busy blocks double submit; 422 keeps the typed name and shows the error; 409 `already signed` reloads to signed state. Signed state shows signer, time, and links to PDF and certificate.

- [ ] **Step 1: Write failing tests.** Page tests: hub hides banner at zero, shows cancelled toggle count, renders notes as text (an `<b>` string appears literally); project page lists general signed doc with "Applies to all projects"; withdrawn doc has no Sign link; sign page Sign disabled until name + consent; 422 preserves name; 409 shows signed state. `portal-sign.yaml` editor contract. Portal index test: single client org redirects to `/hub`, single tenant org to `/invoices`.
- [ ] **Step 2: Run** `task web:test` — expect FAIL.
- [ ] **Step 3: Implement** routes, wrappers, nav, redirect; add ledger rows.
- [ ] **Step 4: Run** `task web:test` and `task lint` — expect PASS.
- [ ] **Step 5: Commit** `feat(web): client portal hub, projects, documents and signing`.

---

### Task 8: End-to-end and docs

**Files:**
- Create: `web/e2e/client-portal.spec.ts`
- Modify: `README.md` (new "Client portal" section next to "Tenant portal"), `docs/design.md` (portal row: clients get hub + signing)

- [ ] **Step 1: Write the failing e2e.** Owner creates client contact, invites to portal (existing members flow), writes welcome notes, adds a checklist item, uploads a signature PDF and a shared discovery PDF on a project, sends the signature doc. Client signs in via magic link from Mailpit, lands on Hub, sees banner and project, opens project, signs with typed name + consent, sees signed state and downloads certificate (200, `application/pdf`). Owner sees the row as signed with signer name; Withdraw is absent. A second client cannot open the first client's document URL (404 page).
- [ ] **Step 2: Run** `task web:e2e` — expect FAIL before Tasks 6–7 are complete, PASS after.
- [ ] **Step 3: Update docs.** README Client portal section: what clients see, signing evidence recorded (name, email, time, IP, user agent, SHA-256), certificate is a separate PDF, studio signs before upload, emails need SMTP.
- [ ] **Step 4: Run** `task verify` sequentially — expect PASS. Record any blocked step honestly.
- [ ] **Step 5: Commit** `test(e2e): client portal signing flow; docs`.
