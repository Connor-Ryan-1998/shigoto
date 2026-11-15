-- External references for records brought in by CSV import (ERPNext
-- history). NULL for records created in Shigoto; unique per org so a
-- re-import can recognise what it already wrote.
ALTER TABLE invoices ADD COLUMN external_ref text;
ALTER TABLE invoices ADD CONSTRAINT invoices_org_external_ref_key UNIQUE (org_id, external_ref);
ALTER TABLE time_entries ADD COLUMN external_ref text;
ALTER TABLE time_entries ADD CONSTRAINT time_entries_org_external_ref_key UNIQUE (org_id, external_ref);
