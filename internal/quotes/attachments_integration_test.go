//go:build integration

package quotes_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"shigoto/internal/core"
	"shigoto/internal/module"
	"shigoto/internal/quotes"
	"shigoto/internal/storage"
	"shigoto/internal/testkit"
)

func saveFile(t *testing.T, e *testkit.Env, store storage.Store, orgID int64, name string, data []byte) core.File {
	t.Helper()
	f, err := core.SaveFile(e.Ctx, e.Pool, store, orgID, name, bytes.NewReader(data), core.DocumentPolicy)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func attach(t *testing.T, e *testkit.Env, orgID, quoteID, userID int64, f core.File) (quotes.Attachment, error) {
	t.Helper()
	var a quotes.Attachment
	err := module.RunInTx(e.Ctx, e.Pool, func(tx pgx.Tx) error {
		var err error
		a, err = quotes.AddAttachment(e.Ctx, tx, orgID, quoteID, userID, f)
		return err
	})
	return a, err
}

func TestAttachments(t *testing.T) {
	e := testkit.New(t)
	store := storage.NewDisk(t.TempDir())
	org := e.Org("acme")
	user := e.User("owner@example.com")
	q, err := quotes.CreateDraft(e.Ctx, e.Pool, org.ID, draft(e.Contact(org).ID,
		quotes.NewLine{Kind: quotes.KindMaterial, Description: "x", QuantityMilli: 1000, UnitAmount: 100, GSTCode: "FRE"}))
	if err != nil {
		t.Fatal(err)
	}
	pdf := saveFile(t, e, store, org.ID, "sow.pdf", testkit.PDF("scope"))
	png := saveFile(t, e, store, org.ID, "photo.png", testkit.PNG)

	a1, err := attach(t, e, org.ID, q.ID, user.ID, pdf)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := attach(t, e, org.ID, q.ID, user.ID, png); err != nil {
		t.Fatal(err)
	}
	again, err := attach(t, e, org.ID, q.ID, user.ID, pdf)
	if err != nil || again.ID != a1.ID {
		t.Fatalf("re-attach = %+v, %v; want existing attachment %d", again, err, a1.ID)
	}
	list, err := quotes.ListAttachments(e.Ctx, e.Pool, org.ID, q.ID)
	if err != nil || len(list) != 2 || list[0].File.OriginalName != "photo.png" || list[1].File.OriginalName != "sow.pdf" {
		t.Fatalf("list = %+v, %v; want newest first", list, err)
	}

	// Attachments are records: allowed after acceptance too.
	setStatus(t, e, q.ID, quotes.StatusAccepted)
	extra := saveFile(t, e, store, org.ID, "extra.pdf", testkit.PDF("extra"))
	if _, err := attach(t, e, org.ID, q.ID, user.ID, extra); err != nil {
		t.Fatalf("attach to accepted: %v", err)
	}

	other := e.Org("other")
	if _, err := quotes.ListAttachments(e.Ctx, e.Pool, other.ID, q.ID); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("cross-org list err = %v, want ErrNotFound", err)
	}
	if _, err := quotes.GetAttachment(e.Ctx, e.Pool, other.ID, q.ID, a1.ID); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("cross-org get err = %v, want ErrNotFound", err)
	}

	var fileID int64
	if err := module.RunInTx(e.Ctx, e.Pool, func(tx pgx.Tx) error {
		var err error
		fileID, err = quotes.RemoveAttachment(e.Ctx, tx, org.ID, q.ID, a1.ID)
		return err
	}); err != nil || fileID != pdf.ID {
		t.Fatalf("RemoveAttachment = %d, %v", fileID, err)
	}
	if list, _ := quotes.ListAttachments(e.Ctx, e.Pool, org.ID, q.ID); len(list) != 2 {
		t.Fatalf("after remove: %d attachments, want 2", len(list))
	}
}

func TestDeleteDraftReturnsAttachmentFiles(t *testing.T) {
	e := testkit.New(t)
	store := storage.NewDisk(t.TempDir())
	org := e.Org("acme")
	user := e.User("owner@example.com")
	q, err := quotes.CreateDraft(e.Ctx, e.Pool, org.ID, draft(e.Contact(org).ID,
		quotes.NewLine{Kind: quotes.KindMaterial, Description: "x", QuantityMilli: 1000, UnitAmount: 100, GSTCode: "FRE"}))
	if err != nil {
		t.Fatal(err)
	}
	f := saveFile(t, e, store, org.ID, "sow.pdf", testkit.PDF("scope"))
	if _, err := attach(t, e, org.ID, q.ID, user.ID, f); err != nil {
		t.Fatal(err)
	}
	ids, err := quotes.DeleteDraftFiles(e.Ctx, e.Pool, org.ID, q.ID)
	if err != nil || len(ids) != 1 || ids[0] != f.ID {
		t.Fatalf("DeleteDraftFiles = %v, %v", ids, err)
	}
	if n := e.Count("quotes", "id = $1", q.ID); n != 0 {
		t.Fatal("quote not deleted")
	}
}
