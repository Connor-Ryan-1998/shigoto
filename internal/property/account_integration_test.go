//go:build integration

package property_test

import (
	"testing"
	"time"

	"shigoto/internal/core"
	"shigoto/internal/property"
	"shigoto/internal/testkit"
)

// accountFixture is a weekly 62000 lease starting 2026-11-05 with three
// issued rent invoices (due 11-05, 11-12, 11-19).
type accountFixture struct {
	e        *testkit.Env
	org      core.Org
	lease    property.Lease
	tenant   int64
	invoices []int64
}

func newAccountFixture(t *testing.T, opts ...testkit.LeaseOption) accountFixture {
	t.Helper()
	e := testkit.New(t)
	org := e.Org("acme")
	e.Settings(org, testkit.RentRun(14, false))
	propertyID := e.Property(org).ID
	tenant := e.Contact(org, testkit.Kind("tenant"), testkit.Name("Tina Tenant")).ID
	start := testkit.Date("2026-11-05")
	lease := e.Lease(org, propertyID, tenant, append([]testkit.LeaseOption{testkit.Start(start), testkit.Rent(62000)}, opts...)...)
	if _, err := property.RunRent(e.Ctx, e.BillingService(), org.ID, "acme", start); err != nil {
		t.Fatalf("RunRent: %v", err)
	}
	ledger, err := property.GetLedger(e.Ctx, e.Pool, org.ID, lease.ID, start)
	if err != nil {
		t.Fatalf("GetLedger: %v", err)
	}
	if len(ledger.Periods) < 3 {
		t.Fatalf("periods = %d, want >= 3", len(ledger.Periods))
	}
	var ids []int64
	for _, p := range ledger.Periods[:3] {
		ids = append(ids, *p.InvoiceID)
		e.Issue(org, *p.InvoiceID, start)
	}
	return accountFixture{e: e, org: org, lease: lease, tenant: tenant, invoices: ids}
}

func (f accountFixture) pay(t *testing.T, invoiceID int64, amount core.Cents, on string) {
	t.Helper()
	if _, _, err := core.RecordPayment(f.e.Ctx, f.e.Pool, f.org.ID, core.NewPayment{
		InvoiceID: invoiceID, Amount: amount, PaidOn: testkit.Date(on), Method: "bank_transfer",
	}); err != nil {
		t.Fatalf("RecordPayment: %v", err)
	}
}

func (f accountFixture) account(t *testing.T) property.Account {
	t.Helper()
	a, err := property.LeaseAccount(f.e.Ctx, f.e.Pool, f.org.ID, f.lease.ID)
	if err != nil {
		t.Fatalf("LeaseAccount: %v", err)
	}
	return a
}

func TestAccountRunningBalance(t *testing.T) {
	f := newAccountFixture(t)
	f.pay(t, f.invoices[0], 62000, "2026-11-05")
	f.pay(t, f.invoices[1], 62000, "2026-11-11")

	a := f.account(t)
	if a.Balance != 62000 {
		t.Fatalf("Balance = %d, want 62000", a.Balance)
	}
	want := []struct {
		date    string
		kind    property.EntryKind
		balance core.Cents
	}{
		{"2026-11-05", property.EntryCharge, 62000},
		{"2026-11-05", property.EntryPayment, 0},
		{"2026-11-11", property.EntryPayment, -62000},
		{"2026-11-12", property.EntryCharge, 0},
		{"2026-11-19", property.EntryCharge, 62000},
	}
	if len(a.Entries) != len(want) {
		t.Fatalf("entries = %+v", a.Entries)
	}
	for i, w := range want {
		got := a.Entries[i]
		if got.Date.Format("2006-01-02") != w.date || got.Kind != w.kind || got.Balance != w.balance {
			t.Errorf("entry %d = %s %s %d, want %s %s %d", i, got.Date.Format("2006-01-02"), got.Kind, got.Balance, w.date, w.kind, w.balance)
		}
	}
	if a.Entries[0].Description != "Rent 5–11 Nov 2026" || a.Entries[0].Debit != 62000 {
		t.Errorf("first charge = %+v", a.Entries[0])
	}
	if a.Entries[1].Description != "Payment · Bank transfer" || a.Entries[1].Credit != 62000 || a.Entries[1].PaymentID == nil {
		t.Errorf("first payment = %+v", a.Entries[1])
	}
}

func TestAccountExcludesDraftVoidBond(t *testing.T) {
	f := newAccountFixture(t, testkit.Bond(248000))
	// A fourth period exists as a draft; void the third invoice.
	if _, err := core.VoidInvoice(f.e.Ctx, f.e.Pool, f.org.ID, f.invoices[2]); err != nil {
		t.Fatalf("VoidInvoice: %v", err)
	}
	lease, err := property.GetLease(f.e.Ctx, f.e.Pool, f.org.ID, f.lease.ID)
	if err != nil {
		t.Fatal(err)
	}
	if lease.BondInvoiceID != nil {
		f.e.Issue(f.org, *lease.BondInvoiceID, testkit.Date("2026-11-05"))
	}
	a := f.account(t)
	if len(a.Entries) != 2 || a.Balance != 124000 {
		t.Fatalf("entries %+v balance %d, want two charges totalling 124000", a.Entries, a.Balance)
	}
}

func TestAccountWriteOffCredit(t *testing.T) {
	f := newAccountFixture(t)
	f.pay(t, f.invoices[0], 20000, "2026-11-06")
	if _, err := core.MarkInvoiceUncollectible(f.e.Ctx, f.e.Pool, f.org.ID, f.invoices[0], testkit.Date("2026-11-20")); err != nil {
		t.Fatalf("mark: %v", err)
	}
	a := f.account(t)
	last := a.Entries[len(a.Entries)-1]
	if last.Kind != property.EntryWriteOff || last.Credit != 42000 || last.Description != "Written off" || last.Date.Format("2006-01-02") != "2026-11-20" {
		t.Fatalf("last entry = %+v", last)
	}
	// 3×62000 − 20000 paid − 42000 written off.
	if a.Balance != 124000 {
		t.Fatalf("Balance = %d, want 124000", a.Balance)
	}
}

// A payment on a written-off invoice recovers the debt: the write-off is
// reversed for the amount recovered, so the tenant is not shown in credit.
func TestAccountPaymentAfterWriteOff(t *testing.T) {
	f := newAccountFixture(t)
	if _, err := core.MarkInvoiceUncollectible(f.e.Ctx, f.e.Pool, f.org.ID, f.invoices[0], testkit.Date("2026-11-20")); err != nil {
		t.Fatalf("mark: %v", err)
	}
	f.pay(t, f.invoices[0], 62000, "2026-11-25")
	a := f.account(t)
	n := len(a.Entries)
	reversal, payment := a.Entries[n-2], a.Entries[n-1]
	if reversal.Kind != property.EntryWriteOffReversed || reversal.Debit != 62000 || reversal.Description != "Write-off reversed" {
		t.Fatalf("reversal = %+v", reversal)
	}
	if payment.Kind != property.EntryPayment || payment.Credit != 62000 {
		t.Fatalf("payment = %+v", payment)
	}
	if a.Balance != 124000 {
		t.Fatalf("Balance = %d, want 124000 (two unpaid weeks)", a.Balance)
	}
}

func TestAccountOverpaymentShowsCredit(t *testing.T) {
	f := newAccountFixture(t)
	for i, id := range f.invoices {
		f.pay(t, id, 62000, "2026-11-0"+string(rune('5'+i)))
	}
	f.pay(t, f.invoices[2], 10000, "2026-11-09")
	if a := f.account(t); a.Balance != -10000 {
		t.Fatalf("Balance = %d, want -10000", a.Balance)
	}
}

func TestStatementOpeningClosing(t *testing.T) {
	f := newAccountFixture(t)
	f.pay(t, f.invoices[0], 62000, "2026-11-05")
	f.pay(t, f.invoices[1], 30000, "2026-11-13")
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)

	s, err := property.BuildStatement(f.e.Ctx, f.e.Pool, f.org.ID, f.lease.ID, testkit.Date("2026-11-10"), testkit.Date("2026-11-15"), now)
	if err != nil {
		t.Fatalf("BuildStatement: %v", err)
	}
	if s.Opening != 0 || s.TotalDebits != 62000 || s.TotalCredits != 30000 || s.Closing != 32000 {
		t.Fatalf("opening %d debits %d credits %d closing %d", s.Opening, s.TotalDebits, s.TotalCredits, s.Closing)
	}
	if len(s.Entries) != 2 {
		t.Fatalf("entries = %+v", s.Entries)
	}
	if s.OrgName == "" || len(s.TenantNames) != 1 || s.TenantNames[0] != "Tina Tenant" || s.PropertyAddress == "" || !s.GeneratedAt.Equal(now) {
		t.Fatalf("header = %+v", s)
	}

	before, err := property.BuildStatement(f.e.Ctx, f.e.Pool, f.org.ID, f.lease.ID, testkit.Date("2026-01-01"), testkit.Date("2026-06-30"), now)
	if err != nil {
		t.Fatalf("BuildStatement(before): %v", err)
	}
	if before.Opening != 0 || before.Closing != 0 || len(before.Entries) != 0 {
		t.Fatalf("before lease = %+v", before)
	}

	after, err := property.BuildStatement(f.e.Ctx, f.e.Pool, f.org.ID, f.lease.ID, testkit.Date("2026-11-16"), testkit.Date("2026-12-31"), now)
	if err != nil {
		t.Fatalf("BuildStatement(after): %v", err)
	}
	if after.Opening != 32000 || after.Closing != 94000 {
		t.Fatalf("after opening %d closing %d, want 32000 94000", after.Opening, after.Closing)
	}
}

func TestLeaseHasContactCoTenant(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	other := e.Org("beta")
	p := e.Property(org).ID
	primary := e.Contact(org, testkit.Kind("tenant")).ID
	co := e.Contact(org, testkit.Kind("tenant")).ID
	stranger := e.Contact(org, testkit.Kind("tenant")).ID
	lease := e.Lease(org, p, primary, testkit.CoTenant(co))

	for name, c := range map[string]struct {
		org     int64
		contact int64
		want    bool
	}{
		"primary":   {org.ID, primary, true},
		"co-tenant": {org.ID, co, true},
		"stranger":  {org.ID, stranger, false},
		"other org": {other.ID, primary, false},
	} {
		got, err := property.LeaseHasContact(e.Ctx, e.Pool, c.org, lease.ID, c.contact)
		if err != nil || got != c.want {
			t.Errorf("%s: got %v err %v, want %v", name, got, err, c.want)
		}
	}
}
