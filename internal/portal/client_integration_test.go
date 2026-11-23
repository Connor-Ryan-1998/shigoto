//go:build integration

package portal_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"shigoto/internal/auth"
	"shigoto/internal/clients"
	"shigoto/internal/core"
	"shigoto/internal/storage"
	"shigoto/internal/testkit"
)

func TestPortalSignEmailsStaff(t *testing.T) {
	e := testkit.New(t)
	sink := &mailSink{}
	store := storage.NewDisk(t.TempDir())
	h := e.Handler(testkit.Mailer(sink.mailer()), testkit.Store(store))
	org := e.Org("acme")
	owner := e.User("owner@acme.example")
	e.Member(owner, org, auth.Owner)
	c := e.Contact(org, testkit.Kind("client"), testkit.Name("Client One"))
	client := e.PortalUser("client1@example.com", org, auth.Client, c.ID)

	f, err := core.SaveFile(e.Ctx, e.Pool, store, org.ID, "msa.pdf", strings.NewReader("%PDF-1.4\n%%EOF\n"), clients.PDFPolicy)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := clients.CreateDocument(e.Ctx, e.Pool, org.ID, owner.ID, clients.NewDocument{ContactID: c.ID, Title: "MSA", Kind: "signature"}, f)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := clients.SendDocument(e.Ctx, e.Pool, org.ID, owner.ID, doc.ID); err != nil {
		t.Fatal(err)
	}

	s := e.Login(client, h)
	r := s.DoWith(http.MethodPost, fmt.Sprintf("/api/v1/portal/acme/documents/%d/sign", doc.ID),
		map[string]any{"typed_name": "Priya Nair", "consent": true}, map[string]string{"User-Agent": "SignTest/1.0"})
	if r.Code != http.StatusOK {
		t.Fatalf("sign = %d %s", r.Code, r.Body)
	}
	sink.mu.Lock()
	sent := append(sink.sent[:0:0], sink.sent...)
	sink.mu.Unlock()
	if len(sent) != 1 || sent[0].To != "owner@acme.example" || sent[0].Subject != "Client One signed MSA" {
		t.Fatalf("emails = %+v", sent)
	}
	var ua string
	if err := e.Pool.QueryRow(e.Ctx, `SELECT user_agent FROM document_signatures WHERE document_id = $1`, doc.ID).Scan(&ua); err != nil || ua != "SignTest/1.0" {
		t.Fatalf("user agent = %q, %v", ua, err)
	}
}

// Identical bytes share one files row (deduplicated by hash), so a client
// must never see the name another client's copy was uploaded under.
func TestPortalDocumentHidesSharedFileName(t *testing.T) {
	e := testkit.New(t)
	store := storage.NewDisk(t.TempDir())
	h := e.Handler(testkit.Store(store))
	org := e.Org("acme")
	owner := e.User("owner@acme.example")
	e.Member(owner, org, auth.Owner)
	a := e.Contact(org, testkit.Kind("client"), testkit.Name("Acme Corp"))
	b := e.Contact(org, testkit.Kind("client"), testkit.Name("Beta Ltd"))
	bUser := e.PortalUser("beta@example.com", org, auth.Client, b.ID)

	body := "%PDF-1.4\n% shared template\n%%EOF\n"
	fa, err := core.SaveFile(e.Ctx, e.Pool, store, org.ID, "acme-corp-confidential.pdf", strings.NewReader(body), clients.PDFPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := clients.CreateDocument(e.Ctx, e.Pool, org.ID, owner.ID, clients.NewDocument{ContactID: a.ID, Title: "NDA", Kind: "shared"}, fa); err != nil {
		t.Fatal(err)
	}
	fb, err := core.SaveFile(e.Ctx, e.Pool, store, org.ID, "beta.pdf", strings.NewReader(body), clients.PDFPolicy)
	if err != nil || fb.ID != fa.ID {
		t.Fatalf("expected the deduplicated file row: %v %d %d", err, fb.ID, fa.ID)
	}
	docB, err := clients.CreateDocument(e.Ctx, e.Pool, org.ID, owner.ID, clients.NewDocument{ContactID: b.ID, Title: "Mutual NDA", Kind: "shared"}, fb)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := clients.SendDocument(e.Ctx, e.Pool, org.ID, owner.ID, docB.ID); err != nil {
		t.Fatal(err)
	}

	s := e.Login(bUser, h)
	r := s.Do(http.MethodGet, fmt.Sprintf("/api/v1/portal/acme/documents/%d", docB.ID), nil)
	if r.Code != http.StatusOK || strings.Contains(string(r.Body), "acme-corp") {
		t.Fatalf("document = %d %s", r.Code, r.Body)
	}
	f := s.Do(http.MethodGet, fmt.Sprintf("/api/v1/portal/acme/documents/%d/file", docB.ID), nil)
	if cd := f.Header.Get("Content-Disposition"); f.Code != http.StatusOK || strings.Contains(cd, "acme") || !strings.Contains(cd, "Mutual NDA.pdf") {
		t.Fatalf("file = %d, Content-Disposition %q", f.Code, cd)
	}
}
