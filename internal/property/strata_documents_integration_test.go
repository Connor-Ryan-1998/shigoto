//go:build integration

package property_test

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"shigoto/internal/core"
	"shigoto/internal/property"
	"shigoto/internal/storage"
	"shigoto/internal/testkit"
)

var agmDate = time.Date(2024, 9, 12, 0, 0, 0, 0, time.UTC)

func savePDF(t *testing.T, e *testkit.Env, store storage.Store, org core.Org, pdf []byte) core.File {
	t.Helper()
	f, err := core.SaveFile(e.Ctx, e.Pool, store, org.ID, "doc.pdf", bytes.NewReader(pdf), core.DocumentPolicy)
	if err != nil {
		t.Fatalf("saving pdf: %v", err)
	}
	return f
}

func TestStrataDocumentCRUD(t *testing.T) {
	e := testkit.New(t)
	store := storage.NewDisk(t.TempDir())
	acme, other := e.Org("acme"), e.Org("other")
	scheme := e.StrataScheme(acme, testkit.Positions(true, false, false))
	second := e.StrataScheme(acme, testkit.SchemeName("Second CTS"))
	f := savePDF(t, e, store, acme, testkit.PDFPages(3))

	doc, err := property.CreateStrataDocument(e.Ctx, e.Pool, acme.ID, scheme.ID, property.StrataDocument{
		FileID: f.ID, DocType: property.DocMinutes, Title: "  AGM 2024 ", DocumentDate: agmDate, PageCount: 3,
	})
	if err != nil {
		t.Fatalf("CreateStrataDocument: %v", err)
	}
	if doc.Status != property.DocPending || doc.Attempts != 0 || doc.Title != "AGM 2024" || doc.PageCount != 3 || doc.SchemeID != scheme.ID || doc.OrgID != acme.ID {
		t.Fatalf("created = %+v", doc)
	}
	if !doc.DocumentDate.Equal(agmDate) {
		t.Fatalf("document date = %v", doc.DocumentDate)
	}

	list, err := property.ListStrataDocuments(e.Ctx, e.Pool, acme.ID, scheme.ID, "")
	if err != nil || len(list) != 1 || list[0].ID != doc.ID {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if list, _ := property.ListStrataDocuments(e.Ctx, e.Pool, acme.ID, second.ID, ""); len(list) != 0 {
		t.Fatalf("other scheme lists %d documents", len(list))
	}

	if _, err := property.GetStrataDocument(e.Ctx, e.Pool, other.ID, scheme.ID, doc.ID); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("get from other org: %v", err)
	}
	if _, err := property.GetStrataDocument(e.Ctx, e.Pool, acme.ID, second.ID, doc.ID); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("get via other scheme: %v", err)
	}
	if got, err := property.GetStrataDocument(e.Ctx, e.Pool, acme.ID, scheme.ID, doc.ID); err != nil || got.ID != doc.ID {
		t.Fatalf("get = %+v, %v", got, err)
	}
}

func TestStrataDocumentRejectsForeignScheme(t *testing.T) {
	e := testkit.New(t)
	store := storage.NewDisk(t.TempDir())
	acme, other := e.Org("acme"), e.Org("other")
	foreign := e.StrataScheme(other)
	f := savePDF(t, e, store, acme, testkit.PDFPages(1))
	_, err := property.CreateStrataDocument(e.Ctx, e.Pool, acme.ID, foreign.ID, property.StrataDocument{
		FileID: f.ID, DocType: property.DocMinutes, Title: "AGM", DocumentDate: agmDate, PageCount: 1,
	})
	if !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}

	// A file of another org may not be attached either.
	mine := e.StrataScheme(acme)
	theirs := savePDF(t, e, store, other, testkit.PDFPages(2))
	_, err = property.CreateStrataDocument(e.Ctx, e.Pool, acme.ID, mine.ID, property.StrataDocument{
		FileID: theirs.ID, DocType: property.DocMinutes, Title: "AGM", DocumentDate: agmDate, PageCount: 2,
	})
	if !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("foreign file err = %v, want ErrNotFound", err)
	}
}

func TestStrataDocumentValidation(t *testing.T) {
	e := testkit.New(t)
	store := storage.NewDisk(t.TempDir())
	acme := e.Org("acme")
	scheme := e.StrataScheme(acme)
	f := savePDF(t, e, store, acme, testkit.PDFPages(1))
	base := property.StrataDocument{FileID: f.ID, DocType: property.DocBudget, Title: "Budget", DocumentDate: agmDate, PageCount: 1}

	for name, mutate := range map[string]func(*property.StrataDocument){
		"blank title":  func(d *property.StrataDocument) { d.Title = "   " },
		"long title":   func(d *property.StrataDocument) { d.Title = string(bytes.Repeat([]byte("x"), 201)) },
		"zero date":    func(d *property.StrataDocument) { d.DocumentDate = time.Time{} },
		"bad type":     func(d *property.StrataDocument) { d.DocType = "nonsense" },
		"no pages":     func(d *property.StrataDocument) { d.PageCount = 0 },
		"over 300 pgs": func(d *property.StrataDocument) { d.PageCount = 301 },
	} {
		d := base
		mutate(&d)
		if _, err := property.CreateStrataDocument(e.Ctx, e.Pool, acme.ID, scheme.ID, d); !errors.Is(err, core.ErrInvalidInput) {
			t.Errorf("%s: err = %v, want ErrInvalidInput", name, err)
		}
	}
}

func TestDeleteStrataDocumentCascadesAndGCs(t *testing.T) {
	e := testkit.New(t)
	store := storage.NewDisk(t.TempDir())
	acme := e.Org("acme")
	scheme := e.StrataScheme(acme, testkit.Positions(true, false, true))
	doc := e.StrataDocument(store, acme, scheme.ID)

	insertDerived(t, e, acme.ID, scheme.ID, doc.ID)
	var sha []byte
	if err := e.Pool.QueryRow(e.Ctx, `SELECT sha256 FROM files WHERE id = $1`, doc.FileID).Scan(&sha); err != nil {
		t.Fatal(err)
	}
	if err := property.DeleteStrataDocument(e.Ctx, e.Pool, store, acme.ID, scheme.ID, doc.ID); err != nil {
		t.Fatalf("DeleteStrataDocument: %v", err)
	}
	for _, table := range []string{"strata_documents", "strata_document_pages", "strata_chunks", "strata_motions", "strata_figures"} {
		if n := e.Count(table, "true"); n != 0 {
			t.Errorf("%s has %d rows after delete", table, n)
		}
	}
	if n := e.Count("files", "id = $1", doc.FileID); n != 0 {
		t.Fatalf("file row survived delete")
	}
	if _, err := store.Open(e.Ctx, storage.Key{OrgID: acme.ID, SHA256: sha}); err == nil {
		t.Fatal("blob survived delete")
	}

	if err := property.DeleteStrataDocument(e.Ctx, e.Pool, store, acme.ID, scheme.ID, doc.ID); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("second delete err = %v, want ErrNotFound", err)
	}
}

func TestDeleteStrataDocumentKeepsSharedFile(t *testing.T) {
	e := testkit.New(t)
	store := storage.NewDisk(t.TempDir())
	acme := e.Org("acme")
	a := e.StrataScheme(acme, testkit.SchemeName("A"))
	b := e.StrataScheme(acme, testkit.SchemeName("B"))
	pdf := testkit.PDFPages(2)
	docA := e.StrataDocument(store, acme, a.ID, testkit.DocPDF(pdf))
	docB := e.StrataDocument(store, acme, b.ID, testkit.DocPDF(pdf))
	if docA.FileID != docB.FileID {
		t.Fatal("identical uploads should share one file row")
	}
	if err := property.DeleteStrataDocument(e.Ctx, e.Pool, store, acme.ID, a.ID, docA.ID); err != nil {
		t.Fatal(err)
	}
	if n := e.Count("files", "id = $1", docB.FileID); n != 1 {
		t.Fatal("shared file deleted while still referenced")
	}
}

func TestReindexResets(t *testing.T) {
	e := testkit.New(t)
	store := storage.NewDisk(t.TempDir())
	acme := e.Org("acme")
	scheme := e.StrataScheme(acme)
	doc := e.StrataDocument(store, acme, scheme.ID)
	if _, err := e.Pool.Exec(e.Ctx, `UPDATE strata_documents SET status = 'failed', attempts = 6, error = 'boom', next_attempt_at = now() WHERE id = $1`, doc.ID); err != nil {
		t.Fatal(err)
	}
	if err := property.ReindexStrataDocument(e.Ctx, e.Pool, acme.ID, scheme.ID, doc.ID); err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	got, err := property.GetStrataDocument(e.Ctx, e.Pool, acme.ID, scheme.ID, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != property.DocPending || got.Attempts != 0 || got.Error != "" || got.NextAttemptAt != nil {
		t.Fatalf("after reindex = %+v", got)
	}
	if err := property.ReindexStrataDocument(e.Ctx, e.Pool, e.Org("other").ID, scheme.ID, doc.ID); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("foreign reindex err = %v", err)
	}
}

func TestListStrataDocumentsCountsEmbeddedChunks(t *testing.T) {
	e := testkit.New(t)
	store := storage.NewDisk(t.TempDir())
	acme := e.Org("acme")
	scheme := e.StrataScheme(acme)
	doc := e.StrataDocument(store, acme, scheme.ID)
	insertDerived(t, e, acme.ID, scheme.ID, doc.ID) // two chunks, one embedded as "m1"

	list, err := property.ListStrataDocuments(e.Ctx, e.Pool, acme.ID, scheme.ID, "m1")
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %v, %v", list, err)
	}
	if list[0].ChunksTotal != 2 || list[0].ChunksEmbedded != 1 {
		t.Fatalf("chunks = %d/%d, want 1/2", list[0].ChunksEmbedded, list[0].ChunksTotal)
	}
	list, _ = property.ListStrataDocuments(e.Ctx, e.Pool, acme.ID, scheme.ID, "m2")
	if list[0].ChunksEmbedded != 0 {
		t.Fatalf("chunks embedded with another model counted: %d", list[0].ChunksEmbedded)
	}
}

// insertDerived adds one page, two chunks (one embedded as "m1"), one
// motion and one figure to document docID.
func insertDerived(t *testing.T, e *testkit.Env, orgID, schemeID, docID int64) {
	t.Helper()
	vec := "[" + string(bytes.Repeat([]byte("0.1,"), 1535)) + "0.1]"
	if _, err := e.Pool.Exec(e.Ctx, `INSERT INTO strata_document_pages (document_id, page_no, text) VALUES ($1, 1, 'roof')`, docID); err != nil {
		t.Fatalf("inserting page: %v", err)
	}
	for _, sql := range []string{
		`INSERT INTO strata_chunks (org_id, scheme_id, document_id, page_from, page_to, text, embedding, embed_model) VALUES ($1, $2, $3, 1, 1, 'roof', '` + vec + `', 'm1')`,
		`INSERT INTO strata_chunks (org_id, scheme_id, document_id, page_from, page_to, text) VALUES ($1, $2, $3, 1, 1, 'lift')`,
		`INSERT INTO strata_motions (org_id, scheme_id, document_id, meeting_date, meeting_kind, text, outcome, page_no) VALUES ($1, $2, $3, '2024-09-12', 'agm', 'Replace roof', 'carried', 1)`,
		`INSERT INTO strata_figures (org_id, scheme_id, document_id, fund, metric, label, period_end, amount_cents, page_no) VALUES ($1, $2, $3, 'sinking', 'balance', 'Closing balance', '2025-06-30', 12345600, 1)`,
	} {
		if _, err := e.Pool.Exec(e.Ctx, sql, orgID, schemeID, docID); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
}
