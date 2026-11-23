//go:build integration

package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"shigoto/internal/auth"
	"shigoto/internal/storage"
	"shigoto/internal/testkit"
)

// portalDo signs name@example.test in by magic link and returns a request
// function carrying its session.
func portalDo(t *testing.T, h http.Handler, e *testkit.Env, name string) func(method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	token, err := auth.RequestMagicLink(context.Background(), e.Pool, name+"@example.test")
	if err != nil || token == "" {
		t.Fatalf("magic link for %s: %v", name, err)
	}
	login := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/auth/magic/verify", strings.NewReader(fmt.Sprintf(`{"token":%q}`, token)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Shigoto-CSRF", "1")
	h.ServeHTTP(login, req)
	if login.Code != 200 {
		t.Fatalf("login %s: %d %s", name, login.Code, login.Body.String())
	}
	cookie := login.Result().Cookies()[0]
	return func(method, path, body string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.AddCookie(cookie)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Shigoto-CSRF", "1")
		h.ServeHTTP(rr, r)
		return rr
	}
}

type sbxHub struct {
	Welcome   struct{ Notes string }
	Checklist []struct {
		Action string  `json:"action"`
		DoneAt *string `json:"done_at"`
	}
	Projects []struct {
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		ToSign int    `json:"to_sign"`
	}
	Documents struct {
		ToSign int `json:"to_sign"`
		Signed int `json:"signed"`
		Shared int `json:"shared"`
	}
}

type sbxDoc struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	Kind      string `json:"kind"`
	Signature *struct {
		TypedName string `json:"typed_name"`
	} `json:"signature"`
}

func decode(t *testing.T, rr *httptest.ResponseRecorder, v any) {
	t.Helper()
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	if err := json.Unmarshal(rr.Body.Bytes(), v); err != nil {
		t.Fatal(err)
	}
}

func TestPopulateClientPortal(t *testing.T) {
	e := testkit.New(t)
	store := storage.NewDisk(t.TempDir())
	if _, err := Populate(e.Ctx, e.Pool, store, testDay); err != nil {
		t.Fatal(err)
	}
	h := e.Handler(testkit.OnlyModules("property", "timesheets", "reconciliation"), testkit.Store(store))

	client := portalDo(t, h, e, "client")
	var hub sbxHub
	decode(t, client("GET", "/api/v1/portal/sbx-property/hub", ""), &hub)
	if hub.Welcome.Notes == "" {
		t.Error("client hub has no welcome notes")
	}
	done := 0
	for _, it := range hub.Checklist {
		if it.DoneAt != nil {
			done++
		}
	}
	if len(hub.Checklist) != 5 || done != 2 {
		t.Errorf("checklist = %d items, %d done; want 5 and 2", len(hub.Checklist), done)
	}
	if hub.Documents.ToSign < 1 || hub.Documents.Signed < 1 || hub.Documents.Shared < 1 {
		t.Errorf("document counts = %+v", hub.Documents)
	}
	var reporting int64
	for _, p := range hub.Projects {
		if p.Name == "Reporting setup" {
			reporting = p.ID
			if p.ToSign != 1 {
				t.Errorf("Reporting setup to_sign = %d, want 1", p.ToSign)
			}
		}
	}
	if reporting == 0 {
		t.Fatal("Reporting setup project missing from hub")
	}

	var project struct {
		Summary      string   `json:"summary"`
		Goals        []string `json:"goals"`
		Stakeholders []string `json:"stakeholders"`
		Documents    struct {
			ToSign    []sbxDoc `json:"to_sign"`
			Signed    []sbxDoc `json:"signed"`
			Discovery []sbxDoc `json:"discovery"`
		} `json:"documents"`
	}
	decode(t, client("GET", fmt.Sprintf("/api/v1/portal/sbx-property/projects/%d", reporting), ""), &project)
	if project.Summary == "" || len(project.Goals) == 0 || len(project.Stakeholders) == 0 {
		t.Errorf("project details = %q %v %v", project.Summary, project.Goals, project.Stakeholders)
	}
	if len(project.Documents.ToSign) != 1 || len(project.Documents.Discovery) == 0 || len(project.Documents.Signed) == 0 {
		t.Fatalf("project documents = %+v", project.Documents)
	}

	var docs []sbxDoc
	decode(t, client("GET", "/api/v1/portal/sbx-property/documents", ""), &docs)
	var withdrawn bool
	var signed *sbxDoc
	for i, d := range docs {
		withdrawn = withdrawn || d.Status == "withdrawn"
		if d.Status == "signed" {
			signed = &docs[i]
		}
	}
	if !withdrawn || signed == nil || signed.Signature == nil {
		t.Fatalf("documents lack a withdrawn or signed example: %+v", docs)
	}
	cert := client("GET", fmt.Sprintf("/api/v1/portal/sbx-property/documents/%d/certificate", signed.ID), "")
	if cert.Code != 200 || !bytes.HasPrefix(cert.Body.Bytes(), []byte("%PDF-")) {
		t.Fatalf("certificate = %d", cert.Code)
	}
	pdf := client("GET", fmt.Sprintf("/api/v1/portal/sbx-property/documents/%d/file", project.Documents.ToSign[0].ID), "")
	if pdf.Code != 200 || !bytes.HasPrefix(pdf.Body.Bytes(), []byte("%PDF-")) {
		t.Fatalf("document file = %d", pdf.Code)
	}
	// The seeded SOW really is signable: its stored bytes match its hash.
	sign := client("POST", fmt.Sprintf("/api/v1/portal/sbx-property/documents/%d/sign", project.Documents.ToSign[0].ID),
		`{"typed_name":"Sandbox Tester","consent":true}`)
	if sign.Code != 200 {
		t.Fatalf("signing the seeded SOW: %d %s", sign.Code, sign.Body.String())
	}
	if rr := client("GET", "/api/v1/portal/sbx-consulting/hub", ""); rr.Code != 404 {
		t.Errorf("client reached the other org's hub: %d", rr.Code)
	}

	empty := portalDo(t, h, e, "client-empty")
	var emptyHub sbxHub
	decode(t, empty("GET", "/api/v1/portal/sbx-property/hub", ""), &emptyHub)
	if emptyHub.Welcome.Notes != "" || len(emptyHub.Checklist) != 0 || len(emptyHub.Projects) != 0 ||
		emptyHub.Documents.ToSign+emptyHub.Documents.Signed+emptyHub.Documents.Shared != 0 {
		t.Errorf("client-empty hub is not empty: %+v", emptyHub)
	}

	beta := portalDo(t, h, e, "client-beta")
	var betaHub sbxHub
	decode(t, beta("GET", "/api/v1/portal/sbx-consulting/hub", ""), &betaHub)
	if betaHub.Documents.ToSign != 1 {
		t.Errorf("client-beta to_sign = %d, want 1", betaHub.Documents.ToSign)
	}
	if rr := beta("GET", fmt.Sprintf("/api/v1/portal/sbx-consulting/documents/%d", signed.ID), ""); rr.Code != 404 {
		t.Errorf("client-beta opened another org's document: %d", rr.Code)
	}
}
