-- Quotes module: itemised quotes for a client contact (optionally against a
-- project), with labour and material lines, a frozen PDF once sent and
-- internal attachments. Quotes never touch invoices, payments, recon or
-- reports. A revision keeps the quote number and bumps revision; expiry
-- (sent and past valid_until) is derived at read time, never stored.
CREATE TABLE quote_counters (
    org_id      bigint PRIMARY KEY REFERENCES orgs,
    last_number int NOT NULL DEFAULT 0
);

CREATE TABLE quotes (
    id           bigserial PRIMARY KEY,
    org_id       bigint NOT NULL REFERENCES orgs,
    contact_id   bigint NOT NULL REFERENCES contacts,
    project_id   bigint REFERENCES projects,
    number       int NOT NULL,
    revision     int NOT NULL DEFAULT 1 CHECK (revision >= 1),
    parent_id    bigint REFERENCES quotes,
    title        text NOT NULL,
    terms        text NOT NULL DEFAULT '',
    status       text NOT NULL DEFAULT 'draft'
                     CHECK (status IN ('draft', 'sent', 'accepted', 'declined', 'superseded')),
    issue_date   date NOT NULL,
    valid_until  date NOT NULL CHECK (valid_until >= issue_date),
    currency     char(3) NOT NULL DEFAULT 'AUD',
    subtotal     bigint NOT NULL DEFAULT 0,
    gst_total    bigint NOT NULL DEFAULT 0,
    total        bigint NOT NULL DEFAULT 0,
    pdf_file_id  bigint REFERENCES files,
    sent_at      timestamptz,
    decided_at   timestamptz,
    decided_by   bigint REFERENCES users,
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (org_id, number, revision)
);

-- At most one accepted revision, and one open draft, per quote number.
CREATE UNIQUE INDEX quotes_one_accepted ON quotes (org_id, number) WHERE status = 'accepted';
CREATE UNIQUE INDEX quotes_one_draft ON quotes (org_id, number) WHERE status = 'draft';
CREATE INDEX quotes_contact ON quotes (org_id, contact_id);
CREATE INDEX quotes_project ON quotes (org_id, project_id);

CREATE TABLE quote_lines (
    id              bigserial PRIMARY KEY,
    org_id          bigint NOT NULL REFERENCES orgs,
    quote_id        bigint NOT NULL REFERENCES quotes ON DELETE CASCADE,
    position        int NOT NULL,
    kind            text NOT NULL CHECK (kind IN ('labour', 'material')),
    description     text NOT NULL,
    quantity_milli  bigint NOT NULL,
    unit_amount     bigint NOT NULL,
    gst_code        text NOT NULL CHECK (gst_code IN ('GST', 'FRE', 'N-T')),
    gst_amount      bigint NOT NULL,
    amount          bigint NOT NULL
);

CREATE INDEX quote_lines_quote ON quote_lines (quote_id);

CREATE TABLE quote_attachments (
    id          bigserial PRIMARY KEY,
    org_id      bigint NOT NULL REFERENCES orgs,
    quote_id    bigint NOT NULL REFERENCES quotes ON DELETE CASCADE,
    file_id     bigint NOT NULL REFERENCES files,
    created_by  bigint NOT NULL REFERENCES users,
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (quote_id, file_id)
);
