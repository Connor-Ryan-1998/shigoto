package property

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"shigoto/internal/core"
	"shigoto/internal/llm"
)

// StrataScope bounds every retrieval: one scheme of one org, and the
// document types a role may read. It is always built by the server.
type StrataScope struct {
	OrgID, SchemeID int64
	DocTypes        []StrataDocType
}

// types intersects requested with the scope's types; empty requested
// means all of the scope's.
func (s StrataScope) types(requested []StrataDocType) []string {
	var out []string
	for _, t := range s.DocTypes {
		if len(requested) == 0 || slices.Contains(requested, t) {
			out = append(out, string(t))
		}
	}
	if out == nil {
		out = []string{}
	}
	return out
}

const (
	searchSideLimit = 30
	searchTopK      = 8
	rrfK            = 60
	maxReadPages    = 5
	maxMotions      = 25
	maxFigures      = 50
)

// SearchHit is one chunk returned by SearchStrata.
type SearchHit struct {
	DocumentID       int64
	Title            string
	DocType          StrataDocType
	DocumentDate     time.Time
	PageFrom, PageTo int
	Text             string
}

// SearchResult is SearchStrata's hits; VectorUsed is false when the
// semantic half was skipped (no embeddings configured, or the query could
// not be embedded).
type SearchResult struct {
	Hits       []SearchHit
	VectorUsed bool
}

// SearchStrata finds chunks of indexed documents in scope matching query,
// fusing full-text and vector rankings with reciprocal rank fusion. types
// narrows the scope's document types; from/to bound document_date.
func SearchStrata(ctx context.Context, q core.Querier, c *llm.Client, s StrataScope, query string, types []StrataDocType, from, to *time.Time) (SearchResult, error) {
	var vec any
	model := ""
	if c != nil && c.EmbedModel() != "" {
		v, err := c.Embed(ctx, []string{query}, llm.Query)
		if err != nil {
			slog.Warn("embedding strata search query failed; using full-text search only", "error", err)
		} else {
			vec, model = vectorLiteral(v[0]), c.EmbedModel()
		}
	}
	rows, err := q.Query(ctx, `
		WITH scoped AS (
			SELECT c.id, c.tsv, c.embedding, c.embed_model
			FROM strata_chunks c
			JOIN strata_documents d ON d.id = c.document_id
			WHERE c.org_id = $1 AND c.scheme_id = $2 AND d.status = 'indexed' AND d.doc_type = ANY($3)
				AND ($4::date IS NULL OR d.document_date >= $4) AND ($5::date IS NULL OR d.document_date <= $5)
		),
		query AS (
			-- OR the query's terms: an agent's search phrase rarely appears
			-- whole in one chunk; ts_rank still favours chunks matching more.
			SELECT nullif(replace(plainto_tsquery('english', $6)::text, '&', '|'), '')::tsquery AS tsq
		),
		fts AS (
			SELECT s.id, row_number() OVER (ORDER BY ts_rank(s.tsv, query.tsq) DESC, s.id) AS r
			FROM scoped s, query
			WHERE query.tsq IS NOT NULL AND s.tsv @@ query.tsq
			ORDER BY r LIMIT $9
		),
		vec AS (
			SELECT s.id, row_number() OVER (ORDER BY s.embedding <=> $7::text::public.vector, s.id) AS r
			FROM scoped s
			WHERE $7::text IS NOT NULL AND s.embed_model = $8
			ORDER BY r LIMIT $9
		),
		fused AS (
			SELECT id, sum(1.0 / ($10 + r)) AS score
			FROM (SELECT id, r FROM fts UNION ALL SELECT id, r FROM vec) ranked
			GROUP BY id
		)
		SELECT d.id, d.title, d.doc_type, d.document_date, c.page_from, c.page_to, c.text
		FROM fused f
		JOIN strata_chunks c ON c.id = f.id
		JOIN strata_documents d ON d.id = c.document_id
		ORDER BY f.score DESC, c.id
		LIMIT $11`,
		s.OrgID, s.SchemeID, s.types(types), from, to, query, vec, model, searchSideLimit, rrfK, searchTopK)
	if err != nil {
		return SearchResult{}, err
	}
	hits, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (SearchHit, error) {
		var h SearchHit
		err := row.Scan(&h.DocumentID, &h.Title, &h.DocType, &h.DocumentDate, &h.PageFrom, &h.PageTo, &h.Text)
		return h, err
	})
	if err != nil {
		return SearchResult{}, err
	}
	return SearchResult{Hits: hits, VectorUsed: vec != nil}, nil
}

// ListStrataDocsForScope lists documents in scope (any status), newest
// first.
func ListStrataDocsForScope(ctx context.Context, q core.Querier, s StrataScope, types []StrataDocType, from, to *time.Time) ([]StrataDocument, error) {
	rows, err := q.Query(ctx, `SELECT `+strataDocumentColumns+` FROM strata_documents d
		WHERE d.org_id = $1 AND d.scheme_id = $2 AND d.doc_type = ANY($3)
			AND ($4::date IS NULL OR d.document_date >= $4) AND ($5::date IS NULL OR d.document_date <= $5)
		ORDER BY d.document_date DESC, d.id DESC
		LIMIT 100`, s.OrgID, s.SchemeID, s.types(types), from, to)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (StrataDocument, error) { return scanStrataDocument(row) })
}

// ReadStrataPages returns pages from..to (at most five) of an indexed
// document in scope; core.ErrNotFound when the document is out of scope or
// not indexed.
func ReadStrataPages(ctx context.Context, q core.Querier, s StrataScope, docID int64, from, to int) ([]ExtractedPage, error) {
	if from < 1 || to < from || to-from+1 > maxReadPages {
		return nil, fmt.Errorf("%w: page_from..page_to must be 1 to %d pages", core.ErrInvalidInput, maxReadPages)
	}
	var ok bool
	if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM strata_documents
		WHERE id = $1 AND org_id = $2 AND scheme_id = $3 AND doc_type = ANY($4) AND status = 'indexed')`,
		docID, s.OrgID, s.SchemeID, s.types(nil)).Scan(&ok); err != nil {
		return nil, err
	}
	if !ok {
		return nil, core.ErrNotFound
	}
	rows, err := q.Query(ctx, `SELECT page_no, text FROM strata_document_pages
		WHERE document_id = $1 AND page_no BETWEEN $2 AND $3 ORDER BY page_no`, docID, from, to)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (ExtractedPage, error) {
		var p ExtractedPage
		err := row.Scan(&p.PageNo, &p.Text)
		return p, err
	})
}

// Motion is an extracted motion with its source document.
type Motion struct {
	ExtractedMotion
	DocumentID   int64
	Title        string
	DocumentDate time.Time
}

// FindMotions lists motions in scope, newest meeting first (at most 25).
// query (full-text, any term), the meeting date range, outcome and
// meeting kind are optional filters.
func FindMotions(ctx context.Context, q core.Querier, s StrataScope, query string, from, to *time.Time, outcome, kind string) ([]Motion, error) {
	rows, err := q.Query(ctx, `
		SELECT m.meeting_date, m.meeting_kind, m.motion_no, m.text, m.outcome, m.page_no, d.id, d.title, d.document_date
		FROM strata_motions m
		JOIN strata_documents d ON d.id = m.document_id
		WHERE m.org_id = $1 AND m.scheme_id = $2 AND d.status = 'indexed' AND d.doc_type = ANY($3)
			AND ($4 = '' OR m.tsv @@ replace(plainto_tsquery('english', $4)::text, '&', '|')::tsquery)
			AND ($5::date IS NULL OR m.meeting_date >= $5) AND ($6::date IS NULL OR m.meeting_date <= $6)
			AND ($7 = '' OR m.outcome = $7) AND ($8 = '' OR m.meeting_kind = $8)
		ORDER BY m.meeting_date DESC, m.id DESC
		LIMIT $9`, s.OrgID, s.SchemeID, s.types(nil), query, from, to, outcome, kind, maxMotions)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Motion, error) {
		var m Motion
		err := row.Scan(&m.MeetingDate, &m.MeetingKind, &m.MotionNo, &m.Text, &m.Outcome, &m.PageNo, &m.DocumentID, &m.Title, &m.DocumentDate)
		return m, err
	})
}

// Figure is an extracted financial figure with its source document.
type Figure struct {
	ExtractedFigure
	DocumentID   int64
	Title        string
	DocumentDate time.Time
}

// GetFigures lists figures in scope, latest period first (at most 50),
// optionally filtered by fund, metric and a period_end range.
func GetFigures(ctx context.Context, q core.Querier, s StrataScope, fund, metric string, from, to *time.Time) ([]Figure, error) {
	rows, err := q.Query(ctx, `
		SELECT f.fund, f.metric, f.label, f.period_start, f.period_end, f.amount_cents, f.page_no, d.id, d.title, d.document_date
		FROM strata_figures f
		JOIN strata_documents d ON d.id = f.document_id
		WHERE f.org_id = $1 AND f.scheme_id = $2 AND d.status = 'indexed' AND d.doc_type = ANY($3)
			AND ($4 = '' OR f.fund = $4) AND ($5 = '' OR f.metric = $5)
			AND ($6::date IS NULL OR f.period_end >= $6) AND ($7::date IS NULL OR f.period_end <= $7)
		ORDER BY f.period_end DESC, f.fund, f.metric, f.id
		LIMIT $8`, s.OrgID, s.SchemeID, s.types(nil), fund, metric, from, to, maxFigures)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Figure, error) {
		var f Figure
		var cents int64
		err := row.Scan(&f.Fund, &f.Metric, &f.Label, &f.PeriodStart, &f.PeriodEnd, &cents, &f.PageNo, &f.DocumentID, &f.Title, &f.DocumentDate)
		f.AmountCents = core.Cents(cents)
		return f, err
	})
}
