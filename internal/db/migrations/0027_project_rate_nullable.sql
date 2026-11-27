-- A fixed-price project (e.g. one created by accepting a quote) may have no
-- hourly rate: its fee is invoiced manually and the billing run skips it.
-- Time-and-materials projects still need one, because the billing run
-- prices their time with it.
ALTER TABLE projects ALTER COLUMN hourly_rate DROP NOT NULL;
ALTER TABLE projects ADD CONSTRAINT projects_rate_required_for_tm
    CHECK (billing_method = 'fixed' OR hourly_rate IS NOT NULL);
