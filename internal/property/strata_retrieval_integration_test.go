//go:build integration

package property_test

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"shigoto/internal/core"
	"shigoto/internal/llm"
	"shigoto/internal/llm/llmtest"
	"shigoto/internal/property"
	"shigoto/internal/storage"
	"shigoto/internal/testkit"
)

// retrievalWorld has an org with a scheme holding both officer positions,
// a fake provider whose embeddings are controllable, and helpers that
// insert already-indexed documents directly.
type retrievalWorld struct {
	e      *testkit.Env
	fake   *llmtest.Fake
	store  storage.Store
	org    core.Org
	scheme property.StrataScheme
	vecs   map[string][]float32
	seq    int
}

func newRetrievalWorld(t *testing.T) *retrievalWorld {
	t.Helper()
	e := testkit.New(t)
	org := e.Org("acme")
	w := &retrievalWorld{e: e, fake: llmtest.New(t), store: storage.NewDisk(t.TempDir()), org: org,
		scheme: e.StrataScheme(org, testkit.Positions(true, false, true)), vecs: map[string][]float32{}}
	w.fake.EmbedFunc(func(s string) []float32 {
		if v, ok := w.vecs[s]; ok {
			return v
		}
		return llmtest.HashVector(s)
	})
	return w
}

func (w *retrievalWorld) client(embedModel string) *llm.Client {
	var ec = w.fake.EmbedConfig(embedModel)
	if embedModel == "" {
		ec = nil
	}
	return llm.New(w.fake.LLMConfig(), ec, http.DefaultClient)
}

// doc inserts an indexed document of type t dated date in scheme schemeID
// (0 = w.scheme) of org (zero = w.org).
func (w *retrievalWorld) doc(t *testing.T, org core.Org, schemeID int64, typ property.StrataDocType, date string) int64 {
	t.Helper()
	if org.ID == 0 {
		org = w.org
	}
	if schemeID == 0 {
		schemeID = w.scheme.ID
	}
	w.seq++
	d := w.e.StrataDocument(w.store, org, schemeID, testkit.DocType(typ), testkit.DocDate(testkit.Date(date)),
		testkit.DocTitle(fmt.Sprintf("%s %s", typ, date)), testkit.DocPDF(testkit.PDF(fmt.Sprintf("fixture %d", w.seq))))
	if _, err := w.e.Pool.Exec(w.e.Ctx, `UPDATE strata_documents SET status = 'indexed' WHERE id = $1`, d.ID); err != nil {
		t.Fatal(err)
	}
	return d.ID
}

func (w *retrievalWorld) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := w.e.Pool.Exec(w.e.Ctx, sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func vecLiteral(v []float32) string {
	parts := make([]string, len(v))
	for i, f := range v {
		parts[i] = strconv.FormatFloat(float64(f), 'g', -1, 32)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// chunk inserts a chunk of document docID on page, embedded with model
// as vec (nil vec: not embedded).
func (w *retrievalWorld) chunk(t *testing.T, docID int64, page int, text string, vec []float32, model string) {
	t.Helper()
	var lit, mdl any
	if vec != nil {
		lit, mdl = vecLiteral(vec), model
	}
	w.exec(t, `INSERT INTO strata_chunks (org_id, scheme_id, document_id, page_from, page_to, text, embedding, embed_model)
		SELECT org_id, scheme_id, id, $2, $2, $3, $4::text::public.vector, $5 FROM strata_documents WHERE id = $1`, docID, page, text, lit, mdl)
	w.exec(t, `INSERT INTO strata_document_pages (document_id, page_no, text) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, docID, page, text)
}

var (
	chairScope = func(w *retrievalWorld) property.StrataScope {
		return property.StrataScope{OrgID: w.org.ID, SchemeID: w.scheme.ID, DocTypes: []property.StrataDocType{property.DocMinutes, property.DocByLaws, property.DocCMS, property.DocOther}}
	}
	treasScope = func(w *retrievalWorld) property.StrataScope {
		return property.StrataScope{OrgID: w.org.ID, SchemeID: w.scheme.ID, DocTypes: []property.StrataDocType{property.DocLevyNotice, property.DocBudget, property.DocFinancialStatement, property.DocSinkingFundForecast, property.DocMinutes}}
	}
)

func hitTexts(r property.SearchResult) []string {
	var out []string
	for _, h := range r.Hits {
		out = append(out, h.Text)
	}
	return out
}

func TestSearchFusesFTSAndVector(t *testing.T) {
	w := newRetrievalWorld(t)
	doc := w.doc(t, core.Org{}, 0, property.DocMinutes, "2024-09-12")
	query := llmtest.HashVector("waterproofing")
	w.vecs["waterproofing"] = query
	w.chunk(t, doc, 1, "The roof leaks above unit 4", llmtest.HashVector("a"), "m1")
	w.chunk(t, doc, 2, "Membrane replacement quote accepted", query, "m1")
	w.chunk(t, doc, 3, "Pool fencing", llmtest.HashVector("b"), "m1")

	res, err := property.SearchStrata(w.e.Ctx, w.e.Pool, w.client("m1"), chairScope(w), "roof waterproofing", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.VectorUsed {
		t.Fatal("VectorUsed = false")
	}
	got := strings.Join(hitTexts(res), "|")
	if !strings.Contains(got, "The roof leaks") || !strings.Contains(got, "Membrane replacement") {
		t.Fatalf("hits = %v, want both the lexical and the semantic match", hitTexts(res))
	}
	if res.Hits[0].DocumentID != doc || res.Hits[0].Title == "" || res.Hits[0].DocumentDate.IsZero() {
		t.Fatalf("hit metadata = %+v", res.Hits[0])
	}
	// The chunk found by both halves outranks one found by a single half.
	w.vecs["roof"] = llmtest.HashVector("a")
	res, _ = property.SearchStrata(w.e.Ctx, w.e.Pool, w.client("m1"), chairScope(w), "roof", nil, nil, nil)
	if len(res.Hits) == 0 || res.Hits[0].Text != "The roof leaks above unit 4" {
		t.Fatalf("top hit = %v", hitTexts(res))
	}
}

func TestSearchCapsAtEight(t *testing.T) {
	w := newRetrievalWorld(t)
	doc := w.doc(t, core.Org{}, 0, property.DocMinutes, "2024-09-12")
	for i := 1; i <= 12; i++ {
		w.chunk(t, doc, i, fmt.Sprintf("roof item %d", i), nil, "")
	}
	res, err := property.SearchStrata(w.e.Ctx, w.e.Pool, w.client(""), chairScope(w), "roof", nil, nil, nil)
	if err != nil || len(res.Hits) != 8 {
		t.Fatalf("hits = %d, %v; want 8", len(res.Hits), err)
	}
}

func TestSearchFTSOnlyWhenNoEmbed(t *testing.T) {
	w := newRetrievalWorld(t)
	doc := w.doc(t, core.Org{}, 0, property.DocMinutes, "2024-09-12")
	w.chunk(t, doc, 1, "The roof leaks", nil, "")
	res, err := property.SearchStrata(w.e.Ctx, w.e.Pool, w.client(""), chairScope(w), "roof", nil, nil, nil)
	if err != nil || res.VectorUsed || len(res.Hits) != 1 {
		t.Fatalf("res = %+v, %v", res, err)
	}
	if n := len(w.fake.Requests("/embeddings")); n != 0 {
		t.Fatalf("embedding calls = %d", n)
	}
}

func TestSearchFallsBackWhenQueryEmbedFails(t *testing.T) {
	w := newRetrievalWorld(t)
	doc := w.doc(t, core.Org{}, 0, property.DocMinutes, "2024-09-12")
	w.chunk(t, doc, 1, "The roof leaks", llmtest.HashVector("x"), "m1")
	w.fake.EmbedError(&llmtest.ChatScript{Status: 503})
	res, err := property.SearchStrata(w.e.Ctx, w.e.Pool, w.client("m1"), chairScope(w), "roof", nil, nil, nil)
	if err != nil || res.VectorUsed || len(res.Hits) != 1 {
		t.Fatalf("res = %+v, %v", res, err)
	}
}

func TestSearchIgnoresStaleEmbeddings(t *testing.T) {
	w := newRetrievalWorld(t)
	doc := w.doc(t, core.Org{}, 0, property.DocMinutes, "2024-09-12")
	q := llmtest.HashVector("waterproofing")
	w.vecs["waterproofing"] = q
	w.chunk(t, doc, 1, "Membrane replacement", q, "old")
	w.chunk(t, doc, 2, "waterproofing of balconies", llmtest.HashVector("z"), "old")
	res, err := property.SearchStrata(w.e.Ctx, w.e.Pool, w.client("new"), chairScope(w), "waterproofing", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := hitTexts(res); len(got) != 1 || got[0] != "waterproofing of balconies" {
		t.Fatalf("hits = %v, want only the lexical match", got)
	}
}

func TestSearchFiltersTypesAndDates(t *testing.T) {
	w := newRetrievalWorld(t)
	old := w.doc(t, core.Org{}, 0, property.DocMinutes, "2022-09-12")
	recent := w.doc(t, core.Org{}, 0, property.DocMinutes, "2024-09-12")
	laws := w.doc(t, core.Org{}, 0, property.DocByLaws, "2020-01-01")
	budget := w.doc(t, core.Org{}, 0, property.DocBudget, "2024-07-01")
	for _, d := range []int64{old, recent, laws, budget} {
		w.chunk(t, d, 1, fmt.Sprintf("roof clause doc %d", d), nil, "")
	}
	from := testkit.Date("2024-01-01")
	res, _ := property.SearchStrata(w.e.Ctx, w.e.Pool, w.client(""), chairScope(w), "roof", nil, &from, nil)
	if len(res.Hits) != 1 || res.Hits[0].DocumentID != recent {
		t.Fatalf("date filter hits = %+v", res.Hits)
	}
	res, _ = property.SearchStrata(w.e.Ctx, w.e.Pool, w.client(""), chairScope(w), "roof", []property.StrataDocType{property.DocByLaws}, nil, nil)
	if len(res.Hits) != 1 || res.Hits[0].DocumentID != laws {
		t.Fatalf("type filter hits = %+v", res.Hits)
	}
	// The chairperson's scope never reaches budgets, even when asked.
	res, _ = property.SearchStrata(w.e.Ctx, w.e.Pool, w.client(""), chairScope(w), "roof", []property.StrataDocType{property.DocBudget}, nil, nil)
	if len(res.Hits) != 0 {
		t.Fatalf("out-of-scope type returned %+v", res.Hits)
	}
}

func TestScopeIsolation(t *testing.T) {
	w := newRetrievalWorld(t)
	other := w.e.Org("other")
	otherScheme := w.e.StrataScheme(other, testkit.Positions(true, true, true))
	secondScheme := w.e.StrataScheme(w.org, testkit.SchemeName("Second"), testkit.Positions(true, false, false))
	mine := w.doc(t, core.Org{}, 0, property.DocMinutes, "2024-09-12")
	theirs := w.doc(t, other, otherScheme.ID, property.DocMinutes, "2024-09-12")
	sibling := w.doc(t, core.Org{}, secondScheme.ID, property.DocMinutes, "2024-09-12")
	budget := w.doc(t, core.Org{}, 0, property.DocBudget, "2024-07-01")
	for _, d := range []int64{mine, theirs, sibling, budget} {
		w.chunk(t, d, 1, "roof", llmtest.HashVector("roof"), "m1")
		w.exec(t, `INSERT INTO strata_motions (org_id, scheme_id, document_id, meeting_date, meeting_kind, text, outcome, page_no)
			SELECT org_id, scheme_id, id, '2024-09-12', 'agm', 'Fix the roof', 'carried', 1 FROM strata_documents WHERE id = $1`, d)
		w.exec(t, `INSERT INTO strata_figures (org_id, scheme_id, document_id, fund, metric, label, period_end, amount_cents, page_no)
			SELECT org_id, scheme_id, id, 'sinking', 'balance', 'Closing', '2025-06-30', 100, 1 FROM strata_documents WHERE id = $1`, d)
	}
	w.vecs["roof"] = llmtest.HashVector("roof")
	scope := chairScope(w)

	res, err := property.SearchStrata(w.e.Ctx, w.e.Pool, w.client("m1"), scope, "roof", nil, nil, nil)
	if err != nil || len(res.Hits) != 1 || res.Hits[0].DocumentID != mine {
		t.Fatalf("search hits = %+v, %v; want only %d", res.Hits, err, mine)
	}
	docs, err := property.ListStrataDocsForScope(w.e.Ctx, w.e.Pool, scope, nil, nil, nil)
	if err != nil || len(docs) != 1 || docs[0].ID != mine {
		t.Fatalf("list = %+v, %v", docs, err)
	}
	for _, id := range []int64{theirs, sibling, budget} {
		if _, err := property.ReadStrataPages(w.e.Ctx, w.e.Pool, scope, id, 1, 1); !errors.Is(err, core.ErrNotFound) {
			t.Errorf("ReadStrataPages(%d) err = %v, want ErrNotFound", id, err)
		}
	}
	motions, err := property.FindMotions(w.e.Ctx, w.e.Pool, scope, "roof", nil, nil, "", "")
	if err != nil || len(motions) != 1 || motions[0].DocumentID != mine {
		t.Fatalf("motions = %+v, %v", motions, err)
	}
	figures, err := property.GetFigures(w.e.Ctx, w.e.Pool, treasScope(w), "", "", nil, nil)
	if err != nil || len(figures) != 2 {
		t.Fatalf("figures = %+v, %v; want this scheme's minutes and budget only", figures, err)
	}
	for _, f := range figures {
		if f.DocumentID != mine && f.DocumentID != budget {
			t.Fatalf("figure from document %d leaked", f.DocumentID)
		}
	}
}

func TestReadPages(t *testing.T) {
	w := newRetrievalWorld(t)
	doc := w.doc(t, core.Org{}, 0, property.DocMinutes, "2024-09-12")
	for p := 1; p <= 7; p++ {
		w.exec(t, `INSERT INTO strata_document_pages (document_id, page_no, text) VALUES ($1, $2, $3)`, doc, p, fmt.Sprintf("page %d", p))
	}
	pages, err := property.ReadStrataPages(w.e.Ctx, w.e.Pool, chairScope(w), doc, 2, 6)
	if err != nil || len(pages) != 5 || pages[0].PageNo != 2 || pages[4].Text != "page 6" {
		t.Fatalf("pages = %+v, %v", pages, err)
	}
	for _, span := range [][2]int{{1, 6}, {0, 2}, {4, 3}} {
		if _, err := property.ReadStrataPages(w.e.Ctx, w.e.Pool, chairScope(w), doc, span[0], span[1]); !errors.Is(err, core.ErrInvalidInput) {
			t.Errorf("span %v err = %v, want ErrInvalidInput", span, err)
		}
	}
}

func TestReadPagesOfUnindexedDocument(t *testing.T) {
	w := newRetrievalWorld(t)
	d := w.e.StrataDocument(w.store, w.org, w.scheme.ID)
	if _, err := property.ReadStrataPages(w.e.Ctx, w.e.Pool, chairScope(w), d.ID, 1, 1); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound for a pending document", err)
	}
}

func TestFindMotionsFilters(t *testing.T) {
	w := newRetrievalWorld(t)
	doc := w.doc(t, core.Org{}, 0, property.DocMinutes, "2024-09-12")
	add := func(date, kind, text, outcome string) {
		w.exec(t, `INSERT INTO strata_motions (org_id, scheme_id, document_id, meeting_date, meeting_kind, text, outcome, page_no)
			SELECT org_id, scheme_id, id, $2, $3, $4, $5, 1 FROM strata_documents WHERE id = $1`, doc, date, kind, text, outcome)
	}
	add("2023-03-01", "committee", "Inspect the roof", "carried")
	add("2024-09-12", "agm", "Replace the roof membrane", "carried")
	add("2024-10-01", "egm", "Paint the roof", "defeated")
	add("2024-11-01", "committee", "Pool gate", "carried")
	for i := 0; i < 30; i++ {
		add("2020-01-01", "committee", fmt.Sprintf("Roof minor item %d", i), "carried")
	}

	from, to := testkit.Date("2024-01-01"), testkit.Date("2024-12-31")
	got, err := property.FindMotions(w.e.Ctx, w.e.Pool, chairScope(w), "roof", &from, &to, "", "")
	if err != nil || len(got) != 2 || got[0].Text != "Paint the roof" || got[1].Text != "Replace the roof membrane" {
		t.Fatalf("motions = %+v, %v; want 2024 roof motions newest first", got, err)
	}
	if got[1].Title == "" || got[1].PageNo != 1 {
		t.Fatalf("motion metadata = %+v", got[1])
	}
	got, _ = property.FindMotions(w.e.Ctx, w.e.Pool, chairScope(w), "roof", &from, &to, "carried", "agm")
	if len(got) != 1 || got[0].Text != "Replace the roof membrane" {
		t.Fatalf("outcome/kind filter = %+v", got)
	}
	got, _ = property.FindMotions(w.e.Ctx, w.e.Pool, chairScope(w), "", nil, nil, "", "")
	if len(got) != 25 {
		t.Fatalf("unfiltered motions = %d, want cap 25", len(got))
	}
}

func TestGetFiguresFilters(t *testing.T) {
	w := newRetrievalWorld(t)
	doc := w.doc(t, core.Org{}, 0, property.DocFinancialStatement, "2025-08-01")
	add := func(fund, metric, end string, cents int64) {
		w.exec(t, `INSERT INTO strata_figures (org_id, scheme_id, document_id, fund, metric, label, period_end, amount_cents, page_no)
			SELECT org_id, scheme_id, id, $2, $3, 'line', $4, $5, 3 FROM strata_documents WHERE id = $1`, doc, fund, metric, end, cents)
	}
	add("sinking", "balance", "2024-06-30", 9000000)
	add("sinking", "balance", "2025-06-30", 12345678)
	add("admin", "levy", "2025-06-30", 150000)
	for i := 0; i < 60; i++ {
		add("other", "other", "2019-06-30", int64(i))
	}
	from := testkit.Date("2025-01-01")
	got, err := property.GetFigures(w.e.Ctx, w.e.Pool, treasScope(w), "sinking", "balance", &from, nil)
	if err != nil || len(got) != 1 || int64(got[0].AmountCents) != 12345678 || got[0].PageNo != 3 || got[0].Title == "" {
		t.Fatalf("figures = %+v, %v", got, err)
	}
	got, _ = property.GetFigures(w.e.Ctx, w.e.Pool, treasScope(w), "sinking", "", nil, nil)
	if len(got) != 2 || !got[0].PeriodEnd.Equal(time.Date(2025, 6, 30, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("sinking figures newest first = %+v", got)
	}
	got, _ = property.GetFigures(w.e.Ctx, w.e.Pool, treasScope(w), "", "", nil, nil)
	if len(got) != 50 {
		t.Fatalf("figures = %d, want cap 50", len(got))
	}
}
