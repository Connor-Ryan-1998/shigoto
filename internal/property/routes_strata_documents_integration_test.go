//go:build integration

package property_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"shigoto/internal/auth"
	"shigoto/internal/core"
	"shigoto/internal/property"
	"shigoto/internal/storage"
	"shigoto/internal/testkit"
)

// strataUploadWorld is an org holding the chairperson position on a scheme,
// an owner and a bookkeeper session over a handler sharing one store.
type strataUploadWorld struct {
	e      *testkit.Env
	org    core.Org
	scheme property.StrataScheme
	owner  testkit.Session
	keeper testkit.Session
	kicks  *int
}

func newStrataUploadWorld(t *testing.T) strataUploadWorld {
	t.Helper()
	e := testkit.New(t)
	kicks := 0
	h := e.Handler(testkit.Store(storage.NewDisk(t.TempDir())), testkit.StrataKick(func() { kicks++ }))
	org, owner := e.OwnerSession("acme", h)
	keeperUser := e.User("keeper@example.com")
	e.Member(keeperUser, org, auth.Bookkeeper)
	return strataUploadWorld{
		e: e, org: org, owner: owner, keeper: e.Login(keeperUser, h), kicks: &kicks,
		scheme: e.StrataScheme(org, testkit.Positions(true, false, false)),
	}
}

func uploadPath(schemeID int64, query string) string {
	return fmt.Sprintf("/api/v1/orgs/acme/strata-schemes/%d/documents?%s", schemeID, query)
}

const validMeta = "doc_type=minutes&title=AGM%202024&document_date=2024-09-12"

func TestUploadStrataDocument(t *testing.T) {
	w := newStrataUploadWorld(t)
	resp := w.owner.Upload(uploadPath(w.scheme.ID, validMeta), "agm.pdf", testkit.PDFPages(3))
	if resp.Code != http.StatusCreated {
		t.Fatalf("status = %d, body %s", resp.Code, resp.Body)
	}
	var got struct {
		ID           int64  `json:"id"`
		DocType      string `json:"doc_type"`
		Title        string `json:"title"`
		DocumentDate string `json:"document_date"`
		Status       string `json:"status"`
		PageCount    int    `json:"page_count"`
	}
	resp.JSON(&got)
	if got.DocType != "minutes" || got.Title != "AGM 2024" || got.DocumentDate != "2024-09-12" || got.Status != "pending" || got.PageCount != 3 {
		t.Fatalf("created = %+v", got)
	}
	if n := w.e.Count("audit_log", "entity = 'strata_document' AND entity_id = $1 AND action = 'create'", got.ID); n != 1 {
		t.Fatalf("audit rows = %d, want 1", n)
	}
	if *w.kicks != 1 {
		t.Fatalf("indexer kicks = %d, want 1", *w.kicks)
	}

	// The same file again on the same scheme is rejected.
	resp = w.owner.Upload(uploadPath(w.scheme.ID, validMeta), "agm.pdf", testkit.PDFPages(3))
	if resp.Code != http.StatusUnprocessableEntity || !strings.Contains(string(resp.Body), "already") {
		t.Fatalf("duplicate upload: %d %s", resp.Code, resp.Body)
	}
	if n := w.e.Count("strata_documents", "true"); n != 1 {
		t.Fatalf("documents = %d, want 1", n)
	}
}

func TestUploadStrataDocumentRejectsBadInput(t *testing.T) {
	w := newStrataUploadWorld(t)
	for _, tc := range []struct {
		name  string
		query string
		data  []byte
		want  string
	}{
		{"png", validMeta, testkit.PNG, "unsupported file type"},
		{"unknown type", "doc_type=memo&title=x&document_date=2024-09-12", testkit.PDFPages(1), "doc_type"},
		{"missing date", "doc_type=minutes&title=x", testkit.PDFPages(1), "document_date"},
		{"bad date", "doc_type=minutes&title=x&document_date=12/09/2024", testkit.PDFPages(1), "document_date"},
		{"blank title", "doc_type=minutes&title=%20&document_date=2024-09-12", testkit.PDFPages(1), "title"},
		{"too many pages", validMeta, testkit.PDFPages(301), "PDF has more than 300 pages"},
		{"encrypted", validMeta, testkit.EncryptedPDF(t, testkit.PDFPages(1)), "password"},
		{"damaged", validMeta, append([]byte("%PDF-1.4\n"), []byte("garbage")...), "could not be read"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := w.owner.Upload(uploadPath(w.scheme.ID, tc.query), "x.pdf", tc.data)
			if resp.Code != http.StatusUnprocessableEntity || !strings.Contains(string(resp.Body), tc.want) {
				t.Fatalf("status %d body %s, want 422 mentioning %q", resp.Code, resp.Body, tc.want)
			}
			if n := w.e.Count("strata_documents", "true"); n != 0 {
				t.Fatalf("documents = %d, want 0", n)
			}
			if n := w.e.Count("files", "true"); n != 0 {
				t.Fatalf("files = %d, want 0 (rejected upload left a file)", n)
			}
		})
	}
	if *w.kicks != 0 {
		t.Fatalf("indexer kicked %d times for rejected uploads", *w.kicks)
	}
}

func TestUploadStrataDocumentPermissions(t *testing.T) {
	w := newStrataUploadWorld(t)
	noOfficer := w.e.StrataScheme(w.org, testkit.SchemeName("Plain"), testkit.Positions(false, true, false))
	foreign := w.e.StrataScheme(w.e.Org("other"), testkit.Positions(true, true, true))

	for _, tc := range []struct {
		name   string
		s      testkit.Session
		scheme int64
		code   int
	}{
		{"bookkeeper", w.keeper, w.scheme.ID, http.StatusForbidden},
		{"no officer position", w.owner, noOfficer.ID, http.StatusForbidden},
		{"foreign scheme", w.owner, foreign.ID, http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := tc.s.Upload(uploadPath(tc.scheme, validMeta), "x.pdf", testkit.PDFPages(1))
			if resp.Code != tc.code {
				t.Fatalf("status = %d, want %d (%s)", resp.Code, tc.code, resp.Body)
			}
		})
	}
	if n := w.e.Count("files", "true"); n != 0 {
		t.Fatalf("files = %d, want 0", n)
	}
}
