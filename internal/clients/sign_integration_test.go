//go:build integration

package clients_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"shigoto/internal/clients"
	"shigoto/internal/core"
	"shigoto/internal/storage"
)

func (d docEnv) sent(t *testing.T, kind string) clients.Document {
	t.Helper()
	doc := d.draft(t, kind, nil)
	if _, err := clients.SendDocument(d.e.Ctx, d.e.Pool, d.org.ID, d.owner.ID, doc.ID); err != nil {
		t.Fatalf("SendDocument: %v", err)
	}
	return doc
}

func (d docEnv) sign(doc clients.Document, contactID int64, in clients.SignInput) (clients.Document, error) {
	return clients.Sign(d.e.Ctx, d.e.Pool, d.store, time.UTC, d.org.ID, contactID, doc.ID, in)
}

func (d docEnv) input(userID int64) clients.SignInput {
	return clients.SignInput{UserID: userID, Email: "client1@example.com", TypedName: "  Priya Nair ",
		IP: "203.0.113.7", UserAgent: "Test/1.0", Consent: true}
}

func TestSignHappyPath(t *testing.T) {
	d := newDocEnv(t)
	u := d.portalLogin(t)
	doc := d.sent(t, "signature")
	signed, err := d.sign(doc, d.client.ID, d.input(u.ID))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if signed.Status != "signed" || signed.Signature == nil || signed.Signature.TypedName != "Priya Nair" ||
		signed.Signature.Email != "client1@example.com" {
		t.Fatalf("signed = %+v sig=%+v", signed, signed.Signature)
	}
	var consent, ip, ua string
	var hash []byte
	if err := d.e.Pool.QueryRow(d.e.Ctx, `SELECT consent_text, ip, user_agent, file_sha256 FROM document_signatures WHERE document_id = $1`, doc.ID).
		Scan(&consent, &ip, &ua, &hash); err != nil {
		t.Fatal(err)
	}
	if consent != clients.ConsentText("Client One") || ip != "203.0.113.7" || ua != "Test/1.0" || string(hash) != string(doc.FileSHA256) {
		t.Fatalf("evidence = %q %q %q", consent, ip, ua)
	}
	cert, err := core.GetFile(d.e.Ctx, d.e.Pool, d.org.ID, signed.Signature.CertificateFileID)
	if err != nil || cert.MIME != "application/pdf" {
		t.Fatalf("certificate = %+v, %v", cert, err)
	}
}

func TestSignValidation(t *testing.T) {
	d := newDocEnv(t)
	u := d.portalLogin(t)
	doc := d.sent(t, "signature")
	for _, name := range []string{"", "   "} {
		in := d.input(u.ID)
		in.TypedName = name
		if _, err := d.sign(doc, d.client.ID, in); !errors.Is(err, clients.ErrNameRequired) || err.Error() != "type your full name" {
			t.Errorf("name %q err = %v, want ErrNameRequired", name, err)
		}
	}
	in := d.input(u.ID)
	in.Consent = false
	if _, err := d.sign(doc, d.client.ID, in); !errors.Is(err, clients.ErrConsentRequired) || err.Error() != "tick the consent box to sign" {
		t.Errorf("no consent err = %v, want ErrConsentRequired", err)
	}
	if n := d.e.Count("document_signatures", "true"); n != 0 {
		t.Fatalf("signatures = %d, want 0", n)
	}
}

func TestSignStates(t *testing.T) {
	d := newDocEnv(t)
	u := d.portalLogin(t)

	draft := d.draft(t, "signature", nil)
	if _, err := d.sign(draft, d.client.ID, d.input(u.ID)); !errors.Is(err, clients.ErrBadState) {
		t.Errorf("draft err = %v, want ErrBadState", err)
	}
	withdrawn := d.sent(t, "signature")
	if _, err := clients.WithdrawDocument(d.e.Ctx, d.e.Pool, d.org.ID, d.owner.ID, withdrawn.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.sign(withdrawn, d.client.ID, d.input(u.ID)); !errors.Is(err, clients.ErrBadState) {
		t.Errorf("withdrawn err = %v, want ErrBadState", err)
	}
	superseded := d.sent(t, "signature")
	repl, err := clients.ReplaceDocument(d.e.Ctx, d.e.Pool, d.org.ID, d.owner.ID, superseded.ID, d.pdf(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := clients.SendDocument(d.e.Ctx, d.e.Pool, d.org.ID, d.owner.ID, repl.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.sign(superseded, d.client.ID, d.input(u.ID)); !errors.Is(err, clients.ErrBadState) {
		t.Errorf("superseded err = %v, want ErrBadState", err)
	}
	shared := d.sent(t, "shared")
	if _, err := d.sign(shared, d.client.ID, d.input(u.ID)); !errors.Is(err, core.ErrNotFound) {
		t.Errorf("shared err = %v, want ErrNotFound", err)
	}
	ok := d.sent(t, "signature")
	other := d.e.Contact(d.org)
	if _, err := d.sign(ok, other.ID, d.input(u.ID)); !errors.Is(err, core.ErrNotFound) {
		t.Errorf("other contact err = %v, want ErrNotFound", err)
	}
	if _, err := d.sign(ok, d.client.ID, d.input(u.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := d.sign(ok, d.client.ID, d.input(u.ID)); !errors.Is(err, clients.ErrAlreadySigned) || err.Error() != "already signed" {
		t.Errorf("second sign err = %v, want ErrAlreadySigned", err)
	}
}

func TestSignConcurrentOnlyOnce(t *testing.T) {
	d := newDocEnv(t)
	u := d.portalLogin(t)
	doc := d.sent(t, "signature")
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = d.sign(doc, d.client.ID, d.input(u.ID))
		}(i)
	}
	wg.Wait()
	okCount, already := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			okCount++
		case errors.Is(err, clients.ErrAlreadySigned):
			already++
		default:
			t.Fatalf("unexpected err %v", err)
		}
	}
	if okCount != 1 || already != 1 {
		t.Fatalf("ok=%d already=%d, want 1/1", okCount, already)
	}
	if n := d.e.Count("document_signatures", "document_id = $1", doc.ID); n != 1 {
		t.Fatalf("signatures = %d, want 1", n)
	}
}

func TestSignRefusesHashMismatch(t *testing.T) {
	d := newDocEnv(t)
	u := d.portalLogin(t)
	doc := d.sent(t, "signature")
	if _, err := d.e.Pool.Exec(d.e.Ctx, `UPDATE client_documents SET file_sha256 = '\x00' WHERE id = $1`, doc.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.sign(doc, d.client.ID, d.input(u.ID)); !errors.Is(err, clients.ErrFileChanged) || err.Error() != "document file has changed" {
		t.Fatalf("err = %v, want ErrFileChanged", err)
	}
	got, _ := clients.GetDocument(d.e.Ctx, d.e.Pool, d.org.ID, doc.ID)
	if got.Status != "sent" || d.e.Count("document_signatures", "true") != 0 {
		t.Fatalf("status = %q after refused sign", got.Status)
	}
}

func TestSignedImmutable(t *testing.T) {
	d := newDocEnv(t)
	u := d.portalLogin(t)
	doc := d.sent(t, "signature")
	if _, err := d.sign(doc, d.client.ID, d.input(u.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := clients.WithdrawDocument(d.e.Ctx, d.e.Pool, d.org.ID, d.owner.ID, doc.ID); !errors.Is(err, clients.ErrSigned) {
		t.Errorf("withdraw err = %v, want ErrSigned", err)
	}
	if _, err := clients.ReplaceDocument(d.e.Ctx, d.e.Pool, d.org.ID, d.owner.ID, doc.ID, d.pdf(t)); !errors.Is(err, clients.ErrSigned) {
		t.Errorf("replace err = %v, want ErrSigned", err)
	}
	if err := clients.DeleteDraft(d.e.Ctx, d.e.Pool, d.store, d.org.ID, d.owner.ID, doc.ID); !errors.Is(err, clients.ErrSigned) {
		t.Errorf("delete err = %v, want ErrSigned", err)
	}
}

func TestSignedRecipients(t *testing.T) {
	d := newDocEnv(t)
	got, err := clients.SignedRecipients(d.e.Ctx, d.e.Pool, d.org.ID, &d.owner.ID)
	if err != nil || len(got) != 1 || got[0] != "owner@example.com" {
		t.Fatalf("recipients = %v, %v", got, err)
	}
}

// failingCommitStore wraps a store so staged writes fail to commit once
// fail is set, as a full or broken disk would.
type failingCommitStore struct {
	storage.Store
	fail bool
}

type failingPending struct{ storage.Pending }

func (failingPending) Commit(context.Context, storage.Key) error { return errors.New("disk full") }

func (s *failingCommitStore) Begin(ctx context.Context, orgID int64) (storage.Pending, error) {
	p, err := s.Store.Begin(ctx, orgID)
	if err != nil || !s.fail {
		return p, err
	}
	return failingPending{p}, nil
}

func TestSignCertificateStoreFailureRecordsNothing(t *testing.T) {
	d := newDocEnv(t)
	u := d.portalLogin(t)
	doc := d.sent(t, "signature")
	store := &failingCommitStore{Store: d.store, fail: true}
	_, err := clients.Sign(d.e.Ctx, d.e.Pool, store, time.UTC, d.org.ID, d.client.ID, doc.ID, d.input(u.ID))
	if err == nil {
		t.Fatal("Sign succeeded with a failing store")
	}
	got, _ := clients.GetDocument(d.e.Ctx, d.e.Pool, d.org.ID, doc.ID)
	if got.Status != "sent" || d.e.Count("document_signatures", "true") != 0 {
		t.Fatalf("after failed certificate store: status %q, signatures %d", got.Status, d.e.Count("document_signatures", "true"))
	}
	// A retry with a healthy store signs.
	if _, err := clients.Sign(d.e.Ctx, d.e.Pool, d.store, time.UTC, d.org.ID, d.client.ID, doc.ID, d.input(u.ID)); err != nil {
		t.Fatalf("retry: %v", err)
	}
}
