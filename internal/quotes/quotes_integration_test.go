//go:build integration

package quotes_test

import (
	"errors"
	"testing"
	"time"

	"shigoto/internal/core"
	"shigoto/internal/quotes"
	"shigoto/internal/testkit"
)

var (
	issued = time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)
	until  = time.Date(2026, 12, 27, 0, 0, 0, 0, time.UTC)
)

func labour(desc string, hoursMilli int64, rate core.Cents) quotes.NewLine {
	return quotes.NewLine{Kind: quotes.KindLabour, Description: desc, QuantityMilli: hoursMilli, UnitAmount: rate, GSTCode: "GST"}
}

func material(desc string, qtyMilli int64, unit core.Cents) quotes.NewLine {
	return quotes.NewLine{Kind: quotes.KindMaterial, Description: desc, QuantityMilli: qtyMilli, UnitAmount: unit, GSTCode: "GST"}
}

func draft(contactID int64, lines ...quotes.NewLine) quotes.Draft {
	return quotes.Draft{ContactID: contactID, Title: "Switchboard upgrade", Terms: "50% deposit", IssueDate: issued, ValidUntil: until, Lines: lines}
}

// setStatus forces a quote's status, for tests of later lifecycle steps
// that don't depend on how the quote got there.
func setStatus(t *testing.T, e *testkit.Env, id int64, status string) {
	t.Helper()
	if _, err := e.Pool.Exec(e.Ctx, `UPDATE quotes SET status = $1 WHERE id = $2`, status, id); err != nil {
		t.Fatal(err)
	}
}

func TestCreateDraftTotals(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	e.Settings(org, testkit.GSTRegistered(true))
	client := e.Contact(org, testkit.Name("Priya")).ID

	q, err := quotes.CreateDraft(e.Ctx, e.Pool, org.ID, draft(client,
		labour("Rough-in", 2500, 12000),
		material("Switchboard", 1000, 18000),
	))
	if err != nil {
		t.Fatalf("CreateDraft: %v", err)
	}
	if q.Status != quotes.StatusDraft || q.Number != 1 || q.Revision != 1 {
		t.Fatalf("quote = %+v", q)
	}
	if q.Subtotal != 48000 || q.GSTTotal != 4800 || q.Total != 52800 {
		t.Fatalf("totals = %d/%d/%d, want 48000/4800/52800", q.Subtotal, q.GSTTotal, q.Total)
	}
	got, err := quotes.GetQuote(e.Ctx, e.Pool, org.ID, q.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Lines) != 2 || got.Lines[0].Kind != quotes.KindLabour || got.Lines[0].Description != "Rough-in" ||
		got.Lines[0].Amount != 30000 || got.Lines[0].GSTAmount != 3000 || got.Lines[1].Kind != quotes.KindMaterial {
		t.Fatalf("lines = %+v", got.Lines)
	}
	if got.Terms != "50% deposit" || !got.ValidUntil.Equal(until) {
		t.Fatalf("round trip = %+v", got)
	}
}

func TestCreateDraftValidation(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	client := e.Contact(org).ID
	other := e.Contact(org, testkit.Name("Other client")).ID
	tenant := e.Contact(org, testkit.Kind("tenant"), testkit.Name("Tina")).ID
	otherProject := e.Project(org, other).ID
	frLine := quotes.NewLine{Kind: quotes.KindLabour, Description: "Work", QuantityMilli: 1000, UnitAmount: 100, GSTCode: "FRE"}

	cases := []struct {
		name string
		d    quotes.Draft
		want string
	}{
		{"no lines", draft(client), "invalid input: quote must have at least one line"},
		{"bad kind", draft(client, quotes.NewLine{Kind: "other", Description: "x", QuantityMilli: 1000, GSTCode: "FRE"}), "invalid input: line 0: kind must be labour or material"},
		{"GST unregistered", draft(client, labour("Work", 1000, 100)), "invalid input: line 0: org is not GST-registered"},
		{"INP not offered", draft(client, quotes.NewLine{Kind: quotes.KindMaterial, Description: "x", QuantityMilli: 1000, GSTCode: "INP"}), "invalid input: line 0: gst_code must be GST, FRE or N-T"},
		{"blank description", draft(client, quotes.NewLine{Kind: quotes.KindMaterial, Description: " ", QuantityMilli: 1000, GSTCode: "FRE"}), "invalid input: line 0: description is required"},
		{"zero quantity", draft(client, quotes.NewLine{Kind: quotes.KindMaterial, Description: "x", GSTCode: "FRE"}), "invalid input: line 0: quantity_milli must be from 1 to 10,000,000"},
		{"tenant contact", draft(tenant, frLine), "invalid input: contact_id is not a client"},
		{"unknown contact", draft(999999, frLine), "invalid input: contact_id not found"},
		{"blank title", func() quotes.Draft { d := draft(client, frLine); d.Title = " "; return d }(), "invalid input: title is required"},
		{"dates reversed", func() quotes.Draft { d := draft(client, frLine); d.ValidUntil = issued.AddDate(0, 0, -1); return d }(), "invalid input: valid_until must not be before issue_date"},
		{"other client's project", func() quotes.Draft { d := draft(client, frLine); d.ProjectID = &otherProject; return d }(), "invalid input: project belongs to another client"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := quotes.CreateDraft(e.Ctx, e.Pool, org.ID, c.d)
			if err == nil || err.Error() != c.want {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
	if n := e.Count("quotes", "org_id = $1", org.ID); n != 0 {
		t.Fatalf("%d quotes written by invalid drafts", n)
	}
}

func TestNumbering(t *testing.T) {
	e := testkit.New(t)
	acme, other := e.Org("acme"), e.Org("other")
	line := quotes.NewLine{Kind: quotes.KindMaterial, Description: "x", QuantityMilli: 1000, UnitAmount: 100, GSTCode: "FRE"}
	a1, err1 := quotes.CreateDraft(e.Ctx, e.Pool, acme.ID, draft(e.Contact(acme).ID, line))
	a2, err2 := quotes.CreateDraft(e.Ctx, e.Pool, acme.ID, draft(e.Contact(acme, testkit.Name("B")).ID, line))
	o1, err3 := quotes.CreateDraft(e.Ctx, e.Pool, other.ID, draft(e.Contact(other).ID, line))
	if err := errors.Join(err1, err2, err3); err != nil {
		t.Fatal(err)
	}
	if a1.Number != 1 || a2.Number != 2 || o1.Number != 1 {
		t.Fatalf("numbers = %d, %d, %d; want 1, 2, 1", a1.Number, a2.Number, o1.Number)
	}
}

func TestUpdateDraftOnlyDraft(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	client := e.Contact(org).ID
	line := quotes.NewLine{Kind: quotes.KindMaterial, Description: "x", QuantityMilli: 1000, UnitAmount: 100, GSTCode: "FRE"}
	q, err := quotes.CreateDraft(e.Ctx, e.Pool, org.ID, draft(client, line))
	if err != nil {
		t.Fatal(err)
	}
	d := draft(client, line, line)
	d.Title = "Bigger job"
	updated, err := quotes.UpdateDraft(e.Ctx, e.Pool, org.ID, q.ID, d)
	if err != nil || updated.Title != "Bigger job" || len(updated.Lines) != 2 || updated.Subtotal != 200 || updated.Number != q.Number {
		t.Fatalf("UpdateDraft = %+v, %v", updated, err)
	}
	setStatus(t, e, q.ID, quotes.StatusSent)
	if _, err := quotes.UpdateDraft(e.Ctx, e.Pool, org.ID, q.ID, d); !errors.Is(err, core.ErrInvalidState) || err.Error() != "quote is not a draft" {
		t.Fatalf("update sent err = %v, want quote is not a draft", err)
	}
	if err := quotes.DeleteDraft(e.Ctx, e.Pool, org.ID, q.ID); !errors.Is(err, core.ErrInvalidState) {
		t.Fatalf("delete sent err = %v, want ErrInvalidState", err)
	}
	other := e.Org("other")
	if _, err := quotes.GetQuote(e.Ctx, e.Pool, other.ID, q.ID); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("cross-org get err = %v, want ErrNotFound", err)
	}
}

func TestRevise(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	client := e.Contact(org).ID
	line := quotes.NewLine{Kind: quotes.KindMaterial, Description: "x", QuantityMilli: 1000, UnitAmount: 100, GSTCode: "FRE"}
	q, err := quotes.CreateDraft(e.Ctx, e.Pool, org.ID, draft(client, line))
	if err != nil {
		t.Fatal(err)
	}
	today := time.Date(2026, 12, 10, 0, 0, 0, 0, time.UTC)
	if _, err := quotes.Revise(e.Ctx, e.Pool, org.ID, q.ID, today); !errors.Is(err, core.ErrInvalidState) {
		t.Fatalf("revise draft err = %v, want ErrInvalidState", err)
	}
	setStatus(t, e, q.ID, quotes.StatusSent)
	r2, err := quotes.Revise(e.Ctx, e.Pool, org.ID, q.ID, today)
	if err != nil {
		t.Fatalf("Revise: %v", err)
	}
	if r2.Number != q.Number || r2.Revision != 2 || r2.ParentID == nil || *r2.ParentID != q.ID || r2.Status != quotes.StatusDraft ||
		!r2.IssueDate.Equal(today) || !r2.ValidUntil.Equal(today.AddDate(0, 0, 30)) || len(r2.Lines) != 1 || r2.Terms != q.Terms {
		t.Fatalf("revision = %+v", r2)
	}
	if old, _ := quotes.GetQuote(e.Ctx, e.Pool, org.ID, q.ID); old.Status != quotes.StatusSent {
		t.Fatalf("original status = %q, want still sent until the revision is sent", old.Status)
	}
	if _, err := quotes.Revise(e.Ctx, e.Pool, org.ID, q.ID, today); !errors.Is(err, core.ErrInvalidState) || err.Error() != "a draft revision already exists" {
		t.Fatalf("second revise err = %v, want a draft revision already exists", err)
	}

	setStatus(t, e, q.ID, quotes.StatusAccepted)
	if err := quotes.DeleteDraft(e.Ctx, e.Pool, org.ID, r2.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := quotes.Revise(e.Ctx, e.Pool, org.ID, q.ID, today); !errors.Is(err, core.ErrInvalidState) {
		t.Fatalf("revise accepted err = %v, want ErrInvalidState", err)
	}
}

func TestDecline(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	user := e.User("owner@example.com")
	client := e.Contact(org).ID
	line := quotes.NewLine{Kind: quotes.KindMaterial, Description: "x", QuantityMilli: 1000, UnitAmount: 100, GSTCode: "FRE"}
	q, err := quotes.CreateDraft(e.Ctx, e.Pool, org.ID, draft(client, line))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 12, 1, 3, 0, 0, 0, time.UTC)
	if _, err := quotes.Decline(e.Ctx, e.Pool, org.ID, user.ID, q.ID, now); !errors.Is(err, core.ErrInvalidState) {
		t.Fatalf("decline draft err = %v, want ErrInvalidState", err)
	}
	setStatus(t, e, q.ID, quotes.StatusSent)
	got, err := quotes.Decline(e.Ctx, e.Pool, org.ID, user.ID, q.ID, now)
	if err != nil || got.Status != quotes.StatusDeclined || got.DecidedBy == nil || *got.DecidedBy != user.ID || got.DecidedAt == nil || !got.DecidedAt.Equal(now) {
		t.Fatalf("Decline = %+v, %v", got, err)
	}
	r2, err := quotes.Revise(e.Ctx, e.Pool, org.ID, q.ID, now)
	if err != nil || r2.Revision != 2 {
		t.Fatalf("revise declined = %+v, %v", r2, err)
	}
}

func TestListQuotes(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	a := e.Contact(org, testkit.Name("A")).ID
	b := e.Contact(org, testkit.Name("B")).ID
	line := quotes.NewLine{Kind: quotes.KindMaterial, Description: "x", QuantityMilli: 1000, UnitAmount: 100, GSTCode: "FRE"}
	qa, _ := quotes.CreateDraft(e.Ctx, e.Pool, org.ID, draft(a, line))
	if _, err := quotes.CreateDraft(e.Ctx, e.Pool, org.ID, draft(b, line)); err != nil {
		t.Fatal(err)
	}
	setStatus(t, e, qa.ID, quotes.StatusSent)
	all, total, err := quotes.ListQuotes(e.Ctx, e.Pool, org.ID, quotes.ListFilter{})
	if err != nil || total != 2 || len(all) != 2 || all[0].ID == qa.ID {
		t.Fatalf("list all = %d/%d %v; want newest first", len(all), total, err)
	}
	onlyA, total, _ := quotes.ListQuotes(e.Ctx, e.Pool, org.ID, quotes.ListFilter{ContactID: &a})
	sent, _, _ := quotes.ListQuotes(e.Ctx, e.Pool, org.ID, quotes.ListFilter{Status: quotes.StatusSent})
	if total != 1 || len(onlyA) != 1 || onlyA[0].ID != qa.ID || len(sent) != 1 || sent[0].ID != qa.ID {
		t.Fatalf("filters: contact %+v, sent %+v", onlyA, sent)
	}
}
