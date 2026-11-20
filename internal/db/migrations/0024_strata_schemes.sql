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
