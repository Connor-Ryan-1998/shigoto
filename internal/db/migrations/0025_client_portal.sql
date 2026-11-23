-- Client portal: client-facing project details, a per-client welcome note
-- and kickoff checklist (staff-maintained), and documents staff share with
-- or send for signature to a client contact. A client signs with a typed
-- name and consent; document_signatures holds the evidence and the id of a
-- separate signing-certificate PDF. The uploaded document file is never
-- modified: file_sha256 pins the exact bytes the client was asked to sign.
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
