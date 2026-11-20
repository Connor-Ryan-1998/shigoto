# Strata setup — design

Date: 2026-11-20
Status: approved

## Intent

Some of the user's properties are lots in a Queensland body corporate
(community titles scheme). As a lot owner they may hold executive committee
positions. Shigoto should record which scheme a property belongs to and which
positions the user holds, and give each held position a home in a new
**Strata** tab on the property. This round sets up that structure only; the
duties workflows for each role are designed later.

## Goals

- Record strata schemes per organisation; several properties may belong to
  the same scheme.
- Link or unlink a property to a scheme from the property Details page.
- Record which QLD executive positions the user holds in a scheme:
  chairperson, secretary, treasurer. One person may hold all three.
- Show a Strata tab on a property only when it is linked to a scheme, with a
  scheme summary and one sub-section per held position.

## Non-goals (this round)

- Role workflows (minutes, notices, levies, accounts, correspondence).
- Levy tracking, meetings, by-laws, sinking fund.
- A top-level scheme list or scheme detail route.
- Deleting schemes.
- Any portal (tenant/client) exposure.
- Ordinary committee members or body corporate managers.

## Anti-goals

- Positions stored per property — two lots in one scheme must never disagree
  about who holds which role.
- Reusing the word "owner" for strata positions in code; `owner` is the org
  RBAC role (`internal/auth/rbac.go`).
- Forcing the property full-replacement PUT to carry the scheme link.

## Constraints

- Go + pgx hand-written SQL; embedded ordered migrations (`internal/db/migrations`).
  Next migration number is `0024`.
- `module.ReadJSON` rejects unknown fields; new request fields need struct support.
- Feature belongs to the `property` module and its routes; reuse
  `PermPropertyRead` / `PermPropertyWrite`.
- Follow `AGENTS.md` and `docs/ui-standards.md`: reuse existing components,
  dialogs for short editors, failed-input preservation, duplicate-submit
  protection, explicit cancellation, dirty-edit preservation, busy dismissal
  blocked. Update the screen ledger for the new route.
- Organisation isolation: a property may only link to a scheme in the same org.

## Approaches considered

1. **Strata tab on the property (chosen).** Scheme is its own record; property
   holds a nullable FK. Details page toggles membership and edits the scheme's
   positions; Strata tab renders the scheme's role sub-sections. Two linked
   properties show the same scheme content. Trade-off: no single place listing
   all schemes; easy to add later on the same model.
2. **Top-level Strata area.** Scheme list/detail routes in the main nav, role
   sections live on the scheme, property only links. Cleaner with many schemes,
   but larger and moves the role sections away from the property, which the
   user did not want.
3. **Per-property strata flags, no scheme record.** Rejected: properties can
   share a scheme.

Chosen: 1, because it matches the requested UX and the data model leaves room
for option 2 later.

## Design

### Data — migration `0024_strata_schemes.sql`

```sql
CREATE TABLE strata_schemes (
    id              bigserial PRIMARY KEY,      -- match existing id convention
    org_id          bigint NOT NULL REFERENCES orgs(id),
    name            text NOT NULL,
    cts_number      text,                       -- community titles scheme number
    is_chairperson  boolean NOT NULL DEFAULT false,
    is_secretary    boolean NOT NULL DEFAULT false,
    is_treasurer    boolean NOT NULL DEFAULT false,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX ... ON strata_schemes (org_id, cts_number) WHERE cts_number IS NOT NULL;

ALTER TABLE properties
    ADD COLUMN strata_scheme_id bigint REFERENCES strata_schemes(id);
```

Builder matches the existing id type, FK/ON DELETE style and index naming used
in `0004_property.sql` / `0018_property_details.sql`.

Positions are booleans on the scheme and are held by the **organisation** as
lot owner, not by an individual login (user decision: strata is managed from
the org's point of view). There are exactly three fixed positions, so a join
table adds nothing. Any member with `PermPropertyWrite` may change them.

Validation: `name` required and trimmed; `cts_number` trimmed, empty → null,
unique within the org (conflict → field error).

### API (property module, under `/api/v1/orgs/{org}`)

| Method | Path | Permission | Purpose |
|---|---|---|---|
| GET | `/strata-schemes` | property read | List org schemes (for picker), each with linked property ids/names |
| POST | `/strata-schemes` | property write | Create `{name, cts_number, is_chairperson, is_secretary, is_treasurer}` |
| PUT | `/strata-schemes/{id}` | property write | Full update of the same fields |
| PUT | `/properties/{id}/strata` | property write | `{strata_scheme_id: id \| null}` link/unlink |

- Linking checks the scheme belongs to the same org (404 otherwise).
- Separate link endpoint, mirroring `PUT /properties/{id}/notes`, so the
  existing full-replacement property PUT is unchanged.
- `propertyDetailResponse` gains `strata_scheme` (object or null) including
  the scheme fields and the other properties linked to it.
- Writes are audited the same way existing property writes are (`core.Audit`).
- Concurrent edits to a shared scheme are last-write-wins, matching the rest
  of the app (no version column). *Decision made by Claude.*

### Details page — new "Strata" fieldset

In `web/src/routes/[org]/properties/[id]/+page.svelte`:

- Toggle **Part of a strata scheme**.
- When on: scheme select (existing org schemes) plus **New scheme…** opening a
  create dialog (name, CTS number, positions) in `web/src/lib/modules/property/`.
- When a scheme is selected: editable name, CTS number and position
  checkboxes (Chairperson, Secretary, Treasurer), plus a note "Shared with: …"
  listing other linked properties because edits apply to all of them
  (user decision: name and CTS are editable here).
- Toggling off unlinks only; the scheme and its positions are kept.
- The Strata fieldset has its **own Save action**, separate from the property
  Save, because its edits apply to every linked property. It saves the link
  (`PUT /properties/{id}/strata`) and scheme fields (`PUT /strata-schemes/{id}`).
- Editor rules from `docs/ui-standards.md` apply: CTS uniqueness conflict shows
  as a field error with input preserved, duplicate-submit protection, explicit
  cancel, dirty strata edits preserved and tracked by the unsaved-changes guard.

### Strata tab — `web/src/routes/[org]/properties/[id]/strata/`

- `[id]/+layout.svelte` adds a **Strata** entry to `sections` only when the
  loaded property has a linked scheme.
- Visiting `/strata` for an unlinked property shows an empty state linking
  back to Details.
- Content: scheme summary (name, CTS number, shared-with properties), then a
  card per held position — Chairperson, Secretary, Treasurer, in that order —
  each with a placeholder for the role's duties. If no positions are held,
  only the summary plus a hint to pick positions on Details.
- Card components should be separate per role so later workflow work drops
  into each without reshaping the tab.

### Types

`web/src/lib/modules/property/types.ts`: `StrataScheme`, position constants
and labels; `PropertyDetail.strata_scheme`.

## Testing

- Store integration tests: create/update/list scheme, CTS uniqueness per org,
  link/unlink, cross-org link rejected.
- HTTP YAML cases (`internal/httpapi/testdata/cases/property.yaml` or a new
  strata file): permissions, validation, cross-org 404, detail response shape.
- Frontend: Details strata fieldset editor contract (YAML editor harness),
  scheme create dialog contract, layout tab visibility, strata page rendering
  per position combination and unlinked empty state.
- Screen ledger entry in `docs/ui-standards.md` for `/properties/[id]/strata`.
- Optional: sandbox seed (`internal/sandbox/property.go`) with one scheme
  shared by two properties.

## Left to the builder

- Exact SQL conventions, index names, FK delete behaviour (scheme deletion is
  out of scope, so `NO ACTION` is acceptable).
- Placeholder copy in the role cards.
- Whether HTTP cases go in `property.yaml` or a new file.
