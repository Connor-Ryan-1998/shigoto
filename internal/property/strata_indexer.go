package property

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"shigoto/internal/core"
	"shigoto/internal/llm"
	"shigoto/internal/storage"
)

const (
	// strataIndexerLockID is the advisory lock held for one indexer tick,
	// so only one instance reads documents at a time.
	strataIndexerLockID  = 7415
	strataExtractTimeout = 180 * time.Second
	strataRetryBase      = time.Minute
	strataEmbedBatch     = 64
	// strataEmbedBatchesPerTick bounds one tick's embedding work so a
	// full re-embed cannot hold the lock indefinitely.
	strataEmbedBatchesPerTick = 50
	strataChunkSize           = 1200
	strataChunkOverlap        = 200
)

// StrataIndexer reads pending strata documents with the extraction model
// and embeds their chunks. It runs in-process beside the scheduler.
type StrataIndexer struct {
	Pool  *pgxpool.Pool
	Store storage.Store
	LLM   *llm.Client
	// Now is the clock (time.Now when nil).
	Now func() time.Time
}

func (x *StrataIndexer) now() time.Time {
	if x.Now == nil {
		return time.Now()
	}
	return x.Now()
}

// withLock runs fn holding the indexer's advisory lock on a dedicated
// connection; when another instance holds it, fn is skipped.
func (x *StrataIndexer) withLock(ctx context.Context, fn func() error) error {
	conn, err := x.Pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()
	var locked bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", strataIndexerLockID).Scan(&locked); err != nil {
		return fmt.Errorf("acquiring strata indexer lock: %w", err)
	}
	if !locked {
		return nil
	}
	defer func() {
		if _, err := conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", strataIndexerLockID); err != nil {
			slog.Error("releasing strata indexer lock", "error", err)
			_ = conn.Conn().Close(context.Background())
		}
	}()
	return fn()
}

// Recover returns documents left "extracting" by a crash to "pending".
func (x *StrataIndexer) Recover(ctx context.Context) error {
	return x.withLock(ctx, func() error {
		_, err := x.Pool.Exec(ctx, `UPDATE strata_documents SET status = 'pending' WHERE status = 'extracting'`)
		return err
	})
}

// Run recovers, then ticks immediately, on every tick of every and
// whenever kick receives, until ctx is done. Tick errors are logged.
func (x *StrataIndexer) Run(ctx context.Context, every time.Duration, kick <-chan struct{}) {
	if err := x.Recover(ctx); err != nil {
		slog.Error("strata indexer recovery failed", "error", err)
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		if err := x.Tick(ctx); err != nil && ctx.Err() == nil {
			slog.Error("strata indexer tick failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-kick:
		}
	}
}

// Tick reads at most one due document, then embeds chunks missing an
// embedding from the current model. It returns an error only for
// database failures; model failures are recorded on the document.
func (x *StrataIndexer) Tick(ctx context.Context) error {
	return x.withLock(ctx, func() error {
		doc, ok, err := x.claim(ctx)
		if err != nil {
			return fmt.Errorf("claiming strata document: %w", err)
		}
		if ok {
			if err := x.process(ctx, doc); err != nil {
				return err
			}
		}
		x.embedPending(ctx)
		return nil
	})
}

func (x *StrataIndexer) claim(ctx context.Context) (StrataDocument, bool, error) {
	doc, err := scanStrataDocument(x.Pool.QueryRow(ctx, `
		UPDATE strata_documents d SET status = 'extracting'
		WHERE d.id = (
			SELECT id FROM strata_documents
			WHERE status = 'pending' OR (status = 'failed' AND attempts < $1 AND next_attempt_at <= $2)
			ORDER BY created_at, id LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING `+strataDocumentColumns, StrataMaxAttempts, x.now()))
	if errors.Is(err, pgx.ErrNoRows) {
		return StrataDocument{}, false, nil
	}
	return doc, err == nil, err
}

// process reads doc batch by batch and stores the result, or records the
// failure. Its error is a database failure only.
func (x *StrataIndexer) process(ctx context.Context, doc StrataDocument) error {
	ext, err := x.extract(ctx, doc)
	if err != nil {
		if ctx.Err() != nil {
			// Shutting down: leave it for Recover.
			return nil
		}
		return x.recordFailure(ctx, doc, err)
	}
	return x.save(ctx, doc, ext)
}

func (x *StrataIndexer) extract(ctx context.Context, doc StrataDocument) (Extraction, error) {
	f, err := core.GetFile(ctx, x.Pool, doc.OrgID, doc.FileID)
	if err != nil {
		return Extraction{}, fmt.Errorf("loading file: %w", err)
	}
	blob, err := core.OpenFile(ctx, x.Store, f)
	if err != nil {
		return Extraction{}, fmt.Errorf("opening file: %w", err)
	}
	pdf, err := io.ReadAll(blob)
	blob.Close()
	if err != nil {
		return Extraction{}, fmt.Errorf("reading file: %w", err)
	}
	batches, err := SplitPDF(pdf, strataBatchPages)
	if err != nil {
		return Extraction{}, err
	}
	var all Extraction
	for _, b := range batches {
		ext, err := x.extractBatch(ctx, doc, b)
		if err != nil {
			return Extraction{}, err
		}
		all.Pages = append(all.Pages, ext.Pages...)
		all.Motions = append(all.Motions, ext.Motions...)
		all.Figures = append(all.Figures, ext.Figures...)
	}
	return all, nil
}

// extractBatch asks the model for one batch, retrying once when its reply
// fails validation.
func (x *StrataIndexer) extractBatch(ctx context.Context, doc StrataDocument, b PageBatch) (Extraction, error) {
	parts, err := llm.PDFParts(x.LLM.PDFMode(), fmt.Sprintf("pages-%d-%d.pdf", b.From, b.To), b.PDF)
	if err != nil {
		return Extraction{}, fmt.Errorf("%w: %v", llm.ErrPermanent, err)
	}
	parts = append(parts, llm.ContentPart{Type: "text", Text: extractInstruction(doc.DocType, b)})
	req := llm.ChatRequest{
		Model:      x.LLM.ExtractModel(),
		Messages:   []llm.Message{{Role: llm.RoleUser, Content: parts}},
		JSONObject: true,
		Timeout:    strataExtractTimeout,
	}
	var lastErr error
	for range 2 {
		resp, err := x.LLM.Chat(ctx, req)
		if err != nil {
			return Extraction{}, err
		}
		ext, err := ParseExtraction([]byte(resp.Content), doc.DocType, b)
		if err == nil {
			return ext, nil
		}
		lastErr = err
	}
	return Extraction{}, lastErr
}

// failureMessage is the user-facing reason stored on a failed document. It
// never includes provider response text.
func failureMessage(err error) string {
	var re *llm.RetryableError
	switch {
	case errors.As(err, &re) && re.Status != 0:
		return "The model provider is unavailable (HTTP " + strconv.Itoa(re.Status) + ")."
	case errors.As(err, &re):
		return "The model provider could not be reached."
	case errors.Is(err, llm.ErrPermanent):
		return "The model provider rejected the request; check the model settings."
	case errors.Is(err, core.ErrInvalidInput):
		msg := strings.TrimPrefix(err.Error(), core.ErrInvalidInput.Error()+": ")
		if len(msg) > 240 {
			msg = msg[:240]
		}
		return "The document could not be read: " + msg
	default:
		return "Indexing failed unexpectedly."
	}
}

func (x *StrataIndexer) recordFailure(ctx context.Context, doc StrataDocument, cause error) error {
	attempts := doc.Attempts + 1
	var next *time.Time
	var re *llm.RetryableError
	if errors.As(cause, &re) && attempts < StrataMaxAttempts {
		delay := max(re.RetryAfter, strataRetryBase<<(attempts-1))
		t := x.now().Add(delay)
		next = &t
	} else {
		attempts = StrataMaxAttempts
	}
	slog.Warn("strata document indexing failed", "document_id", doc.ID, "attempts", attempts, "error", cause)
	// Only a document still being extracted is updated: a reindex or
	// delete meanwhile wins.
	_, err := x.Pool.Exec(ctx, `UPDATE strata_documents SET status = 'failed', attempts = $2, next_attempt_at = $3, error = $4
		WHERE id = $1 AND status = 'extracting'`, doc.ID, attempts, next, failureMessage(cause))
	return err
}

func (x *StrataIndexer) save(ctx context.Context, doc StrataDocument, ext Extraction) error {
	tx, err := x.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE strata_documents
		SET status = 'indexed', error = '', next_attempt_at = NULL, extract_model = $2, extracted_at = $3
		WHERE id = $1 AND status = 'extracting'`, doc.ID, x.LLM.ExtractModel(), x.now())
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		// Reindexed or deleted while the model was reading.
		return nil
	}
	for _, table := range []string{"strata_document_pages", "strata_chunks", "strata_motions", "strata_figures"} {
		if _, err := tx.Exec(ctx, `DELETE FROM `+table+` WHERE document_id = $1`, doc.ID); err != nil {
			return err
		}
	}
	batch := &pgx.Batch{}
	for _, p := range ext.Pages {
		batch.Queue(`INSERT INTO strata_document_pages (document_id, page_no, text) VALUES ($1, $2, $3)`, doc.ID, p.PageNo, p.Text)
	}
	for _, c := range ChunkPages(ext.Pages, strataChunkSize, strataChunkOverlap) {
		batch.Queue(`INSERT INTO strata_chunks (org_id, scheme_id, document_id, page_from, page_to, text) VALUES ($1, $2, $3, $4, $5, $6)`,
			doc.OrgID, doc.SchemeID, doc.ID, c.PageFrom, c.PageTo, c.Text)
	}
	for _, m := range ext.Motions {
		batch.Queue(`INSERT INTO strata_motions (org_id, scheme_id, document_id, meeting_date, meeting_kind, motion_no, text, outcome, page_no)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			doc.OrgID, doc.SchemeID, doc.ID, m.MeetingDate, m.MeetingKind, m.MotionNo, m.Text, m.Outcome, m.PageNo)
	}
	for _, f := range ext.Figures {
		batch.Queue(`INSERT INTO strata_figures (org_id, scheme_id, document_id, fund, metric, label, period_start, period_end, amount_cents, page_no)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			doc.OrgID, doc.SchemeID, doc.ID, f.Fund, f.Metric, f.Label, f.PeriodStart, f.PeriodEnd, int64(f.AmountCents), f.PageNo)
	}
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return fmt.Errorf("storing extraction for document %d: %w", doc.ID, err)
	}
	return tx.Commit(ctx)
}

// embedPending embeds chunks of indexed documents that lack an embedding
// from the current model. Failures are logged and retried next tick.
func (x *StrataIndexer) embedPending(ctx context.Context) {
	model := x.LLM.EmbedModel()
	if model == "" {
		return
	}
	for range strataEmbedBatchesPerTick {
		rows, err := x.Pool.Query(ctx, `
			SELECT c.id, c.text FROM strata_chunks c
			JOIN strata_documents d ON d.id = c.document_id AND d.status = 'indexed'
			WHERE c.embed_model IS DISTINCT FROM $1
			ORDER BY c.id LIMIT $2`, model, strataEmbedBatch)
		if err != nil {
			slog.Error("listing strata chunks to embed", "error", err)
			return
		}
		var ids []int64
		var texts []string
		for rows.Next() {
			var id int64
			var text string
			if err := rows.Scan(&id, &text); err != nil {
				rows.Close()
				slog.Error("scanning strata chunk", "error", err)
				return
			}
			ids, texts = append(ids, id), append(texts, text)
		}
		rows.Close()
		if err := rows.Err(); err != nil || len(ids) == 0 {
			if err != nil {
				slog.Error("listing strata chunks to embed", "error", err)
			}
			return
		}
		vecs, err := x.LLM.Embed(ctx, texts, llm.Document)
		if err != nil {
			slog.Warn("embedding strata chunks failed; will retry", "error", err)
			return
		}
		batch := &pgx.Batch{}
		for i, id := range ids {
			batch.Queue(`UPDATE strata_chunks SET embedding = $2::text::public.vector, embed_model = $3 WHERE id = $1`, id, vectorLiteral(vecs[i]), model)
		}
		if err := x.Pool.SendBatch(ctx, batch).Close(); err != nil {
			slog.Error("storing strata embeddings", "error", err)
			return
		}
	}
}

// vectorLiteral renders v in pgvector's text form "[a,b,...]".
func vectorLiteral(v []float32) string {
	var b strings.Builder
	b.Grow(len(v) * 10)
	b.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(f), 'g', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}
