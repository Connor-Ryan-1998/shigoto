-- Strata scheme documents (StrataMax minutes, by-laws, levy notices,
-- budgets, financial statements) and what the strata assistant derives
-- from them: per-page text, searchable chunks with embeddings, and the
-- motions and financial figures it extracts. Every derived row carries
-- org_id and scheme_id so each query filters both.
CREATE TABLE strata_documents (
    id              bigserial PRIMARY KEY,
    org_id          bigint NOT NULL REFERENCES orgs,
    scheme_id       bigint NOT NULL REFERENCES strata_schemes,
    file_id         bigint NOT NULL REFERENCES files,
    doc_type        text NOT NULL CHECK (doc_type IN ('minutes', 'by_laws', 'cms', 'levy_notice', 'budget',
                                                      'financial_statement', 'sinking_fund_forecast', 'other')),
    title           text NOT NULL CHECK (title <> '' AND length(title) <= 200),
    document_date   date NOT NULL,
    status          text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'extracting', 'indexed', 'failed')),
    error           text NOT NULL DEFAULT '',
    attempts        int NOT NULL DEFAULT 0,
    next_attempt_at timestamptz,
    page_count      int NOT NULL CHECK (page_count > 0),
    extract_model   text NOT NULL DEFAULT '',
    extracted_at    timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (scheme_id, file_id)
);
CREATE INDEX strata_documents_scheme ON strata_documents (org_id, scheme_id, document_date);
CREATE INDEX strata_documents_queue ON strata_documents (status, created_at) WHERE status IN ('pending', 'failed');

CREATE TABLE strata_document_pages (
    document_id bigint NOT NULL REFERENCES strata_documents ON DELETE CASCADE,
    page_no     int NOT NULL CHECK (page_no > 0),
    text        text NOT NULL,
    PRIMARY KEY (document_id, page_no)
);

CREATE TABLE strata_chunks (
    id          bigserial PRIMARY KEY,
    org_id      bigint NOT NULL REFERENCES orgs,
    scheme_id   bigint NOT NULL REFERENCES strata_schemes,
    document_id bigint NOT NULL REFERENCES strata_documents ON DELETE CASCADE,
    page_from   int NOT NULL CHECK (page_from > 0),
    page_to     int NOT NULL CHECK (page_to >= page_from),
    text        text NOT NULL,
    tsv         tsvector GENERATED ALWAYS AS (to_tsvector('english', text)) STORED,
    embedding   public.vector(1536),
    embed_model text,
    CHECK ((embedding IS NULL) = (embed_model IS NULL))
);
CREATE INDEX strata_chunks_scheme ON strata_chunks (org_id, scheme_id);
CREATE INDEX strata_chunks_document ON strata_chunks (document_id);
CREATE INDEX strata_chunks_tsv ON strata_chunks USING gin (tsv);
CREATE INDEX strata_chunks_embedding ON strata_chunks USING hnsw (embedding public.vector_cosine_ops);

CREATE TABLE strata_motions (
    id           bigserial PRIMARY KEY,
    org_id       bigint NOT NULL REFERENCES orgs,
    scheme_id    bigint NOT NULL REFERENCES strata_schemes,
    document_id  bigint NOT NULL REFERENCES strata_documents ON DELETE CASCADE,
    meeting_date date NOT NULL,
    meeting_kind text NOT NULL CHECK (meeting_kind IN ('agm', 'egm', 'committee', 'other')),
    motion_no    text NOT NULL DEFAULT '',
    text         text NOT NULL CHECK (text <> ''),
    outcome      text NOT NULL CHECK (outcome IN ('carried', 'defeated', 'withdrawn', 'not_put', 'unknown')),
    page_no      int NOT NULL CHECK (page_no > 0),
    tsv          tsvector GENERATED ALWAYS AS (to_tsvector('english', motion_no || ' ' || text)) STORED
);
CREATE INDEX strata_motions_scheme ON strata_motions (org_id, scheme_id, meeting_date);
CREATE INDEX strata_motions_document ON strata_motions (document_id);
CREATE INDEX strata_motions_tsv ON strata_motions USING gin (tsv);

CREATE TABLE strata_figures (
    id           bigserial PRIMARY KEY,
    org_id       bigint NOT NULL REFERENCES orgs,
    scheme_id    bigint NOT NULL REFERENCES strata_schemes,
    document_id  bigint NOT NULL REFERENCES strata_documents ON DELETE CASCADE,
    fund         text NOT NULL CHECK (fund IN ('admin', 'sinking', 'other')),
    metric       text NOT NULL CHECK (metric IN ('levy', 'budget_income', 'budget_expense', 'actual_income',
                                                 'actual_expense', 'balance', 'arrears', 'other')),
    label        text NOT NULL CHECK (label <> ''),
    period_start date,
    period_end   date NOT NULL,
    amount_cents bigint NOT NULL,
    page_no      int NOT NULL CHECK (page_no > 0)
);
CREATE INDEX strata_figures_scheme ON strata_figures (org_id, scheme_id, fund, metric, period_end);
CREATE INDEX strata_figures_document ON strata_figures (document_id);
