//go:build integration

package clients_test

import (
	"bytes"
	"errors"
	"fmt"
	"testing"
	"time"

	"shigoto/internal/auth"
	"shigoto/internal/clients"
	"shigoto/internal/core"
	"shigoto/internal/storage"
	"shigoto/internal/testkit"
)

// docEnv is an org with an owner, a client contact and a disk store.
type docEnv struct {
	e      *testkit.Env
	org    core.Org
	owner  testkit.User
	client core.Contact
	store  storage.Store
}

func newDocEnv(t *testing.T) docEnv {
	e := testkit.New(t)
	org := e.Org("acme")
	owner := e.User("owner@example.com")
	e.Member(owner, org, auth.Owner)
	return docEnv{e: e, org: org, owner: owner,
		client: e.Contact(org, testkit.Kind("client"), testkit.Name("Client One")),
		store:  storage.NewDisk(t.TempDir())}
}

var pdfSeq int

// pdf saves a small unique PDF file for the org.
func (d docEnv) pdf(t *testing.T) core.File {
	t.Helper()
	pdfSeq++
	body := fmt.Sprintf("%%PDF-1.4\n%% test document %d\n%%%%EOF\n", pdfSeq)
	f, err := core.SaveFile(d.e.Ctx, d.e.Pool, d.store, d.org.ID, "doc.pdf", bytes.NewReader([]byte(body)), clients.PDFPolicy)
	if err != nil {
		t.Fatalf("SaveFile: %v", err)
	}
	return f
}

func (d docEnv) draft(t *testing.T, kind string, projectID *int64) clients.Document {
	t.Helper()
	doc, err := clients.CreateDocument(d.e.Ctx, d.e.Pool, d.org.ID, d.owner.ID, clients.NewDocument{
		ContactID: d.client.ID, ProjectID: projectID, Title: "MSA", Kind: kind, Category: "general",
	}, d.pdf(t))
	if err != nil {
		t.Fatalf("CreateDocument: %v", err)
	}
	return doc
}

func (d docEnv) portalLogin(t *testing.T) testkit.User {
	return d.e.PortalUser("client1@example.com", d.org, auth.Client, d.client.ID)
}

func TestCreateDraftCopiesHash(t *testing.T) {
	d := newDocEnv(t)
	f := d.pdf(t)
	doc, err := clients.CreateDocument(d.e.Ctx, d.e.Pool, d.org.ID, d.owner.ID, clients.NewDocument{
		ContactID: d.client.ID, Title: "MSA", Kind: "signature", Category: "general",
	}, f)
	if err != nil {
		t.Fatalf("CreateDocument: %v", err)
	}
	if doc.Status != "draft" || !bytes.Equal(doc.FileSHA256, f.SHA256) || doc.FileID != f.ID {
		t.Fatalf("doc = %+v", doc)
	}
	for _, nd := range []clients.NewDocument{
		{ContactID: d.client.ID, Title: "  ", Kind: "signature", Category: "general"},
		{ContactID: d.client.ID, Title: "X", Kind: "contract", Category: "general"},
		{ContactID: d.client.ID, Title: "X", Kind: "shared", Category: "misc"},
	} {
		if _, err := clients.CreateDocument(d.e.Ctx, d.e.Pool, d.org.ID, d.owner.ID, nd, f); !errors.Is(err, core.ErrInvalidInput) {
			t.Errorf("CreateDocument(%+v) err = %v, want ErrInvalidInput", nd, err)
		}
	}
	tenant := d.e.Contact(d.org, testkit.Kind("tenant"))
	if _, err := clients.CreateDocument(d.e.Ctx, d.e.Pool, d.org.ID, d.owner.ID, clients.NewDocument{
		ContactID: tenant.ID, Title: "X", Kind: "shared", Category: "general"}, f); !errors.Is(err, clients.ErrNotClient) {
		t.Errorf("tenant contact err = %v, want ErrNotClient", err)
	}
}

func TestUploadRejectsOtherClientsProject(t *testing.T) {
	d := newDocEnv(t)
	other := d.e.Contact(d.org, testkit.Kind("client"), testkit.Name("Other"))
	proj := d.e.Project(d.org, other.ID)
	_, err := clients.CreateDocument(d.e.Ctx, d.e.Pool, d.org.ID, d.owner.ID, clients.NewDocument{
		ContactID: d.client.ID, ProjectID: &proj.ID, Title: "SOW", Kind: "signature", Category: "general",
	}, d.pdf(t))
	if !errors.Is(err, clients.ErrProjectOtherClient) || err.Error() != "project belongs to another client" {
		t.Fatalf("err = %v, want ErrProjectOtherClient", err)
	}
	own := d.e.Project(d.org, d.client.ID, testkit.ProjectName("Own"))
	doc := d.draft(t, "signature", &own.ID)
	if doc.ProjectID == nil || *doc.ProjectID != own.ID {
		t.Fatalf("project id = %v, want %d", doc.ProjectID, own.ID)
	}
}

func TestSendRequiresPortalLogin(t *testing.T) {
	d := newDocEnv(t)
	doc := d.draft(t, "signature", nil)
	_, err := clients.SendDocument(d.e.Ctx, d.e.Pool, d.org.ID, d.owner.ID, doc.ID)
	if !errors.Is(err, clients.ErrNoPortalLogin) || err.Error() != "Invite this client to the portal first" {
		t.Fatalf("err = %v, want ErrNoPortalLogin", err)
	}
	d.portalLogin(t)
	sent, err := clients.SendDocument(d.e.Ctx, d.e.Pool, d.org.ID, d.owner.ID, doc.ID)
	if err != nil || sent.Status != "sent" || sent.SentAt == nil {
		t.Fatalf("sent = %+v, %v", sent, err)
	}
	if _, err := clients.SendDocument(d.e.Ctx, d.e.Pool, d.org.ID, d.owner.ID, doc.ID); !errors.Is(err, clients.ErrBadState) {
		t.Fatalf("resend err = %v, want ErrBadState", err)
	}
	email, ok, err := clients.PortalLoginEmail(d.e.Ctx, d.e.Pool, d.org.ID, d.client.ID)
	if err != nil || !ok || email != "client1@example.com" {
		t.Fatalf("PortalLoginEmail = %q, %v, %v", email, ok, err)
	}
}

func TestSharedSendNeedsNoLogin(t *testing.T) {
	d := newDocEnv(t)
	doc := d.draft(t, "shared", nil)
	sent, err := clients.SendDocument(d.e.Ctx, d.e.Pool, d.org.ID, d.owner.ID, doc.ID)
	if err != nil || sent.Status != "sent" {
		t.Fatalf("sent = %+v, %v", sent, err)
	}
}

func TestWithdrawOnlySent(t *testing.T) {
	d := newDocEnv(t)
	doc := d.draft(t, "shared", nil)
	if _, err := clients.WithdrawDocument(d.e.Ctx, d.e.Pool, d.org.ID, d.owner.ID, doc.ID); !errors.Is(err, clients.ErrBadState) {
		t.Fatalf("withdraw draft err = %v, want ErrBadState", err)
	}
	if _, err := clients.SendDocument(d.e.Ctx, d.e.Pool, d.org.ID, d.owner.ID, doc.ID); err != nil {
		t.Fatal(err)
	}
	w, err := clients.WithdrawDocument(d.e.Ctx, d.e.Pool, d.org.ID, d.owner.ID, doc.ID)
	if err != nil || w.Status != "withdrawn" {
		t.Fatalf("withdrawn = %+v, %v", w, err)
	}
}

func TestReplaceSupersedes(t *testing.T) {
	d := newDocEnv(t)
	d.portalLogin(t)
	doc := d.draft(t, "signature", nil)
	if _, err := clients.ReplaceDocument(d.e.Ctx, d.e.Pool, d.org.ID, d.owner.ID, doc.ID, d.pdf(t)); !errors.Is(err, clients.ErrBadState) {
		t.Fatalf("replace draft err = %v, want ErrBadState", err)
	}
	if _, err := clients.SendDocument(d.e.Ctx, d.e.Pool, d.org.ID, d.owner.ID, doc.ID); err != nil {
		t.Fatal(err)
	}
	f2 := d.pdf(t)
	next, err := clients.ReplaceDocument(d.e.Ctx, d.e.Pool, d.org.ID, d.owner.ID, doc.ID, f2)
	if err != nil {
		t.Fatalf("ReplaceDocument: %v", err)
	}
	if next.Status != "draft" || next.SupersedesID == nil || *next.SupersedesID != doc.ID ||
		next.Title != doc.Title || next.Kind != doc.Kind || !bytes.Equal(next.FileSHA256, f2.SHA256) {
		t.Fatalf("replacement = %+v", next)
	}
	// The original stays live until the replacement is sent.
	old, err := clients.GetDocument(d.e.Ctx, d.e.Pool, d.org.ID, doc.ID)
	if err != nil || old.Status != "sent" {
		t.Fatalf("old before sending replacement = %+v, %v", old, err)
	}
	if _, err := clients.SendDocument(d.e.Ctx, d.e.Pool, d.org.ID, d.owner.ID, next.ID); err != nil {
		t.Fatalf("send replacement: %v", err)
	}
	old, err = clients.GetDocument(d.e.Ctx, d.e.Pool, d.org.ID, doc.ID)
	if err != nil || old.Status != "superseded" {
		t.Fatalf("old after sending replacement = %+v, %v", old, err)
	}
}

func TestDeleteReplacementDraftKeepsOriginal(t *testing.T) {
	d := newDocEnv(t)
	d.portalLogin(t)
	doc := d.draft(t, "signature", nil)
	if _, err := clients.SendDocument(d.e.Ctx, d.e.Pool, d.org.ID, d.owner.ID, doc.ID); err != nil {
		t.Fatal(err)
	}
	next, err := clients.ReplaceDocument(d.e.Ctx, d.e.Pool, d.org.ID, d.owner.ID, doc.ID, d.pdf(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := clients.DeleteDraft(d.e.Ctx, d.e.Pool, d.store, d.org.ID, d.owner.ID, next.ID); err != nil {
		t.Fatal(err)
	}
	old, _ := clients.GetDocument(d.e.Ctx, d.e.Pool, d.org.ID, doc.ID)
	if old.Status != "sent" {
		t.Fatalf("original after deleting replacement draft = %q, want sent", old.Status)
	}
}

func TestSendReplacementRefusedAfterOriginalSigned(t *testing.T) {
	d := newDocEnv(t)
	u := d.portalLogin(t)
	doc := d.draft(t, "signature", nil)
	if _, err := clients.SendDocument(d.e.Ctx, d.e.Pool, d.org.ID, d.owner.ID, doc.ID); err != nil {
		t.Fatal(err)
	}
	next, err := clients.ReplaceDocument(d.e.Ctx, d.e.Pool, d.org.ID, d.owner.ID, doc.ID, d.pdf(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := clients.Sign(d.e.Ctx, d.e.Pool, d.store, time.UTC, d.org.ID, d.client.ID, doc.ID, clients.SignInput{
		UserID: u.ID, Email: "client1@example.com", TypedName: "Priya", IP: "192.0.2.1", Consent: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := clients.SendDocument(d.e.Ctx, d.e.Pool, d.org.ID, d.owner.ID, next.ID); !errors.Is(err, clients.ErrSigned) {
		t.Fatalf("send replacement after original signed err = %v, want ErrSigned", err)
	}
}

func TestDeleteOnlyDraft(t *testing.T) {
	d := newDocEnv(t)
	sent := d.draft(t, "shared", nil)
	if _, err := clients.SendDocument(d.e.Ctx, d.e.Pool, d.org.ID, d.owner.ID, sent.ID); err != nil {
		t.Fatal(err)
	}
	if err := clients.DeleteDraft(d.e.Ctx, d.e.Pool, d.store, d.org.ID, d.owner.ID, sent.ID); !errors.Is(err, clients.ErrBadState) {
		t.Fatalf("delete sent err = %v, want ErrBadState", err)
	}
	draft := d.draft(t, "shared", nil)
	if err := clients.DeleteDraft(d.e.Ctx, d.e.Pool, d.store, d.org.ID, d.owner.ID, draft.ID); err != nil {
		t.Fatalf("DeleteDraft: %v", err)
	}
	if n := d.e.Count("client_documents", "id = $1", draft.ID); n != 0 {
		t.Fatalf("document rows = %d, want 0", n)
	}
	if n := d.e.Count("files", "id = $1", draft.FileID); n != 0 {
		t.Fatalf("file rows = %d, want 0 (garbage collected)", n)
	}
}

func TestListDocumentsScopedToContact(t *testing.T) {
	d := newDocEnv(t)
	other := d.e.Contact(d.org, testkit.Kind("client"), testkit.Name("Other"))
	d.draft(t, "shared", nil)
	if _, err := clients.CreateDocument(d.e.Ctx, d.e.Pool, d.org.ID, d.owner.ID, clients.NewDocument{
		ContactID: other.ID, Title: "Theirs", Kind: "shared", Category: "general"}, d.pdf(t)); err != nil {
		t.Fatal(err)
	}
	docs, err := clients.ListDocuments(d.e.Ctx, d.e.Pool, d.org.ID, d.client.ID, nil)
	if err != nil || len(docs) != 1 || docs[0].ContactID != d.client.ID {
		t.Fatalf("docs = %+v, %v", docs, err)
	}
}
