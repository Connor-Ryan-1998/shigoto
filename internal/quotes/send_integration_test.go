//go:build integration

package quotes_test

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"shigoto/internal/core"
	"shigoto/internal/module"
	"shigoto/internal/quotes"
	"shigoto/internal/storage"
	"shigoto/internal/testkit"
)

var sentAt = time.Date(2026, 11, 28, 1, 0, 0, 0, time.UTC)

func markSent(t *testing.T, e *testkit.Env, store storage.Store, orgID, id int64) (quotes.Quote, error) {
	t.Helper()
	var q quotes.Quote
	err := module.RunInTx(e.Ctx, e.Pool, func(tx pgx.Tx) error {
		var err error
		q, err = quotes.MarkSent(e.Ctx, tx, store, orgID, id, sentAt)
		return err
	})
	return q, err
}

func storedPDF(t *testing.T, e *testkit.Env, store storage.Store, orgID, fileID int64) []byte {
	t.Helper()
	f, err := core.GetFile(e.Ctx, e.Pool, orgID, fileID)
	if err != nil {
		t.Fatal(err)
	}
	blob, err := core.OpenFile(e.Ctx, store, f)
	if err != nil {
		t.Fatal(err)
	}
	defer blob.Close()
	b, err := io.ReadAll(blob)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestMarkSentFreezesPDF(t *testing.T) {
	e := testkit.New(t)
	store := storage.NewDisk(t.TempDir())
	org := e.Org("acme")
	q, err := quotes.CreateDraft(e.Ctx, e.Pool, org.ID, draft(e.Contact(org).ID,
		quotes.NewLine{Kind: quotes.KindMaterial, Description: "Cable", QuantityMilli: 1000, UnitAmount: 100, GSTCode: "FRE"}))
	if err != nil {
		t.Fatal(err)
	}
	sent, err := markSent(t, e, store, org.ID, q.ID)
	if err != nil {
		t.Fatalf("MarkSent: %v", err)
	}
	if sent.Status != quotes.StatusSent || sent.SentAt == nil || !sent.SentAt.Equal(sentAt) || sent.PDFFileID == nil {
		t.Fatalf("sent = %+v", sent)
	}
	before := storedPDF(t, e, store, org.ID, *sent.PDFFileID)
	if !bytes.HasPrefix(before, []byte("%PDF-")) {
		t.Fatal("stored file is not a PDF")
	}
	if _, err := e.Pool.Exec(e.Ctx, `UPDATE orgs SET name = 'Renamed' WHERE id = $1`, org.ID); err != nil {
		t.Fatal(err)
	}
	pdf, name, err := quotes.PDF(e.Ctx, e.Pool, store, org.ID, sent)
	if err != nil || !bytes.Equal(pdf, before) || name != "Q-0001.pdf" {
		t.Fatalf("PDF after rename: name %q, err %v, unchanged %v", name, err, bytes.Equal(pdf, before))
	}

	if _, err := markSent(t, e, store, org.ID, q.ID); !errors.Is(err, core.ErrInvalidState) || err.Error() != "quote is not a draft" {
		t.Fatalf("resend err = %v, want quote is not a draft", err)
	}
}

func TestDraftPDFRendersOnDemand(t *testing.T) {
	e := testkit.New(t)
	store := storage.NewDisk(t.TempDir())
	org := e.Org("acme")
	q, err := quotes.CreateDraft(e.Ctx, e.Pool, org.ID, draft(e.Contact(org).ID,
		quotes.NewLine{Kind: quotes.KindMaterial, Description: "Cable", QuantityMilli: 1000, UnitAmount: 100, GSTCode: "FRE"}))
	if err != nil {
		t.Fatal(err)
	}
	pdf, name, err := quotes.PDF(e.Ctx, e.Pool, store, org.ID, q)
	if err != nil || !bytes.HasPrefix(pdf, []byte("%PDF-")) || name != "Q-0001 (draft).pdf" {
		t.Fatalf("draft PDF: name %q, err %v", name, err)
	}
	if n := e.Count("files", "org_id = $1", org.ID); n != 0 {
		t.Fatalf("draft PDF stored %d files, want none", n)
	}
}

func TestMarkSentSupersedesParent(t *testing.T) {
	e := testkit.New(t)
	store := storage.NewDisk(t.TempDir())
	org := e.Org("acme")
	q, err := quotes.CreateDraft(e.Ctx, e.Pool, org.ID, draft(e.Contact(org).ID,
		quotes.NewLine{Kind: quotes.KindMaterial, Description: "Cable", QuantityMilli: 1000, UnitAmount: 100, GSTCode: "FRE"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := markSent(t, e, store, org.ID, q.ID); err != nil {
		t.Fatal(err)
	}
	r2, err := quotes.Revise(e.Ctx, e.Pool, org.ID, q.ID, issued)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := markSent(t, e, store, org.ID, r2.ID); err != nil {
		t.Fatalf("send R2: %v", err)
	}
	r1, _ := quotes.GetQuote(e.Ctx, e.Pool, org.ID, q.ID)
	got, _ := quotes.GetQuote(e.Ctx, e.Pool, org.ID, r2.ID)
	if r1.Status != quotes.StatusSuperseded || got.Status != quotes.StatusSent {
		t.Fatalf("statuses = %q / %q, want superseded / sent", r1.Status, got.Status)
	}
}

func TestSendRevisionAfterAcceptRefused(t *testing.T) {
	e := testkit.New(t)
	store := storage.NewDisk(t.TempDir())
	org := e.Org("acme")
	q, err := quotes.CreateDraft(e.Ctx, e.Pool, org.ID, draft(e.Contact(org).ID,
		quotes.NewLine{Kind: quotes.KindMaterial, Description: "Cable", QuantityMilli: 1000, UnitAmount: 100, GSTCode: "FRE"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := markSent(t, e, store, org.ID, q.ID); err != nil {
		t.Fatal(err)
	}
	r2, err := quotes.Revise(e.Ctx, e.Pool, org.ID, q.ID, issued)
	if err != nil {
		t.Fatal(err)
	}
	setStatus(t, e, q.ID, quotes.StatusAccepted)
	if _, err := markSent(t, e, store, org.ID, r2.ID); !errors.Is(err, core.ErrInvalidState) || err.Error() != "another revision of this quote was accepted" {
		t.Fatalf("send R2 err = %v, want another revision of this quote was accepted", err)
	}
	if r1, _ := quotes.GetQuote(e.Ctx, e.Pool, org.ID, q.ID); r1.Status != quotes.StatusAccepted {
		t.Fatalf("R1 status = %q, want still accepted", r1.Status)
	}
}
