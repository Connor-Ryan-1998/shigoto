-- Business profile printed on quotes (and later invoices): contact details,
-- trade licences (free-form type + number, e.g. QBCC), default quote terms
-- and an optional logo stored in files.
ALTER TABLE orgs
    ADD COLUMN address      text NOT NULL DEFAULT '',
    ADD COLUMN phone        text NOT NULL DEFAULT '',
    ADD COLUMN email        text NOT NULL DEFAULT '',
    ADD COLUMN quote_terms  text NOT NULL DEFAULT '',
    ADD COLUMN licences     jsonb NOT NULL DEFAULT '[]',
    ADD COLUMN logo_file_id bigint REFERENCES files;
