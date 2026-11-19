-- Tenant availability windows, staff-booked visits and per-visit entry
-- permission for maintenance tickets. A ticket's entry_preference is only
-- the tenant's stated preference; permission is in force only on a
-- confirmed visit (entry_mode 'permitted', not withdrawn).
ALTER TABLE maintenance_tickets
    ADD COLUMN entry_preference text CHECK (entry_preference IN ('tenant_home', 'permitted'));

CREATE TABLE maintenance_availability (
    id         bigserial PRIMARY KEY,
    org_id     bigint NOT NULL REFERENCES orgs,
    ticket_id  bigint NOT NULL REFERENCES maintenance_tickets,
    date       date NOT NULL,
    slot       text NOT NULL CHECK (slot IN ('morning', 'afternoon', 'custom')),
    start_time time,
    end_time   time,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (slot <> 'custom' OR (start_time IS NOT NULL AND end_time IS NOT NULL AND start_time < end_time))
);
CREATE INDEX maintenance_availability_ticket ON maintenance_availability (ticket_id);

CREATE TABLE maintenance_visits (
    id                       bigserial PRIMARY KEY,
    org_id                   bigint NOT NULL REFERENCES orgs,
    ticket_id                bigint NOT NULL REFERENCES maintenance_tickets,
    supplier_contact_id      bigint NOT NULL REFERENCES contacts,
    starts_at                timestamptz NOT NULL,
    ends_at                  timestamptz NOT NULL CHECK (ends_at > starts_at),
    status                   text NOT NULL DEFAULT 'booked' CHECK (status IN ('booked', 'confirmed', 'change_requested', 'cancelled', 'done')),
    tenant_confirmed_at      timestamptz,
    change_note              text NOT NULL DEFAULT '' CHECK (length(change_note) <= 2000),
    entry_mode               text CHECK (entry_mode IN ('tenant_home', 'permitted')),
    entry_granted_at         timestamptz,
    entry_granted_by_user_id bigint REFERENCES users,
    entry_withdrawn_at       timestamptz,
    created_at               timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX maintenance_visits_one_open ON maintenance_visits (ticket_id)
    WHERE status IN ('booked', 'confirmed', 'change_requested');

ALTER TABLE maintenance_events DROP CONSTRAINT maintenance_events_kind_check;
ALTER TABLE maintenance_events ADD CONSTRAINT maintenance_events_kind_check CHECK (kind IN (
    'created', 'viewed', 'assigned', 'completed', 'cancelled', 'comment', 'note', 'file_added',
    'availability_updated', 'visit_booked', 'visit_confirmed', 'visit_change_requested', 'visit_rescheduled',
    'visit_cancelled', 'visit_done', 'entry_granted', 'entry_withdrawn'));
