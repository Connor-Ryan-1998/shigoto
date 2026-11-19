//go:build integration

package core_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"shigoto/internal/core"
	"shigoto/internal/testkit"
)

func TestMarkUncollectibleAndReinstate(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	inv := e.Invoice(org, e.Contact(org).ID, testkit.Amount(10000), testkit.Issued(time.Now()))

	marked, err := core.MarkInvoiceUncollectible(e.Ctx, e.Pool, org.ID, inv.ID, time.Now())
	if err != nil {
		t.Fatalf("MarkInvoiceUncollectible: %v", err)
	}
	if marked.Status != "uncollectible" || !marked.WrittenOff {
		t.Fatalf("after mark: status %q written_off %v, want uncollectible true", marked.Status, marked.WrittenOff)
	}

	reinstated, err := core.ReinstateInvoice(e.Ctx, e.Pool, org.ID, inv.ID)
	if err != nil {
		t.Fatalf("ReinstateInvoice: %v", err)
	}
	if reinstated.Status != "open" || reinstated.WrittenOff {
		t.Fatalf("after reinstate: status %q written_off %v, want open false", reinstated.Status, reinstated.WrittenOff)
	}
}

func TestMarkUncollectibleRejectsNonOpen(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	contact := e.Contact(org).ID

	draft := e.Invoice(org, contact)
	paid := e.Invoice(org, contact, testkit.Amount(1000), testkit.Issued(time.Now()))
	if _, _, err := core.RecordPayment(e.Ctx, e.Pool, org.ID, core.NewPayment{InvoiceID: paid.ID, Amount: 1000, PaidOn: time.Now(), Method: "cash"}); err != nil {
		t.Fatalf("RecordPayment: %v", err)
	}
	void := e.Invoice(org, contact)
	if _, err := core.VoidInvoice(e.Ctx, e.Pool, org.ID, void.ID); err != nil {
		t.Fatalf("VoidInvoice: %v", err)
	}

	for name, id := range map[string]int64{"draft": draft.ID, "paid": paid.ID, "void": void.ID} {
		if _, err := core.MarkInvoiceUncollectible(e.Ctx, e.Pool, org.ID, id, time.Now()); !errors.Is(err, core.ErrInvalidState) {
			t.Errorf("mark %s: err = %v, want ErrInvalidState", name, err)
		}
	}

	open := e.Invoice(org, contact, testkit.Issued(time.Now()))
	if _, err := core.ReinstateInvoice(e.Ctx, e.Pool, org.ID, open.ID); !errors.Is(err, core.ErrInvalidState) {
		t.Errorf("reinstate open: err = %v, want ErrInvalidState", err)
	}
	if _, err := core.MarkInvoiceUncollectible(e.Ctx, e.Pool, org.ID, 999999, time.Now()); !errors.Is(err, core.ErrNotFound) {
		t.Errorf("mark missing: err = %v, want ErrNotFound", err)
	}
}

func TestVoidUncollectible(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	contact := e.Contact(org).ID

	clean := e.Invoice(org, contact, testkit.Amount(10000), testkit.Issued(time.Now()))
	if _, err := core.MarkInvoiceUncollectible(e.Ctx, e.Pool, org.ID, clean.ID, time.Now()); err != nil {
		t.Fatalf("mark: %v", err)
	}
	voided, err := core.VoidInvoice(e.Ctx, e.Pool, org.ID, clean.ID)
	if err != nil || voided.Status != "void" {
		t.Fatalf("void uncollectible: status %q err %v, want void nil", voided.Status, err)
	}

	partPaid := e.Invoice(org, contact, testkit.Amount(10000), testkit.Issued(time.Now()))
	if _, _, err := core.RecordPayment(e.Ctx, e.Pool, org.ID, core.NewPayment{InvoiceID: partPaid.ID, Amount: 4000, PaidOn: time.Now(), Method: "cash"}); err != nil {
		t.Fatalf("RecordPayment: %v", err)
	}
	if _, err := core.MarkInvoiceUncollectible(e.Ctx, e.Pool, org.ID, partPaid.ID, time.Now()); err != nil {
		t.Fatalf("mark part-paid: %v", err)
	}
	if _, err := core.VoidInvoice(e.Ctx, e.Pool, org.ID, partPaid.ID); !errors.Is(err, core.ErrInvoiceHasPayments) {
		t.Fatalf("void part-paid uncollectible: err = %v, want ErrInvoiceHasPayments", err)
	}
}

func TestUncollectiblePaymentMarksPaid(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	inv := e.Invoice(org, e.Contact(org).ID, testkit.Amount(10000), testkit.Issued(time.Now()))
	if _, err := core.MarkInvoiceUncollectible(e.Ctx, e.Pool, org.ID, inv.ID, time.Now()); err != nil {
		t.Fatalf("mark: %v", err)
	}
	if _, _, err := core.RecordPayment(e.Ctx, e.Pool, org.ID, core.NewPayment{InvoiceID: inv.ID, Amount: 10000, PaidOn: time.Now(), Method: "cash"}); err != nil {
		t.Fatalf("RecordPayment: %v", err)
	}
	got, err := core.GetInvoice(e.Ctx, e.Pool, org.ID, inv.ID)
	if err != nil {
		t.Fatalf("GetInvoice: %v", err)
	}
	if got.Status != "paid" || !got.WrittenOff {
		t.Fatalf("status %q written_off %v, want paid true", got.Status, got.WrittenOff)
	}
}

func TestWriteOffRecordsDateAndOutstanding(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	inv := e.Invoice(org, e.Contact(org).ID, testkit.Amount(50000), testkit.Issued(time.Now()))
	if _, _, err := core.RecordPayment(e.Ctx, e.Pool, org.ID, core.NewPayment{InvoiceID: inv.ID, Amount: 20000, PaidOn: time.Now(), Method: "cash"}); err != nil {
		t.Fatalf("RecordPayment: %v", err)
	}
	marked, err := core.MarkInvoiceUncollectible(e.Ctx, e.Pool, org.ID, inv.ID, testkit.Date("2026-03-10"))
	if err != nil {
		t.Fatalf("mark: %v", err)
	}
	if marked.WrittenOffOn == nil || !marked.WrittenOffOn.Equal(testkit.Date("2026-03-10")) || marked.WrittenOffAmount != 30000 {
		t.Fatalf("written_off_on %v amount %d, want 2026-03-10 30000", marked.WrittenOffOn, marked.WrittenOffAmount)
	}
}

func TestReinstateClearsWriteOff(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	inv := e.Invoice(org, e.Contact(org).ID, testkit.Amount(50000), testkit.Issued(time.Now()))
	if _, err := core.MarkInvoiceUncollectible(e.Ctx, e.Pool, org.ID, inv.ID, testkit.Date("2026-03-10")); err != nil {
		t.Fatalf("mark: %v", err)
	}
	got, err := core.ReinstateInvoice(e.Ctx, e.Pool, org.ID, inv.ID)
	if err != nil {
		t.Fatalf("reinstate: %v", err)
	}
	if got.WrittenOffOn != nil || got.WrittenOffAmount != 0 {
		t.Fatalf("written_off_on %v amount %d, want nil 0", got.WrittenOffOn, got.WrittenOffAmount)
	}
}

// TestWriteOffBackfill runs migration 0021's backfill statement against an
// invoice written off before the columns existed.
func TestWriteOffBackfill(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	inv := e.Invoice(org, e.Contact(org).ID, testkit.Amount(50000), testkit.Issued(time.Now()))
	if _, _, err := core.RecordPayment(e.Ctx, e.Pool, org.ID, core.NewPayment{InvoiceID: inv.ID, Amount: 5000, PaidOn: time.Now(), Method: "cash"}); err != nil {
		t.Fatalf("RecordPayment: %v", err)
	}
	if _, err := e.Pool.Exec(e.Ctx, `UPDATE invoices SET status = 'uncollectible', written_off = true, written_off_on = NULL, written_off_amount = 0 WHERE id = $1`, inv.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Pool.Exec(e.Ctx, `INSERT INTO audit_log (org_id, entity, entity_id, action, at) VALUES ($1, 'invoice', $2, 'mark_uncollectible', '2026-02-01T09:00:00+10:00'), ($1, 'invoice', $2, 'mark_uncollectible', '2026-02-03T09:00:00+10:00')`, org.ID, inv.ID); err != nil {
		t.Fatal(err)
	}
	sql, err := os.ReadFile("../db/migrations/0021_invoice_written_off_detail.sql")
	if err != nil {
		t.Fatal(err)
	}
	backfill := string(sql)[strings.Index(string(sql), "UPDATE invoices"):]
	if _, err := e.Pool.Exec(e.Ctx, backfill); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	got, err := core.GetInvoice(e.Ctx, e.Pool, org.ID, inv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.WrittenOffOn == nil || got.WrittenOffOn.Format("2006-01-02") != "2026-02-03" || got.WrittenOffAmount != 45000 {
		t.Fatalf("written_off_on %v amount %d, want 2026-02-03 45000", got.WrittenOffOn, got.WrittenOffAmount)
	}
}
