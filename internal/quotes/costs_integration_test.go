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

func TestProjectCosts(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	e.Settings(org, testkit.GSTRegistered(true))
	client := e.Contact(org).ID
	p := e.Project(org, client)

	onProject := func(lines ...quotes.NewLine) quotes.Draft {
		d := draft(client, lines...)
		d.ProjectID = &p.ID
		return d
	}
	frLabour := func(unit core.Cents) quotes.NewLine {
		return quotes.NewLine{Kind: quotes.KindLabour, Description: "Work", QuantityMilli: 1000, UnitAmount: unit, GSTCode: "GST"}
	}
	a1 := sentQuote(t, e, org.ID, onProject(frLabour(1000), material("Cable", 1000, 500)))
	a2 := sentQuote(t, e, org.ID, onProject(frLabour(2000)))
	declined := sentQuote(t, e, org.ID, onProject(frLabour(99999)))
	superseded := sentQuote(t, e, org.ID, onProject(frLabour(88888)))
	for id, status := range map[int64]string{a1.ID: quotes.StatusAccepted, a2.ID: quotes.StatusAccepted, declined.ID: quotes.StatusDeclined, superseded.ID: quotes.StatusSuperseded} {
		setStatus(t, e, id, status)
	}

	// A $110 GST-inclusive expense line on the project: $100 ex GST.
	var category, expense int64
	if err := e.Pool.QueryRow(e.Ctx, `SELECT id FROM categories WHERE org_id = $1 AND name = 'Equipment'`, org.ID).Scan(&category); err != nil {
		t.Fatal(err)
	}
	if err := e.Pool.QueryRow(e.Ctx, `INSERT INTO expenses (org_id, spent_on, description) VALUES ($1, '2026-12-01', 'Switchboard parts') RETURNING id`, org.ID).Scan(&expense); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Pool.Exec(e.Ctx, `INSERT INTO expense_lines (org_id, expense_id, category_id, amount, gst_code, gst_amount, project_id)
		VALUES ($1, $2, $3, 11000, 'GST', 1000, $4)`, org.ID, expense, category, p.ID); err != nil {
		t.Fatal(err)
	}

	today := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	open := e.Invoice(org, client, testkit.Amount(3000), testkit.InvoiceGST("GST"), testkit.Issued(today))
	void := e.Invoice(org, client, testkit.Amount(9999), testkit.InvoiceGST("GST"), testkit.Issued(today))
	draftInvoice := e.Invoice(org, client, testkit.Amount(7777), testkit.InvoiceGST("GST"))
	for _, id := range []int64{open.ID, void.ID, draftInvoice.ID} {
		if _, err := e.Pool.Exec(e.Ctx, `UPDATE invoice_lines SET project_id = $1 WHERE invoice_id = $2`, p.ID, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.Pool.Exec(e.Ctx, `UPDATE invoices SET status = 'void' WHERE id = $1`, void.ID); err != nil {
		t.Fatal(err)
	}

	c, err := quotes.ProjectCosts(e.Ctx, e.Pool, org.ID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if c.QuotedLabour != 3000 || c.QuotedMaterials != 500 || c.QuotedTotal != 3500 || c.Invoiced != 3000 || c.ExpensesTotal != 10000 {
		t.Fatalf("costs = %+v", c)
	}
	if len(c.Expenses) != 1 || c.Expenses[0].Amount != 10000 || c.Expenses[0].Description != "Switchboard parts" ||
		c.Expenses[0].Category != "Equipment" || c.Expenses[0].ExpenseID != expense {
		t.Fatalf("expense lines = %+v", c.Expenses)
	}

	other := e.Org("other")
	if _, err := quotes.ProjectCosts(e.Ctx, e.Pool, other.ID, p.ID); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("cross-org err = %v, want ErrNotFound", err)
	}
}
