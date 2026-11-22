# Strata assistant (chairperson + treasurer) — design

Date: 2026-11-21
Status: draft (awaiting approval)

User decisions this round: chairperson + treasurer only; answers not saved;
StrataMax documents by manual upload; external models accepted; hybrid
structured + vector retrieval; Shigoto-maintained Alpine+pgvector image;
Gemini embeddings, DeepSeek V4.1 Flash for extraction and agent; config via
homelab vault + redeploy; PDF `file` mode now, `image` mode reserved; no
spending cap.
Builds on: `2026-11-20-strata-setup-design.md`

## Intent

The org holds executive positions in QLD body corporate schemes. The body
corporate manager (StrataMax) issues minutes, by-laws, levy notices, budgets
and financial statements as PDFs. Today the answers to committee questions are
buried in those PDFs. The user wants a per-role assistant on the property's
Strata tab that answers questions **grounded in the scheme's own documents,
with page citations**:

- **Chairperson** — "what did the committee decide about the roof in 2024?"
  across minutes; drafting notices grounded in the by-laws.
- **Treasurer** — levy and financial questions answered from StrataMax
  financial documents ("sinking fund balance at end of FY25", "how did admin
  levies change from last year's budget").

Architecture is agentic hybrid RAG: a document store (SeaweedFS + Postgres),
a light structured knowledge layer extracted from documents (motions,
financial figures) linked to the existing scheme/property graph, and a
tool-calling agent per role.

## Goals

- Upload scheme PDFs to a strata scheme, tagged with type and date.
- Background indexing: a model reads each PDF once and produces page text,
  plus motions (minutes) or financial figures (financial documents); pages
  are chunked, full-text indexed and embedded (pgvector).
- A per-role "Ask" panel (chairperson, treasurer) on the Strata tab that runs
  a bounded tool-calling agent and returns an answer with validated citations
  to document pages.
- A provider-agnostic model layer: any OpenAI-compatible endpoint
  (OpenRouter, Google's OpenAI-compatible endpoint, Cloudflare Workers AI,
  vLLM, Ollama) selected by configuration only.
- Defaults: `deepseek/deepseek-v4.1-flash` for extraction and agent;
  `gemini-embedding-2` for embeddings.

## Non-goals (this round)

- Secretary assistant (card unchanged).
- Saving answers, drafts or conversation history. Each question stands alone;
  the user copies what they need.
- Multi-turn chat / follow-up questions.
- Tabular levy/fund records owned by Shigoto (possible later as an extra
  treasurer tool).
- StrataMax integration or scraping. Documents arrive by manual upload.
- Live model hot-swap without restart. A model change is a config change plus
  redeploy.
- Portal (tenant/client) exposure of any strata document or assistant.
- General entity/relationship extraction (full GraphRAG).

## Anti-goals

- An answer that states a figure or decision without a citation the server
  verified came from this request's tool results.
- Retrieval crossing scheme or org boundaries. Scope comes from the server,
  never from model-supplied arguments.
- Assistant tools that write, delete or send anything. Tools are read-only.
- Mixing vectors from different embedding models in one search.
- Money held as floats. Figures are integer cents (`docs/design.md`).
- Vendor lock-in in code: no provider-specific SDKs; one OpenAI-compatible
  HTTP client.

## Constraints

- Go + pgx hand-written SQL; embedded ordered migrations. Next migration is
  `0025`.
- Runtime stays one Go binary + Postgres + SeaweedFS. Background work runs
  in-process (as `internal/scheduler` does), guarded by a Postgres advisory
  lock.
- Postgres image stays Alpine and digest-pinned; pgvector is added by a
  Shigoto-maintained image (below). Switching to a Debian base is rejected:
  musl → glibc collation changes can silently corrupt text indexes on the
  existing data directory.
- Strata belongs to the `property` module; reuse `PermPropertyRead` /
  `PermPropertyWrite`, org isolation, `core.Audit`, `module.HandleUpload`,
  `storage.Store`.
- Follow `AGENTS.md`, `docs/ui-standards.md` and `docs/testing.md` (YAML HTTP
  cases for permissions/isolation; testkit integration tests for workflows;
  editor contract for the upload dialog; screen ledger updated).
- The user accepts sending strata PDFs to external model providers. Provider
  terms (logging/training on free tiers) are the operator's choice and are
  documented, not enforced.
- Config is environment variables (`internal/config`), rendered by the
  homelab `ansible/roles/shigoto/templates/app.env.j2`; API keys live in the
  homelab vault. Shigoto and homelab-infra are separate git repos; this work
  touches both.

## Approaches considered

1. **Vector RAG + agent.** Chunk, embed, semantic search, read pages. Strong
   on paraphrase and prose; weak on exact figures, totals and date filters.
2. **Structured extraction + Postgres full-text + agent.** Motions and
   figures as rows; exact and filterable; weaker on prose and paraphrase.
3. **Full GraphRAG.** General entity/relation graph + vectors. Most powerful
   cross-document reasoning; most expensive and hardest to test; oversized for
   a handful of schemes.

**Chosen: 1 + 2 combined.** They are complementary: structured tools answer
exact/filtered questions, hybrid (full-text + vector) search answers open
recall. The user accepted the pgvector image migration for robustness. Graph
extraction (3) can be layered later on the same documents.

## Design

### 1. Postgres image with pgvector

- New `deploy/postgres/Dockerfile`: `FROM postgres:18.6-alpine@<current
  digest>`, build pgvector from a pinned release tag in a builder stage, copy
  the extension files into the final stage (no build toolchain left behind).
- CI (`.github/workflows/validate.yml`) builds and pushes
  `ghcr.io/connor-ryan-1998/shigoto-postgres` tagged
  `18.6-pgvector<version>` and `sha-<commit>` on main.
- Local `docker-compose.yml` `db` builds from that Dockerfile.
- Homelab role: new `shigoto_postgres_image` default; `shigoto_postgres_version`
  becomes that image's tag + digest (still digest-pinned, as `AGENTS.md`
  requires). Compose template uses `{{ shigoto_postgres_image }}:{{ shigoto_postgres_version }}`.
- Migration `0025` runs `CREATE EXTENSION IF NOT EXISTS vector`
  (`POSTGRES_USER shigoto` is superuser). Startup on an image without
  pgvector fails the migration with a clear error.
- Same major version and same libc, so the existing data directory is reused
  in place. Cutover: take the paired cold backup, deploy, verify. Restore
  docs note the new image is required.

### 2. Configuration (`internal/config`)

All optional. The assistant is enabled only when the LLM group is complete;
partial configuration is a startup error. Embeddings are optional on top:
without them search is full-text only.

| Variable | Meaning |
|---|---|
| `SHIGOTO_LLM_BASE_URL` | OpenAI-compatible base URL (e.g. `https://openrouter.ai/api/v1`) |
| `SHIGOTO_LLM_API_KEY` | secret (vault) |
| `SHIGOTO_LLM_MODEL` | agent model, default suggestion `deepseek/deepseek-v4.1-flash` |
| `SHIGOTO_LLM_EXTRACT_MODEL` | extraction model; defaults to `SHIGOTO_LLM_MODEL` |
| `SHIGOTO_LLM_PDF_MODE` | how page batches reach the extract model: `file` (default; PDF `file` content part). `image` is reserved and rejected at startup until built |
| `SHIGOTO_EMBED_BASE_URL` | OpenAI-compatible base URL for `/embeddings` |
| `SHIGOTO_EMBED_API_KEY` | secret (vault) |
| `SHIGOTO_EMBED_MODEL` | e.g. `gemini-embedding-2` |
| `SHIGOTO_EMBED_QUERY_PREFIX` / `SHIGOTO_EMBED_DOCUMENT_PREFIX` | optional text prefixed to queries / passages for models that distinguish them |

The embedding dimension is fixed at **1536** in the schema (`vector(1536)`,
HNSW-indexable). Every embeddings request sends `dimensions: 1536`; a response
of another length is an error. Changing the dimension is a new migration, not
config. Consequence (documented in the role README): the embeddings provider
must honour the `dimensions` parameter (Gemini via Google or OpenRouter does);
fixed-size models such as most Cloudflare Workers AI / Ollama embedders
(768/1024) are not supported for embeddings. They remain usable for chat.

Provider capability per purpose:

| Purpose | Requirement |
|---|---|
| Agent | OpenAI-compatible `/chat/completions` with function tools |
| Extraction | as agent, plus accepts PDF `file` content parts (OpenRouter shape) in `file` mode |

PDF input is chosen by capability, not provider name: one encoder seam
(`pdfParts(batch []byte, mode) ([]ContentPart, error)`) turns a page batch
into message content. Only `file` mode is built this round. It keeps the
born-digital text layer of StrataMax PDFs, so figures come from real
characters rather than OCR of a picture. A future `image` mode (page rasters,
optionally plus a local text layer) would add a renderer to the `FROM scratch`
image and is deferred (user decision).
| Embeddings | OpenAI-compatible `/embeddings` honouring `dimensions` | Base URLs must be absolute http(s) URLs; keys are never logged.
`GET /api/v1/meta` gains `strata_assistant: bool`.

Homelab role: matching `shigoto_llm_*` / `shigoto_embed_*` defaults (empty
keys), vault entries for the two API keys, rendered into `app.env.j2`;
`validate.yml` rejects a partial LLM group. Model swap = change variable,
`task deploy TARGET=cloud TAG=erp2_service`.

### 3. Model client (`internal/llm`)

One provider-agnostic HTTP client speaking OpenAI-compatible JSON. No SDKs.

- `Chat(ctx, ChatRequest) (ChatResponse, error)` — `/chat/completions` with
  `messages`, `tools` (function schema), `tool_choice`, `response_format`
  (`json_object` for extraction), `temperature`. User content parts: `text`
  and `file` (PDF as `data:application/pdf;base64,...` with filename — the
  OpenRouter file-input shape, which DeepSeek V4.1 Flash accepts).
- `Embed(ctx, inputs []string, kind Query|Document) ([][]float32, error)` —
  `/embeddings`, applies the configured prefix, sends `dimensions`.
- Errors: 429/5xx are `ErrRetryable` carrying `Retry-After` when present;
  4xx otherwise are permanent. Per-call timeout (extraction 180 s, chat 60 s,
  embed 30 s). Responses bodies capped.
- A test fake (`internal/llm/llmtest`) serves scripted chat/embedding
  responses over `httptest`, used by all non-live tests. An opt-in live smoke
  test runs when `SHIGOTO_TEST_LLM_*` env vars are set (same pattern as S3).

### 4. Data — migration `0025_strata_documents.sql`

```
strata_documents
  id, org_id → orgs, scheme_id → strata_schemes, file_id → files,
  doc_type text CHECK IN ('minutes','by_laws','cms','levy_notice','budget',
                          'financial_statement','sinking_fund_forecast','other'),
  title text NOT NULL, document_date date NOT NULL,
  status text CHECK IN ('pending','extracting','indexed','failed'),
  error text, attempts int, next_attempt_at timestamptz,
  page_count int, extract_model text, extracted_at timestamptz,
  created_at
  UNIQUE (scheme_id, file_id)

strata_document_pages
  document_id → strata_documents ON DELETE CASCADE, page_no int, text text,
  PRIMARY KEY (document_id, page_no)

strata_chunks
  id, org_id, scheme_id, document_id ON DELETE CASCADE,
  page_from int, page_to int, text text,
  tsv tsvector GENERATED ALWAYS AS (to_tsvector('english', text)) STORED,
  embedding vector(1536) NULL, embed_model text NULL
  GIN (tsv); HNSW (embedding vector_cosine_ops); index (scheme_id)

strata_motions
  id, org_id, scheme_id, document_id ON DELETE CASCADE,
  meeting_date date, meeting_kind text CHECK IN ('agm','egm','committee','other'),
  motion_no text, text text NOT NULL,
  outcome text CHECK IN ('carried','defeated','withdrawn','not_put','unknown'),
  page_no int NOT NULL
  index (scheme_id, meeting_date); GIN tsvector on text

strata_figures
  id, org_id, scheme_id, document_id ON DELETE CASCADE,
  fund text CHECK IN ('admin','sinking','other'),
  metric text CHECK IN ('levy','budget_income','budget_expense','actual_income',
                        'actual_expense','balance','arrears','other'),
  label text NOT NULL, period_start date, period_end date NOT NULL,
  amount_cents bigint NOT NULL, page_no int NOT NULL
  index (scheme_id, fund, metric, period_end)
```

Every child row carries `org_id` and `scheme_id` so every query filters both.
`org_id` on the document must equal the scheme's org (enforced in the store).

### 5. Upload, list, delete (property module)

| Method | Path | Permission |
|---|---|---|
| GET | `/strata-schemes/{id}/documents` | property read |
| POST | `/strata-schemes/{id}/documents` (multipart: file, doc_type, title, document_date) | property write |
| GET | `/strata-schemes/{id}/documents/{doc}/file` | property read |
| POST | `/strata-schemes/{id}/documents/{doc}/reindex` | property write |
| DELETE | `/strata-schemes/{id}/documents/{doc}` | property write |

- PDF only (sniffed, not trusted from the client); cap **25 MiB** and **300
  pages** (counted at upload with a pure-Go PDF library; over-limit → 400).
  Uses `module.HandleUpload` with a strata policy; file dedupe as today.
- Scheme must belong to the org (404 otherwise). Writes audited.
- Delete removes the document and its derived rows; the file follows existing
  file GC (`files_gc.go`) once unreferenced. `files_gc.go` and any
  "file referenced" checks learn about `strata_documents.file_id`.
- `reindex` resets status to `pending`, attempts to 0 (used after a failure
  or an extraction-model change).
- List returns status, error message (sanitised, no provider body dumps),
  page count, extract model, and whether all chunks are embedded with the
  current embed model.

### 6. Indexer (background)

A loop started beside the scheduler when the assistant is enabled, ticking
every **30 s** with its own advisory lock id, and kicked immediately after an
upload/reindex (non-blocking channel send). One document at a time.

1. Claim the oldest `pending` (or retryable `failed` whose
   `next_attempt_at <= now`) document; set `extracting`.
2. Read the PDF from `storage.Store`. Split into batches of **5 pages**
   (pure-Go PDF split) so each batch's transcription fits a flash model's
   output limit (~4k tokens per batch). Stored page text is the model's
   transcription, not a local text layer: local Go text extraction scrambles
   StrataMax tables, which is the problem this solves, and model reading also
   copes with scanned pages. Citations always link to the original PDF page,
   so the human checks the source, not the transcription.
3. Per batch, call the extract model with the batch PDF and a type-specific
   instruction, `response_format: json_object`, demanding:
   `{pages:[{page_no, text}], motions:[...]?, figures:[...]?}`. `text` is
   markdown with tables preserved. Motions only for `minutes`; figures only
   for `levy_notice`, `budget`, `financial_statement`,
   `sinking_fund_forecast`.
4. Validate strictly: page numbers exactly cover the batch; enums in range;
   dates parse; amounts are decimal strings converted to integer cents
   (never via float); each item's `page_no` within the batch. Invalid output
   → retry that batch once, then fail the document with a clear error.
5. In one transaction: replace pages, motions, figures, chunks for the
   document; set `indexed`, `extract_model`, `extracted_at`, `page_count`.
6. Chunking: per page, ~1,200 characters with ~200 overlap, never across
   documents; a chunk records its page span.
7. Embedding pass (same loop, after extraction work): embed chunks whose
   `embed_model` is null or differs from the configured model, in batches of
   up to 64, writing vector + model. This also performs full re-embed after
   an embedding-model change.
8. Retryable errors: exponential backoff from 1 min, honour `Retry-After`,
   max **6** attempts, then `failed`. Permanent errors fail immediately.

On startup, documents left in `extracting` (crash) return to `pending`.

### 7. Agent

`POST /api/v1/orgs/{org}/strata-schemes/{id}/ask` — property read.

Request: `{ "role": "chairperson" | "treasurer", "question": string (1–2,000 chars) }`
Response: `{ "answer": markdown, "citations": [{document_id, title, document_date, page_no}], "grounded": bool }`

- 404 if scheme not in org; 403 if the org does not hold that role on the
  scheme; 503 `{error: "strata assistant is not configured"}` when disabled.
- One in-flight question per user (a second concurrent ask → 429). No
  spending cap or rate limit: sole operator, managed outside the app (user
  decision). Document upload/reindex/delete endpoints also return 403 unless
  the org holds chairperson or treasurer on the scheme.
- Nothing is persisted. Log role, scheme, tool-call count, token usage,
  latency — never question or answer text.
- Synchronous; whole request budget **90 s**; at most **8** tool-call rounds.

**Role profiles** (server-side, not model-selectable):

| | Chairperson | Treasurer |
|---|---|---|
| Doc types searchable | minutes, by_laws, cms, other | levy_notice, budget, financial_statement, sinking_fund_forecast, minutes |
| Structured tool | `find_motions` | `get_figures` |
| Prompt focus | decisions, motion history, by-law/CMS interpretation, drafting notices that cite by-laws | levies, budgets vs actuals, fund balances, arrears; arithmetic shown step by step from cited figures |

**Tools** (JSON-schema function tools; `org_id`, `scheme_id` and the role's
doc types are injected by the server and absent from the schema):

- `search_documents(query, doc_types?, date_from?, date_to?)` — hybrid:
  top 30 by `ts_rank` on `tsv` (websearch_to_tsquery) and top 30 by cosine
  distance, fused with reciprocal rank fusion (k = 60), top 8 returned with
  document id/title/date/page span/text. `doc_types` is intersected with the
  role's set. If embeddings are unconfigured or the query embed fails, vector
  half is skipped (full-text only) and the tool result says so.
  Vector search only uses chunks whose `embed_model` equals the current model.
- `list_documents(doc_types?, date_from?, date_to?)` — id, type, title,
  date, page count, status.
- `read_pages(document_id, page_from, page_to)` — max 5 pages per call;
  document must be in scope.
- `find_motions(query?, date_from?, date_to?, outcome?, meeting_kind?)` —
  chairperson; max 25 rows, newest first.
- `get_figures(fund?, metric?, period_from?, period_to?)` — treasurer;
  max 50 rows; amounts returned as cents and formatted dollars.

Tool argument validation is strict (unknown fields, bad dates, out-of-scope
ids → a tool error message to the model, not an HTTP error).

**Grounding.** The system prompt requires every factual claim to cite
`[doc:<document_id> p:<page_no>]`. The server parses those markers, keeps
only those whose (document, page) appeared in a tool result during this
request, renders them as citation list entries, and strips invalid markers.
`grounded` is false when no valid citation remains; the UI shows a warning
in that case. Document text in tool results is wrapped as untrusted content;
since tools are read-only and scoped, injected instructions cannot reach
other data or cause writes.

### 8. UI (Strata tab)

- **Documents card** (scheme-level, shown when the org holds chairperson or
  treasurer): table of documents (title, type, date, status badge, pages),
  download link, reindex and delete actions (delete confirms), and an
  "Upload document" dialog (file, type, title, date) following the dialog
  editor contract (failed-input preservation, duplicate-submit guard,
  explicit cancel, busy dismissal blocked). Status polls every 5 s while any
  document is `pending`/`extracting`.
- **ChairpersonCard / TreasurerCard**: an "Ask" form (textarea + submit,
  disabled while busy, cancelable via abort). Answer rendered as sanitised
  markdown with numbered citations linking to
  `.../documents/{doc}/file#page=N`. Non-grounded warning. Previous answer is
  replaced by the next question; nothing saved. Hidden with an explanatory
  line when `meta.strata_assistant` is false.
- SecretaryCard unchanged. Screen ledger row for the strata route updated.
- API calls added to `lib/api.ts` usage pattern; types in
  `lib/modules/property/types.ts`.

### 9. Testing

- Config YAML table cases: complete/partial/absent LLM and embed groups,
  bad URLs.
- HTTP YAML cases (`strata.yaml` / new `strata-assistant.yaml`): document
  endpoints and `ask` — permissions, foreign org/scheme 404s, role-not-held
  403, disabled 503, non-PDF/oversize 400, `exact: true` denials. New seed
  kind `strata_documents` if needed.
- Integration (testkit + `llmtest` fake): extraction validation (bad page
  coverage, bad enum, float-looking amounts → cents exactly, retry then
  fail), backoff on 429 with Retry-After, crash recovery of `extracting`,
  re-embed on model change, hybrid search fusion and FTS fallback, scope
  injection (model passes another scheme's document id → tool error),
  citation validation (fabricated marker stripped, `grounded:false`),
  tool-round cap.
- Vitest: upload dialog editor contract YAML; Ask panel busy/abort/stale
  response; role gating; disabled state.
- Playwright: upload → indexed (fake provider) → ask → citation link.
- Homelab: `test SUITE=shigoto` asserts rendered env includes the new
  variables and rejects a partial LLM group; image var asserted by variable,
  not literal.

### 10. Rollout

1. Ship image + migration (extension only) first; deploy behind paired cold
   backup; confirm Postgres starts on existing data and `vector` exists.
2. Ship the feature with LLM config empty (disabled), then set vault keys and
   model variables, redeploy.
3. Live acceptance: upload one real minutes PDF and one financial statement;
   spot-check extracted motions/figures against the PDFs; ask the two example
   questions.

## Decisions made by Claude (please check)

- Package layout: `internal/llm` (client + fake); strata assistant code in
  the `property` module (store, indexer, agent, routes) following existing
  file naming (`strata_documents.go`, `strata_indexer.go`, `strata_agent.go`,
  `routes_strata_documents.go`).
- Embedding dimension fixed at 1536; Gemini output truncated to it.
- Doc type list, motion/figure enums, and the treasurer seeing minutes (AGMs
  approve budgets and levies).
- Model transcription (not local text extraction) as stored page text.
- Embeddings restricted to providers honouring `dimensions`.
- Integration tests (Postgres/pgvector) run locally via
  `task test SUITE=integration`; CI does not run them today and this work
  does not change that.
- 25 MiB / 300-page cap; 5-page extraction batches; chunk size; RRF k=60;
  8 tool rounds; 90 s budget; 6 retry attempts.
- `ask` requires property **read**, not write.
- Documents are scheme-level (shared by both roles), not per role.
- Indexer runs every 30 s in-process, separate from the hourly scheduler.
- Delete is allowed and hard-deletes derived rows.

## Left to the builder

- Exact pgvector release tag and Alpine build steps.
- Pure-Go PDF library choice for page count/split.
- Prompt wording, within the grounding/citation rules above.
- Markdown renderer/sanitiser choice in the web app (reuse if one exists).
- Index naming and FK `ON DELETE` style matching existing migrations.
