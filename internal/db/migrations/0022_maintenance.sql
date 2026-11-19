-- Maintenance (repair) tickets raised by tenants through the portal or by
-- staff, with attached media and an append-only event timeline that tenants
-- see (tenant_visible) alongside staff-only notes.
ALTER TABLE orgs
    ADD COLUMN emergency_phone text NOT NULL DEFAULT '',
    ADD COLUMN maintenance_seq integer NOT NULL DEFAULT 0;

CREATE TABLE maintenance_tickets (
    id                   bigserial PRIMARY KEY,
    org_id               bigint NOT NULL REFERENCES orgs,
    ref_no               integer NOT NULL,
    property_id          bigint NOT NULL REFERENCES properties,
    lease_id             bigint REFERENCES leases,
    raised_by_contact_id bigint REFERENCES contacts,
    raised_by_user_id    bigint REFERENCES users,
    title                text NOT NULL CHECK (length(title) BETWEEN 1 AND 120),
    description          text NOT NULL CHECK (length(description) BETWEEN 1 AND 4000),
    category             text NOT NULL CHECK (category IN ('plumbing', 'electrical', 'appliance', 'heating_cooling',
                             'doors_windows_locks', 'pests', 'garden_exterior', 'other')),
    urgency              text NOT NULL CHECK (urgency IN ('routine', 'soon', 'urgent')),
    status               text NOT NULL DEFAULT 'new' CHECK (status IN ('new', 'viewed', 'assigned', 'scheduled', 'completed', 'cancelled')),
    supplier_contact_id  bigint REFERENCES contacts,
    access_notes         text NOT NULL DEFAULT '' CHECK (length(access_notes) <= 1000),
    viewed_at            timestamptz,
    completed_at         timestamptz,
    cancelled_at         timestamptz,
    cancel_reason        text NOT NULL DEFAULT '',
    created_at           timestamptz NOT NULL DEFAULT now(),
    UNIQUE (org_id, ref_no)
);
CREATE INDEX maintenance_tickets_property ON maintenance_tickets (org_id, property_id, status);
CREATE INDEX maintenance_tickets_lease ON maintenance_tickets (org_id, lease_id);

CREATE TABLE maintenance_ticket_files (
    ticket_id           bigint NOT NULL REFERENCES maintenance_tickets,
    file_id             bigint NOT NULL REFERENCES files,
    uploaded_by_user_id bigint REFERENCES users,
    created_at          timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (ticket_id, file_id)
);

CREATE TABLE maintenance_events (
    id             bigserial PRIMARY KEY,
    org_id         bigint NOT NULL REFERENCES orgs,
    ticket_id      bigint NOT NULL REFERENCES maintenance_tickets,
    kind           text NOT NULL CHECK (kind IN ('created', 'viewed', 'assigned', 'completed', 'cancelled', 'comment', 'note', 'file_added')),
    actor_user_id  bigint REFERENCES users,
    body           text NOT NULL DEFAULT '' CHECK (length(body) <= 4000),
    data           jsonb NOT NULL DEFAULT '{}',
    tenant_visible boolean NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX maintenance_events_ticket ON maintenance_events (ticket_id, created_at);

-- Media uploaded from the portal before its ticket exists. Submitting the
-- ticket moves these to maintenance_ticket_files; the scheduler deletes
-- rows (and unreferenced files) older than 24 hours.
CREATE TABLE maintenance_pending_files (
    id         bigserial PRIMARY KEY,
    org_id     bigint NOT NULL REFERENCES orgs,
    file_id    bigint NOT NULL REFERENCES files,
    user_id    bigint NOT NULL REFERENCES users,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX maintenance_pending_files_user ON maintenance_pending_files (org_id, user_id);
