# Strata Setup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a property link to an org-scoped QLD strata scheme whose executive positions the org holds, and show a Strata tab with one sub-section per held position.

**Architecture:** New `strata_schemes` table plus nullable `properties.strata_scheme_id` FK. Store functions and routes live in the property module (`internal/property/strata.go`, `routes_strata.go`). The Details page gains a self-contained `StrataSection` component with its own Save; the property layout shows a Strata tab when the property is linked, and the new `/strata` route renders per-role cards.

**Tech Stack:** Go + pgx (hand-written SQL, embedded migrations), SvelteKit 5 runes + Tailwind/DaisyUI, Vitest, YAML HTTP/editor harnesses.

**Spec:** `docs/superpowers/specs/2026-11-20-strata-setup-design.md`

## Global Constraints

- Positions are org-held booleans `is_chairperson`, `is_secretary`, `is_treasurer` on the scheme; never per user, never per property.
- Do not name strata positions "owner" anywhere in code or copy.
- Permissions: reads use `auth.PermPropertyRead`, writes `auth.PermPropertyWrite`. Routes live in the `property` module so module gating applies automatically.
- `name` required (trimmed); `cts_number` trimmed, empty → NULL, unique per org.
- Cross-org scheme or property → `core.ErrNotFound` (HTTP 404 `{error: not found}`).
- Every write audited with `core.Audit` inside `module.RunInTx`.
- Existing `PUT /properties/{id}` body and behaviour unchanged.
- Shared-scheme edits are last-write-wins (no version column).
- Editors follow `docs/ui-standards.md`: duplicate-submit guard, failed-input preservation, explicit cancel, busy dismissal blocked, dirty edits guarded via `useUnsavedChanges`.
- Run commands sequentially via `uv run --locked task …` from `apps/shigoto`.

## Review Focus

1. Linking a property to another org's scheme id must 404 and change nothing — pinned in Task 1 store test and Task 2 HTTP case.
2. Duplicate CTS number on create *and* update returns 422 with a readable message, including the DB race path — Task 1 test `TestStrataSchemeCTSUnique`.
3. Two CTS-less schemes in one org are allowed (NULL not unique) — same test.
4. Unlinking leaves the scheme and its positions intact and still listed — Task 1 `TestSetPropertyStrataScheme`.
5. After saving strata on Details, the Strata tab appears/disappears without a reload — Task 3 test "linking shows the Strata tab".

---

### Task 1: Schema and store

**Files:**
- Create: `internal/db/migrations/0024_strata_schemes.sql`
- Create: `internal/property/strata.go`
- Create: `internal/property/strata_integration_test.go`
- Modify: `internal/property/model.go:22-44` (add field)
- Modify: `internal/property/store.go:37-48` (`propertyColumns`, `scanProperty`)
- Modify: `internal/testkit/records.go` (builder)

**Interfaces:**
- Produces:
  - `Property.StrataSchemeID *int64`
  - `type StrataScheme struct { ID, OrgID int64; Name, CTSNumber string; IsChairperson, IsSecretary, IsTreasurer bool; Properties []StrataProperty }`
  - `type StrataProperty struct { ID int64; Name string }`
  - `CreateStrataScheme(ctx, q core.Querier, orgID int64, s StrataScheme) (StrataScheme, error)`
  - `UpdateStrataScheme(ctx, q core.Querier, orgID int64, s StrataScheme) (StrataScheme, error)` — full replace of name/cts/positions by `s.ID`
  - `GetStrataScheme(ctx, q core.Querier, orgID, id int64) (StrataScheme, error)` — includes `Properties` ordered by property id
  - `ListStrataSchemes(ctx, q core.Querier, orgID int64) ([]StrataScheme, error)` — ordered by name, id; each with `Properties`
  - `SetPropertyStrataScheme(ctx, q core.Querier, orgID, propertyID int64, schemeID *int64) error`
  - testkit: `func (e *Env) StrataScheme(org core.Org, opts ...StrataSchemeOption) property.StrataScheme`, options `SchemeName(string)`, `CTSNumber(string)`; default name `"Test CTS"`, no CTS, no positions.

- [ ] **Step 1: Write migration `0024_strata_schemes.sql`**

```sql
-- QLD strata (body corporate) schemes an org's properties belong to, and
-- the executive committee positions the org holds in each.
CREATE TABLE strata_schemes (
    id              bigserial PRIMARY KEY,
    org_id          bigint NOT NULL REFERENCES orgs,
    name            text NOT NULL CHECK (name <> ''),
    cts_number      text CHECK (cts_number <> ''),
    is_chairperson  boolean NOT NULL DEFAULT false,
    is_secretary    boolean NOT NULL DEFAULT false,
    is_treasurer    boolean NOT NULL DEFAULT false,
    created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX strata_schemes_org_cts ON strata_schemes (org_id, cts_number) WHERE cts_number IS NOT NULL;

ALTER TABLE properties ADD COLUMN strata_scheme_id bigint REFERENCES strata_schemes;
CREATE INDEX properties_strata_scheme ON properties (strata_scheme_id);
```

- [ ] **Step 2: Write failing integration tests in `strata_integration_test.go`** (`//go:build integration`, package `property_test`)

```go
func TestStrataSchemeRoundTrips(t *testing.T)
// Create {Name:"  Harbour View CTS ", CTSNumber:" 12345 ", IsSecretary:true}
// -> Name "Harbour View CTS", CTSNumber "12345", only IsSecretary true, Properties empty (non-nil).
// Update same ID to {Name:"Harbour View", CTSNumber:"", IsChairperson:true, IsTreasurer:true}
// -> CTSNumber "" (stored NULL), IsSecretary false, IsChairperson+IsTreasurer true.

func TestStrataSchemeRejectsInvalid(t *testing.T)
// Create and Update with Name "   " -> errors.Is(err, core.ErrInvalidInput).
// Update with unknown ID or other org's ID -> core.ErrNotFound.

func TestStrataSchemeCTSUnique(t *testing.T)
// Two schemes with CTSNumber "" in one org: both succeed.
// Second Create with "12345" in same org -> ErrInvalidInput, message contains "cts_number".
// Update of a different scheme to "12345" -> ErrInvalidInput.
// Create "12345" in another org -> succeeds.

func TestSetPropertyStrataScheme(t *testing.T)
// Two properties linked to one scheme: GetStrataScheme(...).Properties == [{p1.ID,p1.Name},{p2.ID,p2.Name}];
// GetProperty(p1).StrataSchemeID == &scheme.ID.
// Unlink p1 (nil): p1.StrataSchemeID nil; scheme still in ListStrataSchemes with positions unchanged and Properties == [p2].

func TestSetPropertyStrataSchemeCrossOrg(t *testing.T)
// acme property + other-org scheme -> ErrNotFound, property unchanged.
// other-org property + acme scheme (called with acme orgID) -> ErrNotFound.
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `uv run --locked task test SUITE=integration PACKAGES=./internal/property/...`
Expected: FAIL — undefined `property.CreateStrataScheme`, `e.StrataScheme`, etc.

- [ ] **Step 4: Implement**

- Add `StrataSchemeID *int64` to `Property`; append `strata_scheme_id` to `propertyColumns` and scan it. `UpdateProperty`/`CreateProperty` SQL untouched.
- `strata.go`: `validateStrataScheme` trims and checks name (`"%w: name is required"`); CTS duplicate is a pre-check `SELECT EXISTS … WHERE org_id=$1 AND cts_number=$2 AND id <> $3` plus `isUniqueViolation` backstop, both returning `fmt.Errorf("%w: cts_number is already used by another scheme in this organisation", core.ErrInvalidInput)`. Store CTS with `NULLIF($n,'')`.
- `SetPropertyStrataScheme`: when `schemeID != nil`, `GetStrataScheme` first (propagates `ErrNotFound`); then `UPDATE properties SET strata_scheme_id=$3 WHERE org_id=$1 AND id=$2`, `RowsAffected()==0` → `ErrNotFound`.
- Load `Properties` with one query per scheme (`SELECT id, name FROM properties WHERE org_id=$1 AND strata_scheme_id=$2 ORDER BY id`); list may batch with `= ANY($2)` — implementer's choice.
- testkit builder mirroring `Env.Property`.

- [ ] **Step 5: Run tests to verify they pass**

Run: `uv run --locked task test SUITE=integration PACKAGES=./internal/property/...`
Expected: PASS (including existing property tests — `propertyColumns` change touches every read).

- [ ] **Step 6: Commit**

```bash
git add internal/db/migrations/0024_strata_schemes.sql internal/property/strata.go internal/property/strata_integration_test.go internal/property/model.go internal/property/store.go internal/testkit/records.go
git commit -m "feat(property): strata schemes and property link"
```

---

### Task 2: HTTP routes

**Files:**
- Create: `internal/property/routes_strata.go`
- Create: `internal/httpapi/testdata/cases/strata.yaml`
- Modify: `internal/property/routes.go:100-104` (call `registerStrataRoutes(mux, d)`), `:215-229` (`propertyDetailResponse.StrataScheme`), `:459-484` (`writePropertyDetail` loads scheme)
- Modify: `internal/httpapi/casefile/casefile.go` (seed kind), `internal/httpapi/cases_integration_test.go` (seeding), `docs/testing.md:64-66` (seed kinds list)

**Interfaces:**
- Consumes: Task 1 store functions and `Property.StrataSchemeID`.
- Produces (JSON, all under `/api/v1/orgs/{org}`):
  - `strataSchemeResponse`: `{id, name, cts_number, is_chairperson, is_secretary, is_treasurer, properties: [{id, name}]}` (`cts_number` is `""` when unset; `properties` never null)
  - `GET /strata-schemes` → `200 [strataSchemeResponse]` (read)
  - `POST /strata-schemes` body `{name, cts_number, is_chairperson, is_secretary, is_treasurer}` → `201 strataSchemeResponse` (write; audit `"strata_scheme", id, "create", body`)
  - `PUT /strata-schemes/{id}` same body → `200 strataSchemeResponse` (write; audit `"update"`)
  - `PUT /properties/{id}/strata` body `{strata_scheme_id: number|null}` → `200 {strata_scheme: strataSchemeResponse|null}` (write; audit `"property", id, "update_strata", body`)
  - `propertyDetailResponse.strata_scheme: strataSchemeResponse|null` on GET/PUT `/properties/{id}`
  - Casefile seed kind `strata_schemes: {name: {org, name, cts}}`, and `SeedProperty.StrataScheme string` (`strata_scheme:` seed-name, same org) applied after both are created. Names join the interpolation/uniqueness groups.

- [ ] **Step 1: Extend casefile** — add `SeedStrataScheme{Org, Name, CTS}`, `Seed.StrataSchemes` (`yaml:"strata_schemes"`), `SeedProperty.StrataScheme`; add `"strata_schemes"` to `seedNames` and the uniqueness groups; validate org, and that a property's `strata_scheme` exists and is in the same org. Add a `casefile_test.go` case: property referencing another org's scheme fails validation. Seed in `cases_integration_test.go` before properties (`e.StrataScheme`), then link with `property.SetPropertyStrataScheme` after creating each property. Update `docs/testing.md` seed kinds.

- [ ] **Step 2: Write `strata.yaml` cases**

```yaml
seed:
  orgs: [acme, other]
  users:
    owner: {org: acme, role: owner}
    keeper: {org: acme, role: bookkeeper}
    other_owner: {org: other, role: owner}
  strata_schemes:
    s_harbour: {org: acme, name: Harbour View CTS, cts: "12345"}
    s_other: {org: other, name: Elsewhere CTS}
  properties:
    p_unit1: {org: acme, name: Unit 1, strata_scheme: s_harbour}
    p_unit2: {org: acme, name: Unit 2}
    p_other: {org: other, name: Far Unit}
```

Cases (names and expectations):
- `owner lists schemes` → 200, body subset `[{name: Harbour View CTS, cts_number: "12345", properties: [{id: "{p_unit1}", name: Unit 1}]}]`.
- `property detail includes its scheme` GET `/properties/{p_unit1}` → `strata_scheme: {id: "{s_harbour}", name: Harbour View CTS}`; GET `{p_unit2}` → `strata_scheme: null`.
- `owner creates a scheme` (isolate) POST `{name: Bayside, cts_number: "", is_treasurer: true}` → 201 `{name: Bayside, cts_number: "", is_treasurer: true, is_chairperson: false, properties: []}`.
- `duplicate CTS is rejected` POST `{name: Dup, cts_number: "12345"}` → 422 `{error: "invalid input: cts_number is already used by another scheme in this organisation"}`, `unchanged: [strata_schemes, audit_log]`.
- `blank name is rejected` POST `{name: "  "}` → 422, unchanged.
- `owner sets positions` (isolate) PUT `/strata-schemes/{s_harbour}` `{name: Harbour View CTS, cts_number: "12345", is_chairperson: true, is_secretary: true, is_treasurer: true}` → 200 with all three true.
- `owner links a property` (isolate) PUT `/properties/{p_unit2}/strata` `{strata_scheme_id: "{s_harbour}"}` → 200 `{strata_scheme: {id: "{s_harbour}", properties: [{name: Unit 1}, {name: Unit 2}]}}`.
- `owner unlinks a property` (isolate) PUT `/properties/{p_unit1}/strata` `{strata_scheme_id: null}` → 200 `{strata_scheme: null}`.
- `cannot link another org's scheme` PUT `/properties/{p_unit2}/strata` `{strata_scheme_id: "{s_other}"}` → 404 `{error: not found}` exact, `unchanged: [properties, audit_log]`.
- `another org cannot edit a scheme` as other_owner PUT `/api/v1/orgs/other/strata-schemes/{s_harbour}` → 404 exact, unchanged `[strata_schemes, audit_log]`.
- `bookkeeper cannot write strata` as keeper: POST `/strata-schemes`, PUT `/strata-schemes/{s_harbour}`, PUT `/properties/{p_unit2}/strata` → 403 `{error: forbidden}` exact, unchanged. (Split into three cases if the harness needs one request per case.)
- `unknown field is rejected` PUT `/properties/{p_unit2}/strata` `{strata_scheme_id: null, extra: 1}` → 400.

- [ ] **Step 3: Run to verify failure**

Run: `uv run --locked task test SUITE=integration PACKAGES=./internal/httpapi/...`
Expected: `strata.yaml` cases FAIL (404 on unregistered routes / missing `strata_scheme`); other files PASS.

- [ ] **Step 4: Implement `routes_strata.go`** — `registerStrataRoutes(mux, d)`, request struct `strataSchemeRequest` (all five fields, plain `string`/`bool`), link request `struct{ StrataSchemeID *int64 \`json:"strata_scheme_id"\` }`, `strataSchemeToResponse(StrataScheme) strataSchemeResponse`. Handlers follow `updateNotesHandler` shape: `module.PathID`, `module.ReadJSON` (400 on error), `RunInTx` + `core.Audit`, `module.WriteCoreError`. Link handler re-reads the scheme after the tx for the response. `writePropertyDetail` calls `GetStrataScheme` when `prop.StrataSchemeID != nil`.

- [ ] **Step 5: Run to verify pass**

Run: `uv run --locked task test SUITE=integration PACKAGES=./internal/httpapi/...` then `uv run --locked task test`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/property/routes_strata.go internal/property/routes.go internal/httpapi docs/testing.md
git commit -m "feat(property): strata scheme API"
```

---

### Task 3: Details page strata section

**Files:**
- Create: `web/src/lib/modules/property/StrataSection.svelte`
- Create: `web/src/lib/modules/property/StrataSchemeDialog.svelte`
- Create: `web/src/lib/modules/property/StrataSection.test.ts`
- Create: `web/src/lib/test/editors/strata-section.yaml`, `web/src/lib/test/editors/strata-scheme-dialog.yaml`
- Modify: `web/src/lib/modules/property/types.ts` (types), `web/src/lib/modules/property/header.svelte.ts` (strata flag)
- Modify: `web/src/routes/[org]/properties/[id]/+page.svelte` (render section after the main grid, before Insurance; set header flag in `accept`)
- Modify: `web/src/lib/test/editors.registry.ts`, `web/src/lib/test/editors/property-details.yaml` (GET body gains `strata_scheme: null`; add `GET /api/v1/orgs/acme/strata-schemes: []` if the section loads on mount)

**Interfaces:**
- Consumes: Task 2 JSON.
- Produces:
  - `types.ts`: `export type StrataPosition = 'chairperson' | 'secretary' | 'treasurer'`; `export const STRATA_POSITIONS: { value: StrataPosition; label: string; field: 'is_chairperson' | 'is_secretary' | 'is_treasurer' }[]` in order Chairperson, Secretary, Treasurer; `export type StrataScheme = { id: number; name: string; cts_number: string; is_chairperson: boolean; is_secretary: boolean; is_treasurer: boolean; properties: { id: number; name: string }[] }`; `PropertyDetail.strata_scheme: StrataScheme | null`.
  - `propertyHeader` becomes `$state({ key: '', name: '', strata: false })`.
  - `StrataSection` props `{ org: string; property: PropertyDetail; onsaved: (scheme: StrataScheme | null) => void }`.
  - `StrataSchemeDialog` props `{ open?: boolean (bindable); org: string; oncreated: (s: StrataScheme) => void }` — POSTs `/strata-schemes`.

Behaviour of `StrataSection` (its own `<form>`, outside the details form, `aria-labelledby` heading "Strata"):
- Checkbox **Part of a strata scheme**; when on, `<select>` **Scheme** listing `GET /strata-schemes` (loaded when the section mounts and canWrite) plus a **New scheme…** button opening `StrataSchemeDialog`; on create, the new scheme is added to the list and selected.
- With a scheme selected: inputs **Scheme name**, **CTS number**, checkboxes **Chairperson**, **Secretary**, **Treasurer**, and "Shared with: …" listing the scheme's other properties (omit when none).
- Save **Save strata**: if on, `PUT /strata-schemes/{id}` (when scheme fields changed) then `PUT /properties/{id}/strata` (when link changed); if off, only the link PUT with `null`. Validation before sending: on with no scheme → "Choose or create a scheme." On success call `onsaved(scheme|null)`, snapshot new values, status "Strata saved.". On error show message, keep inputs, re-enable. Retry resends both PUTs (idempotent).
- Cancel restores the snapshot (`allowLeave` guarded). `useUnsavedChanges` with own dirty/busy. Read-only users see values with controls disabled and no actions.
- Details page `onsaved` sets `property.strata_scheme` and `propertyHeader.strata = scheme !== null`; `accept()` also sets `propertyHeader.strata`.

- [ ] **Step 1: Write failing tests**

`strata-section.yaml` (editor contract): `component: StrataSection`, props `{org: acme, property: <PropertyDetail id 1 with strata_scheme {id: 3, name: Harbour View CTS, cts_number: "12345", is_chairperson: false, is_secretary: true, is_treasurer: false, properties: [{id: 1, name: Cottage}]}>}`, `callbacks: [onsaved]`, `page: {org: acme, id: "1"}`, api `GET /api/v1/orgs/acme/strata-schemes: [<same scheme>]`, `fill: {Scheme name: Harbour View, Treasurer: "true"}`, `submit: Save strata`, `request: PUT /api/v1/orgs/acme/strata-schemes/3`, contract `blocks_duplicate_submit: true`, `preserves_input_on: 422`, `busy_dismiss_blocked: Cancel`, `dirty_navigation_blocked: true`; skip `cancel` and `dirty_dismiss_confirms` with the same reasons as `property-details.yaml`.

`strata-scheme-dialog.yaml`: modelled on `insurance-dialog.yaml`; `fill: {Scheme name: Bayside, Secretary: "true"}`, `submit: Create scheme`, `request: POST /api/v1/orgs/acme/strata-schemes`, full dialog contract.

`StrataSection.test.ts`:
- `it('links an unlinked property to an existing scheme')` — property `strata_scheme: null`; tick **Part of a strata scheme**, select scheme 3, Save → only `PUT /api/v1/orgs/acme/properties/1/strata` with `{strata_scheme_id: 3}`; `onsaved` called with the returned scheme.
- `it('unlinks without touching the scheme')` — untick, Save → only the link PUT with `{strata_scheme_id: null}`; no scheme PUT.
- `it('requires a scheme when enabled')` — tick, no selection, Save → alert "Choose or create a scheme."; no request.
- `it('shows other properties sharing the scheme')` — scheme properties `[{1,Cottage},{2,Unit 2}]` → text "Shared with: Unit 2".
- `it('creates a scheme from the dialog and selects it')` — New scheme… → fill dialog → POST → select value is the new id.
- `it('is read-only without property_write')` — `signIn({perms: ['property_read']})` → no **Save strata** button, checkbox disabled.

- [ ] **Step 2: Run to verify failure**

Run: `(cd web && npx vitest run src/lib/modules/property/StrataSection.test.ts src/lib/test)`
Expected: FAIL — components and registry entries missing.

- [ ] **Step 3: Implement** — types, header flag, both components (mirror `InsuranceDialog` for the dialog, the Details page's `seq`/`scope` stale-response guards for the section), registry entries `StrataSection`, `StrataSchemeDialog`, wire into Details page, update `property-details.yaml` fixtures.

- [ ] **Step 4: Run to verify pass**

Run: `uv run --locked task web:test`
Expected: type-check clean, all Vitest PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/modules/property web/src/lib/test web/src/routes/[org]/properties/[id]/+page.svelte
git commit -m "feat(web): strata section on property details"
```

---

### Task 4: Strata tab and role sub-sections

**Files:**
- Create: `web/src/routes/[org]/properties/[id]/strata/+page.svelte`, `.../strata/+page.svelte.test.ts`
- Create: `web/src/lib/modules/property/strata/ChairpersonCard.svelte`, `SecretaryCard.svelte`, `TreasurerCard.svelte`
- Modify: `web/src/routes/[org]/properties/[id]/+layout.svelte:16-36`, `+layout.svelte.test.ts`
- Modify: `docs/ui-standards.md:214-215` (ledger: layout tabs list; new strata row; details row mentions strata section + editors)

**Interfaces:**
- Consumes: `PropertyDetail.strata_scheme`, `propertyHeader.strata`, `STRATA_POSITIONS`.
- Produces: role card components with props `{ org: string; scheme: StrataScheme }` (no behaviour yet; future workflow work fills them).

Behaviour:
- Layout: its existing GET also reads `strata_scheme` and sets `propertyHeader.strata`; `sections` inserts `{ href: \`${base}/strata\`, label: 'Strata' }` after Details when `propertyHeader.key === key && propertyHeader.strata`.
- Strata page: loads `GET /properties/{id}` with the same `seq`/`scope`/`canRead` guards as `notes/+page.svelte`. Unlinked → "This property is not part of a strata scheme." with link "Set up strata on Details" to `/{org}/properties/{id}`. Linked → summary card (scheme name heading, CTS number or "No CTS number", "Shared with: …" when other properties exist), then cards in order Chairperson, Secretary, Treasurer for held positions only. Each card: `<section aria-labelledby>` with heading = role label and placeholder "Duties for this role will appear here.". None held → hint "You don't hold any committee positions in this scheme. Choose positions on Details." linking to Details.

- [ ] **Step 1: Write failing tests**

Layout test additions:
- `it('shows the Strata tab only for a linked property')` — mock property with `strata_scheme: {…}` → nav links `Details, Strata, Bills, Depreciation, Notes, Maintenance`; with `strata_scheme: null` → existing list unchanged (update the existing test's mock to include `strata_scheme: null`).
- `it('linking shows the Strata tab')` — render unlinked, then set `propertyHeader.strata = true` → Strata link appears.

Strata page tests:
- `it('renders a card per held position in order')` — secretary + treasurer held → headings `['Secretary', 'Treasurer']`, no Chairperson.
- `it('renders all three when every position is held')`.
- `it('prompts for positions when none are held')`.
- `it('shows an empty state for an unlinked property')` — link href `/acme/properties/1`.
- `it('ignores a stale response after switching property')` — `deferred()` for property 1, switch to 2, resolve 1 → shows 2.
- `it('denies without property_read')`.

- [ ] **Step 2: Run to verify failure**

Run: `(cd web && npx vitest run "src/routes/[org]/properties/[id]")`
Expected: FAIL — strata route missing, tab absent.

- [ ] **Step 3: Implement** layout change, page, three cards, ledger rows.

- [ ] **Step 4: Run full verification**

Run, sequentially: `uv run --locked task web:test`, `uv run --locked task test`, `uv run --locked task test SUITE=integration`, `uv run --locked task lint`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/routes/[org]/properties/[id] web/src/lib/modules/property/strata docs/ui-standards.md
git commit -m "feat(web): strata tab with role sections"
```

---

### Task 5: Sandbox examples

Added at the user's request after plan approval.

**Files:**
- Modify: `internal/sandbox/property.go` (seed schemes after properties, main org only)
- Modify: `internal/sandbox/reset.go:66-72` (delete `strata_schemes` after `properties`)
- Modify: `internal/sandbox/scenarios_integration_test.go` (assertions)

**Interfaces:**
- Consumes: Task 1 `CreateStrataScheme`, `SetPropertyStrataScheme`.

Examples (main sandbox org only):
- "Garden Terrace Body Corporate", CTS `"54321"`, all three positions held, linked to **Garden Terrace** and **Vacant Apartment** (shared scheme; one lot owner holding every role).
- "Market Studio Owners", no CTS, only Secretary held, linked to **Market Studio**.
- River Cottage and Previous Home stay unlinked.

- [ ] **Step 1: Failing test** — in `TestPopulateScenarios` add counts (main org): `"strata schemes"` `SELECT count(*) FROM strata_schemes WHERE org_id=$1` ≥ 1; `"shared strata scheme"` `SELECT count(*) FROM (SELECT strata_scheme_id FROM properties WHERE org_id=$1 AND strata_scheme_id IS NOT NULL GROUP BY 1 HAVING count(*) > 1) s` ≥ 1; `"all strata positions"` `SELECT count(*) FROM strata_schemes WHERE org_id=$1 AND is_chairperson AND is_secretary AND is_treasurer` ≥ 1.
- [ ] **Step 2: Run** `uv run --locked task test SUITE=integration PACKAGES=./internal/sandbox/...` — Expected: the three new subtests FAIL.
- [ ] **Step 3: Implement** seeding and reset deletion.
- [ ] **Step 4: Run** same command — Expected: PASS, including reset/repopulate tests (`TestPopulateReplacesTesterEdits`).
- [ ] **Step 5: Commit** `feat(sandbox): strata scheme examples`

---

## Not in this plan

Role workflows, levies, meetings, scheme list route, scheme deletion, portal exposure.
