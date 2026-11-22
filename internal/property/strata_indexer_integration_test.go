//go:build integration

package property_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"testing"
	"time"

	"shigoto/internal/core"
	"shigoto/internal/llm"
	"shigoto/internal/llm/llmtest"
	"shigoto/internal/property"
	"shigoto/internal/storage"
	"shigoto/internal/testkit"
)

var batchRange = regexp.MustCompile(`pages (\d+)-(\d+)`)

// instructionText returns the text part of the first user message of a
// chat request body.
func instructionText(body map[string]any) string {
	msgs, _ := body["messages"].([]any)
	for _, m := range msgs {
		mm, _ := m.(map[string]any)
		switch c := mm["content"].(type) {
		case string:
			if batchRange.MatchString(c) {
				return c
			}
		case []any:
			for _, p := range c {
				pp, _ := p.(map[string]any)
				if s, _ := pp["text"].(string); batchRange.MatchString(s) {
					return s
				}
			}
		}
	}
	return ""
}

// validExtraction answers an extraction request with "Page N roof
// membrane" on every page and, for minutes, one carried motion on the
// batch's first page.
func validExtraction(body map[string]any) llmtest.ChatScript {
	m := batchRange.FindStringSubmatch(instructionText(body))
	if m == nil {
		return llmtest.ChatScript{Status: 400, Body: "no batch in instruction"}
	}
	from, _ := strconv.Atoi(m[1])
	to, _ := strconv.Atoi(m[2])
	out := map[string]any{}
	var pages []map[string]any
	for p := from; p <= to; p++ {
		pages = append(pages, map[string]any{"page_no": p, "text": fmt.Sprintf("Page %d roof membrane", p)})
	}
	out["pages"] = pages
	out["motions"] = []map[string]any{{"meeting_date": "2024-09-12", "meeting_kind": "agm", "motion_no": strconv.Itoa(from),
		"text": "Replace the roof membrane", "outcome": "carried", "page_no": from}}
	b, _ := json.Marshal(out)
	return llmtest.ChatScript{Content: string(b)}
}

type indexerWorld struct {
	e      *testkit.Env
	fake   *llmtest.Fake
	store  storage.Store
	org    core.Org
	scheme property.StrataScheme
	now    time.Time
}

func newIndexerWorld(t *testing.T) *indexerWorld {
	t.Helper()
	e := testkit.New(t)
	org := e.Org("acme")
	return &indexerWorld{
		e: e, fake: llmtest.New(t), store: storage.NewDisk(t.TempDir()), org: org,
		scheme: e.StrataScheme(org, testkit.Positions(true, false, true)),
		now:    time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC),
	}
}

func (w *indexerWorld) indexer(embedModel string) *property.StrataIndexer {
	var ec = w.fake.EmbedConfig(embedModel)
	if embedModel == "" {
		ec = nil
	}
	return &property.StrataIndexer{Pool: w.e.Pool, Store: w.store, LLM: llm.New(w.fake.LLMConfig(), ec, http.DefaultClient),
		Now: func() time.Time { return w.now }}
}

func (w *indexerWorld) doc(t *testing.T, id int64) property.StrataDocument {
	t.Helper()
	d, err := property.GetStrataDocument(w.e.Ctx, w.e.Pool, w.org.ID, w.scheme.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func (w *indexerWorld) tick(t *testing.T, x *property.StrataIndexer) {
	t.Helper()
	if err := x.Tick(w.e.Ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}
}

func TestIndexerHappyPath(t *testing.T) {
	w := newIndexerWorld(t)
	w.fake.ChatFunc(validExtraction)
	doc := w.e.StrataDocument(w.store, w.org, w.scheme.ID, testkit.DocPDF(testkit.PDFPages(12)))

	w.tick(t, w.indexer("emb-1"))

	got := w.doc(t, doc.ID)
	if got.Status != property.DocIndexed || got.ExtractModel != "fake-extract" || got.ExtractedAt == nil || got.Error != "" {
		t.Fatalf("doc = %+v", got)
	}
	chats := w.fake.Requests("/chat/completions")
	if len(chats) != 3 {
		t.Fatalf("chat calls = %d, want 3 (12 pages in batches of 5)", len(chats))
	}
	first := chats[0].JSON(t)
	if first["model"] != "fake-extract" || first["response_format"].(map[string]any)["type"] != "json_object" {
		t.Fatalf("extraction request = model %v, response_format %v", first["model"], first["response_format"])
	}
	if n := w.e.Count("strata_document_pages", "document_id = $1", doc.ID); n != 12 {
		t.Fatalf("pages = %d, want 12", n)
	}
	if n := w.e.Count("strata_motions", "document_id = $1 AND org_id = $2 AND scheme_id = $3", doc.ID, w.org.ID, w.scheme.ID); n != 3 {
		t.Fatalf("motions = %d, want 3", n)
	}
	if n := w.e.Count("strata_chunks", "document_id = $1 AND org_id = $2 AND scheme_id = $3", doc.ID, w.org.ID, w.scheme.ID); n != 12 {
		t.Fatalf("chunks = %d, want 12", n)
	}
	if n := w.e.Count("strata_chunks", "embed_model = 'emb-1' AND embedding IS NOT NULL"); n != 12 {
		t.Fatalf("embedded chunks = %d, want 12", n)
	}

	// Nothing left to do: a second tick makes no model calls.
	before := len(w.fake.Requests(""))
	w.tick(t, w.indexer("emb-1"))
	if n := len(w.fake.Requests("")); n != before {
		t.Fatalf("second tick made %d more requests", n-before)
	}
}

func TestIndexerInvalidReplyRetriesBatchOnceThenFails(t *testing.T) {
	w := newIndexerWorld(t)
	w.fake.QueueChat(llmtest.ChatScript{Content: "not json"}, llmtest.ChatScript{Content: `{"pages":[]}`})
	doc := w.e.StrataDocument(w.store, w.org, w.scheme.ID, testkit.DocPDF(testkit.PDFPages(2)))

	w.tick(t, w.indexer(""))

	got := w.doc(t, doc.ID)
	if got.Status != property.DocFailed || got.Attempts != property.StrataMaxAttempts || got.NextAttemptAt != nil {
		t.Fatalf("doc = %+v, want final failure", got)
	}
	if got.Error == "" || len(got.Error) > 300 {
		t.Fatalf("error = %q", got.Error)
	}
	if n := len(w.fake.Requests("/chat/completions")); n != 2 {
		t.Fatalf("chat calls = %d, want 2", n)
	}
	if n := w.e.Count("strata_document_pages", "document_id = $1", doc.ID); n != 0 {
		t.Fatalf("partial pages written: %d", n)
	}
}

func TestIndexerKeepsEarlierBatchesOutOfAFailedDocument(t *testing.T) {
	w := newIndexerWorld(t)
	calls := 0
	w.fake.ChatFunc(func(body map[string]any) llmtest.ChatScript {
		calls++
		if calls >= 2 {
			return llmtest.ChatScript{Status: 400, Body: "bad request"}
		}
		return validExtraction(body)
	})
	doc := w.e.StrataDocument(w.store, w.org, w.scheme.ID, testkit.DocPDF(testkit.PDFPages(7)))
	w.tick(t, w.indexer(""))
	if got := w.doc(t, doc.ID); got.Status != property.DocFailed || got.Attempts != property.StrataMaxAttempts {
		t.Fatalf("doc = %+v, want permanent failure", got)
	}
	if n := w.e.Count("strata_document_pages", "document_id = $1", doc.ID); n != 0 {
		t.Fatalf("pages from the successful batch were written: %d", n)
	}
}

func TestIndexer429Backoff(t *testing.T) {
	w := newIndexerWorld(t)
	w.fake.QueueChat(llmtest.ChatScript{Status: 429, RetryAfter: "120", Body: "slow down"})
	doc := w.e.StrataDocument(w.store, w.org, w.scheme.ID)
	x := w.indexer("")

	w.tick(t, x)
	got := w.doc(t, doc.ID)
	if got.Status != property.DocFailed || got.Attempts != 1 || got.NextAttemptAt == nil || !got.NextAttemptAt.Equal(w.now.Add(2*time.Minute)) {
		t.Fatalf("after 429: %+v (next %v)", got, got.NextAttemptAt)
	}

	w.now = w.now.Add(time.Minute)
	w.tick(t, x)
	if n := len(w.fake.Requests("/chat/completions")); n != 1 {
		t.Fatalf("retried before next_attempt_at: %d calls", n)
	}

	w.fake.ChatFunc(validExtraction)
	w.now = w.now.Add(time.Minute)
	w.tick(t, x)
	if got := w.doc(t, doc.ID); got.Status != property.DocIndexed || got.NextAttemptAt != nil {
		t.Fatalf("after retry: %+v", got)
	}
}

func TestIndexerBackoffGrowsAndStops(t *testing.T) {
	w := newIndexerWorld(t)
	w.fake.ChatFunc(func(map[string]any) llmtest.ChatScript { return llmtest.ChatScript{Status: 503, Body: "down"} })
	doc := w.e.StrataDocument(w.store, w.org, w.scheme.ID)
	x := w.indexer("")
	wantDelays := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute}
	for i, delay := range wantDelays {
		w.tick(t, x)
		got := w.doc(t, doc.ID)
		if got.Attempts != i+1 || got.NextAttemptAt == nil || !got.NextAttemptAt.Equal(w.now.Add(delay)) {
			t.Fatalf("attempt %d: %+v next %v, want +%v", i+1, got, got.NextAttemptAt, delay)
		}
		w.now = *got.NextAttemptAt
	}
	w.tick(t, x)
	if got := w.doc(t, doc.ID); got.Attempts != property.StrataMaxAttempts || got.NextAttemptAt != nil || got.Status != property.DocFailed {
		t.Fatalf("after last attempt: %+v", got)
	}
	w.now = w.now.Add(24 * time.Hour)
	w.tick(t, x)
	if n := len(w.fake.Requests("/chat/completions")); n != property.StrataMaxAttempts {
		t.Fatalf("chat calls = %d, want %d", n, property.StrataMaxAttempts)
	}
}

func TestIndexerRecover(t *testing.T) {
	w := newIndexerWorld(t)
	doc := w.e.StrataDocument(w.store, w.org, w.scheme.ID)
	if _, err := w.e.Pool.Exec(w.e.Ctx, `UPDATE strata_documents SET status = 'extracting' WHERE id = $1`, doc.ID); err != nil {
		t.Fatal(err)
	}
	if err := w.indexer("").Recover(w.e.Ctx); err != nil {
		t.Fatal(err)
	}
	if got := w.doc(t, doc.ID); got.Status != property.DocPending {
		t.Fatalf("status = %s, want pending", got.Status)
	}
}

func TestIndexerConcurrentTicksProcessOnce(t *testing.T) {
	w := newIndexerWorld(t)
	w.fake.ChatFunc(func(body map[string]any) llmtest.ChatScript {
		time.Sleep(300 * time.Millisecond)
		return validExtraction(body)
	})
	w.e.StrataDocument(w.store, w.org, w.scheme.ID, testkit.DocPDF(testkit.PDFPages(3)))
	x1, x2 := w.indexer(""), w.indexer("")
	var wg sync.WaitGroup
	for _, x := range []*property.StrataIndexer{x1, x2} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := x.Tick(w.e.Ctx); err != nil {
				t.Errorf("Tick: %v", err)
			}
		}()
	}
	wg.Wait()
	if n := len(w.fake.Requests("/chat/completions")); n != 1 {
		t.Fatalf("chat calls = %d, want 1", n)
	}
}

func TestIndexerReembedOnModelChange(t *testing.T) {
	w := newIndexerWorld(t)
	w.fake.ChatFunc(validExtraction)
	w.e.StrataDocument(w.store, w.org, w.scheme.ID, testkit.DocPDF(testkit.PDFPages(2)))
	w.tick(t, w.indexer("m1"))
	chats := len(w.fake.Requests("/chat/completions"))

	w.tick(t, w.indexer("m2"))
	if n := w.e.Count("strata_chunks", "embed_model = 'm2'"); n != 2 {
		t.Fatalf("chunks re-embedded with m2 = %d, want 2", n)
	}
	if n := len(w.fake.Requests("/chat/completions")); n != chats {
		t.Fatalf("re-embed re-read the document (%d chat calls, want %d)", n, chats)
	}
}

func TestIndexerWithoutEmbeddings(t *testing.T) {
	w := newIndexerWorld(t)
	w.fake.ChatFunc(validExtraction)
	doc := w.e.StrataDocument(w.store, w.org, w.scheme.ID)
	w.tick(t, w.indexer(""))
	if got := w.doc(t, doc.ID); got.Status != property.DocIndexed {
		t.Fatalf("status = %s", got.Status)
	}
	if n := w.e.Count("strata_chunks", "embedding IS NULL"); n != 1 {
		t.Fatalf("unembedded chunks = %d, want 1", n)
	}
	if n := len(w.fake.Requests("/embeddings")); n != 0 {
		t.Fatalf("embedding calls = %d, want 0", n)
	}
}

func TestIndexerEmbeddingFailureLeavesDocumentIndexed(t *testing.T) {
	w := newIndexerWorld(t)
	w.fake.ChatFunc(validExtraction)
	w.fake.EmbedError(&llmtest.ChatScript{Status: 429, Body: "quota"})
	doc := w.e.StrataDocument(w.store, w.org, w.scheme.ID)
	w.tick(t, w.indexer("m1"))
	if got := w.doc(t, doc.ID); got.Status != property.DocIndexed {
		t.Fatalf("status = %s, want indexed", got.Status)
	}
	w.fake.EmbedError(nil)
	w.tick(t, w.indexer("m1"))
	if n := w.e.Count("strata_chunks", "embed_model = 'm1'"); n != 1 {
		t.Fatalf("chunks embedded after recovery = %d, want 1", n)
	}
}

func TestIndexerReindexDuringExtractionIsNotOverwritten(t *testing.T) {
	w := newIndexerWorld(t)
	doc := w.e.StrataDocument(w.store, w.org, w.scheme.ID)
	w.fake.ChatFunc(func(body map[string]any) llmtest.ChatScript {
		// The user reindexes while the model is reading.
		if err := property.ReindexStrataDocument(w.e.Ctx, w.e.Pool, w.org.ID, w.scheme.ID, doc.ID); err != nil {
			t.Error(err)
		}
		return validExtraction(body)
	})
	w.tick(t, w.indexer(""))
	if got := w.doc(t, doc.ID); got.Status != property.DocPending {
		t.Fatalf("status = %s, want pending (reindexed mid-extraction)", got.Status)
	}
	if n := w.e.Count("strata_document_pages", "document_id = $1", doc.ID); n != 0 {
		t.Fatalf("stale extraction written: %d pages", n)
	}
}
