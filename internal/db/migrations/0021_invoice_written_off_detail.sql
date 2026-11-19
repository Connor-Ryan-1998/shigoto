-- written_off_on/written_off_amount record when an invoice was written off
-- and how much was outstanding at that moment, so rent statements can show
-- the write-off as a dated credit. Existing write-offs are backfilled from
-- the latest mark_uncollectible audit entry, in the Brisbane calendar
-- (Shigoto's default timezone), with the amount outstanding today.
ALTER TABLE invoices
    ADD COLUMN written_off_on date,
    ADD COLUMN written_off_amount bigint NOT NULL DEFAULT 0 CHECK (written_off_amount >= 0);

UPDATE invoices i
SET written_off_on = (a.at AT TIME ZONE 'Australia/Brisbane')::date,
    written_off_amount = greatest(i.total - coalesce((SELECT sum(p.amount) FROM payments p WHERE p.org_id = i.org_id AND p.invoice_id = i.id), 0), 0)
FROM (
    SELECT DISTINCT ON (org_id, entity_id) org_id, entity_id, at
    FROM audit_log
    WHERE entity = 'invoice' AND action = 'mark_uncollectible'
    ORDER BY org_id, entity_id, at DESC
) a
WHERE a.org_id = i.org_id AND a.entity_id = i.id AND i.written_off AND i.written_off_on IS NULL;
