//go:build integration

// Package clients_test exercises the client portal store (welcome notes,
// checklist, documents, signing) against a real database.
package clients_test

import (
	"errors"
	"fmt"
	"testing"

	"shigoto/internal/clients"
	"shigoto/internal/core"
	"shigoto/internal/testkit"
)

func TestWelcomeUpsert(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	owner := e.User("owner@example.com")
	c := e.Contact(org, testkit.Kind("client"))

	got, err := clients.GetWelcome(e.Ctx, e.Pool, org.ID, c.ID)
	if err != nil || got.Notes != "" || got.UpdatedAt != nil {
		t.Fatalf("empty welcome = %+v, %v", got, err)
	}
	if _, err := clients.PutWelcome(e.Ctx, e.Pool, org.ID, c.ID, owner.ID, "Slack #acme, 4 business hours"); err != nil {
		t.Fatalf("PutWelcome: %v", err)
	}
	if _, err := clients.PutWelcome(e.Ctx, e.Pool, org.ID, c.ID, owner.ID, "Email, 1 business day"); err != nil {
		t.Fatalf("second PutWelcome: %v", err)
	}
	got, err = clients.GetWelcome(e.Ctx, e.Pool, org.ID, c.ID)
	if err != nil || got.Notes != "Email, 1 business day" || got.UpdatedAt == nil {
		t.Fatalf("welcome = %+v, %v", got, err)
	}
	if n := e.Count("client_welcome", "contact_id = $1", c.ID); n != 1 {
		t.Fatalf("welcome rows = %d, want 1", n)
	}
}

func TestWelcomeRejectsNonClient(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	owner := e.User("owner@example.com")
	tenant := e.Contact(org, testkit.Kind("tenant"))

	_, err := clients.PutWelcome(e.Ctx, e.Pool, org.ID, tenant.ID, owner.ID, "hi")
	if !errors.Is(err, clients.ErrNotClient) {
		t.Fatalf("err = %v, want ErrNotClient", err)
	}
	other := e.Org("beta")
	foreign := e.Contact(other, testkit.Kind("client"))
	if _, err := clients.PutWelcome(e.Ctx, e.Pool, org.ID, foreign.ID, owner.ID, "hi"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("foreign contact err = %v, want ErrNotFound", err)
	}
}

func TestChecklistOrderingAndDone(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	owner := e.User("owner@example.com")
	c := e.Contact(org, testkit.Kind("client"))

	due := testkit.Date("2026-11-27")
	var ids []int64
	for _, a := range []string{"Grant GA access", "Send brand PDF", "Add us to Slack"} {
		it, err := clients.CreateChecklistItem(e.Ctx, e.Pool, org.ID, c.ID, owner.ID, clients.ChecklistItem{Action: a, Owner: "Sam", DueDate: &due})
		if err != nil {
			t.Fatalf("CreateChecklistItem(%q): %v", a, err)
		}
		ids = append(ids, it.ID)
	}
	list, err := clients.ListChecklist(e.Ctx, e.Pool, org.ID, c.ID)
	if err != nil || len(list) != 3 || list[0].Action != "Grant GA access" || list[2].Action != "Add us to Slack" {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if list[0].Position >= list[1].Position {
		t.Fatalf("positions not ascending: %+v", list)
	}

	done, err := clients.SetChecklistDone(e.Ctx, e.Pool, org.ID, c.ID, owner.ID, ids[1], true)
	if err != nil || done.DoneAt == nil {
		t.Fatalf("done = %+v, %v", done, err)
	}
	undone, err := clients.SetChecklistDone(e.Ctx, e.Pool, org.ID, c.ID, owner.ID, ids[1], false)
	if err != nil || undone.DoneAt != nil {
		t.Fatalf("undone = %+v, %v", undone, err)
	}

	if _, err := clients.CreateChecklistItem(e.Ctx, e.Pool, org.ID, c.ID, owner.ID, clients.ChecklistItem{Action: "   "}); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("blank action err = %v, want ErrInvalidInput", err)
	}
	if err := clients.DeleteChecklistItem(e.Ctx, e.Pool, org.ID, c.ID, owner.ID, ids[0]); err != nil {
		t.Fatalf("DeleteChecklistItem: %v", err)
	}
	list, _ = clients.ListChecklist(e.Ctx, e.Pool, org.ID, c.ID)
	if len(list) != 2 {
		t.Fatalf("after delete len = %d, want 2", len(list))
	}
}

func TestChecklistFull(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	owner := e.User("owner@example.com")
	c := e.Contact(org, testkit.Kind("client"))
	for i := 0; i < 20; i++ {
		if _, err := clients.CreateChecklistItem(e.Ctx, e.Pool, org.ID, c.ID, owner.ID, clients.ChecklistItem{Action: fmt.Sprintf("Item %d", i)}); err != nil {
			t.Fatalf("item %d: %v", i, err)
		}
	}
	_, err := clients.CreateChecklistItem(e.Ctx, e.Pool, org.ID, c.ID, owner.ID, clients.ChecklistItem{Action: "One too many"})
	if !errors.Is(err, core.ErrInvalidInput) || err.Error() != "checklist is full" {
		t.Fatalf("21st item err = %v, want checklist is full", err)
	}
}

func TestChecklistCrossContact(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	owner := e.User("owner@example.com")
	a := e.Contact(org, testkit.Kind("client"), testkit.Name("A"))
	b := e.Contact(org, testkit.Kind("client"), testkit.Name("B"))
	it, err := clients.CreateChecklistItem(e.Ctx, e.Pool, org.ID, a.ID, owner.ID, clients.ChecklistItem{Action: "A's item"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := clients.UpdateChecklistItem(e.Ctx, e.Pool, org.ID, b.ID, owner.ID, it.ID, clients.ChecklistItem{Action: "hijack"}); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("update via other contact err = %v, want ErrNotFound", err)
	}
	if _, err := clients.SetChecklistDone(e.Ctx, e.Pool, org.ID, b.ID, owner.ID, it.ID, true); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("done via other contact err = %v, want ErrNotFound", err)
	}
	if err := clients.DeleteChecklistItem(e.Ctx, e.Pool, org.ID, b.ID, owner.ID, it.ID); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("delete via other contact err = %v, want ErrNotFound", err)
	}
}
