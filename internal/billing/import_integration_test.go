//go:build integration

package billing_test

import (
	"testing"
	"time"

	"shigoto/internal/billing"
	"shigoto/internal/core"
	"shigoto/internal/importers"
	"shigoto/internal/testkit"
)

func day(s string) time.Time {
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return d
}

func dayPtr(s string) *time.Time { d := day(s); return &d }

// importRow is a valid invoice row: 1000 + 100 GST, unpaid, issued on issue.
func importRow(row int, number, issue string) billing.InvoiceRow {
	return billing.InvoiceRow{
		Row: row, Number: number, Customer: "Acme Pty Ltd",
		IssueDate: day(issue), DueDate: day(issue).AddDate(0, 0, 14),
		Subtotal: 1000, GST: 100, Total: 1100, Outstanding: 1100,
	}
}

func importSetup(t *testing.T) (*testkit.Env, core.Org, int64) {
	e := testkit.New(t)
	org := e.Org("acme")
	e.Settings(org, testkit.GSTRegistered(true))
	contactID := e.Contact(org, testkit.Kind("client"), testkit.Name("Acme Pty Ltd")).ID
	return e, org, contactID
}

func TestInvoiceImportPaidPartAndOpen(t *testing.T) {
	e, org, contactID := importSetup(t)
	paid := importRow(2, "ACC-3", "2025-03-03")
	paid.Outstanding, paid.PaidDate = 0, dayPtr("2025-03-20")
	part := importRow(3, "ACC-1", "2025-03-01")
	part.Outstanding, part.PaidDate = 500, dayPtr("2025-04-02")
	open := importRow(4, "ACC-2", "2025-03-02")

	res, err := billing.ImportInvoices(e.Ctx, e.Pool, org.ID, nil, []billing.InvoiceRow{paid, part, open}, true)
	if err != nil {
		t.Fatalf("ImportInvoices: %v", err)
	}
	if len(res.Problems) != 0 || res.New != 3 || res.Existing != 0 || res.TotalCents != 3300 {
		t.Fatalf("result = %+v, want 3 new, 3300 total, no problems", res)
	}

	type got struct {
		number         int
		status         string
		paid           int64
		paidOn         *time.Time
		providerRef    *string
		contactID      int64
		issue          time.Time
		gstCode        string
		lineGST, total int64
	}
	byRef := map[string]got{}
	rows, err := e.Pool.Query(e.Ctx, `
		SELECT i.external_ref, i.number, i.status, coalesce(sum(p.amount),0), max(p.paid_on), i.provider_ref, i.contact_id, i.issue_date,
			(SELECT gst_code FROM invoice_lines WHERE invoice_id = i.id), (SELECT gst_amount FROM invoice_lines WHERE invoice_id = i.id), i.total
		FROM invoices i LEFT JOIN payments p ON p.invoice_id = i.id
		WHERE i.org_id = $1 GROUP BY i.id`, org.ID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var ref string
		var g got
		if err := rows.Scan(&ref, &g.number, &g.status, &g.paid, &g.paidOn, &g.providerRef, &g.contactID, &g.issue, &g.gstCode, &g.lineGST, &g.total); err != nil {
			t.Fatal(err)
		}
		byRef[ref] = g
	}
	rows.Close()

	// Numbers ascend by issue date: ACC-1 (1 Mar), ACC-2 (2 Mar), ACC-3 (3 Mar).
	for ref, want := range map[string]struct {
		number int
		status string
		paid   int64
		paidOn string
	}{
		"import:ACC-1": {1, "open", 600, "2025-04-02"},
		"import:ACC-2": {2, "open", 0, ""},
		"import:ACC-3": {3, "paid", 1100, "2025-03-20"},
	} {
		g, ok := byRef[ref]
		if !ok {
			t.Fatalf("missing %s", ref)
		}
		if g.number != want.number || g.status != want.status || g.paid != want.paid {
			t.Errorf("%s: number/status/paid = %d/%s/%d, want %d/%s/%d", ref, g.number, g.status, g.paid, want.number, want.status, want.paid)
		}
		if want.paidOn != "" && (g.paidOn == nil || !g.paidOn.Equal(day(want.paidOn))) {
			t.Errorf("%s: paid_on = %v, want %s", ref, g.paidOn, want.paidOn)
		}
		if g.providerRef != nil || g.contactID != contactID || g.gstCode != "GST" || g.lineGST != 100 || g.total != 1100 {
			t.Errorf("%s: provider_ref/contact/gst/total = %v/%d/%s/%d/%d", ref, g.providerRef, g.contactID, g.gstCode, g.lineGST, g.total)
		}
	}
	if n := e.Count("payments", "org_id = $1 AND provider_ref = 'import:ACC-1' AND method = 'other'", org.ID); n != 1 {
		t.Errorf("ACC-1 import payments = %d, want 1", n)
	}
	if n := e.Count("audit_log", "org_id = $1 AND entity = 'invoice' AND action = 'import'", org.ID); n != 3 {
		t.Errorf("import audit rows = %d, want 3", n)
	}
}

func TestInvoiceImportPreviewWritesNothing(t *testing.T) {
	e, org, _ := importSetup(t)
	before := e.Snapshot("invoices", "invoice_lines", "payments", "invoice_counters", "audit_log")
	res, err := billing.ImportInvoices(e.Ctx, e.Pool, org.ID, nil,
		[]billing.InvoiceRow{importRow(2, "A", "2025-03-01"), importRow(3, "B", "2025-03-01")}, false)
	if err != nil {
		t.Fatalf("ImportInvoices: %v", err)
	}
	if res.New != 2 || res.TotalCents != 2200 || len(res.Problems) != 0 {
		t.Errorf("result = %+v, want 2 new, 2200", res)
	}
	if after := e.Snapshot("invoices", "invoice_lines", "payments", "invoice_counters", "audit_log"); after != before {
		t.Error("preview wrote to the database")
	}
}

func TestInvoiceImportCustomerMatching(t *testing.T) {
	e, org, _ := importSetup(t)
	e.Contact(org, testkit.Kind("client"), testkit.Name("Twin"))
	e.Contact(org, testkit.Kind("client"), testkit.Name("twin "))
	e.Contact(org, testkit.Kind("tenant"), testkit.Name("Tenant Only"))

	match := importRow(2, "A", "2025-03-01")
	match.Customer = "  ACME pty ltd "
	unknown := importRow(3, "B", "2025-03-01")
	unknown.Customer = "Nobody"
	twin := importRow(4, "C", "2025-03-01")
	twin.Customer = "Twin"
	tenant := importRow(5, "D", "2025-03-01")
	tenant.Customer = "Tenant Only"

	before := e.Snapshot("invoices")
	res, err := billing.ImportInvoices(e.Ctx, e.Pool, org.ID, nil, []billing.InvoiceRow{match, unknown, twin, tenant}, true)
	if err != nil {
		t.Fatal(err)
	}
	want := []importers.Problem{
		{Row: 3, Field: "customer", Message: `no client contact named "Nobody"`},
		{Row: 4, Field: "customer", Message: `more than one client contact named "Twin"`},
		{Row: 5, Field: "customer", Message: `no client contact named "Tenant Only"`},
	}
	assertProblems(t, res.Problems, want)
	if e.Snapshot("invoices") != before {
		t.Error("apply with problems wrote invoices")
	}
}

func TestInvoiceImportGSTNotRegistered(t *testing.T) {
	e, org, _ := importSetup(t)
	e.Settings(org, testkit.GSTRegistered(false))
	free := importRow(3, "B", "2025-03-01")
	free.GST, free.Total, free.Outstanding = 0, 1000, 1000

	res, err := billing.ImportInvoices(e.Ctx, e.Pool, org.ID, nil, []billing.InvoiceRow{importRow(2, "A", "2025-03-01"), free}, false)
	if err != nil {
		t.Fatal(err)
	}
	assertProblems(t, res.Problems, []importers.Problem{
		{Row: 2, Field: "gst", Message: "GST charged but organisation is not GST-registered"},
	})
}

func TestInvoiceImportOverlap(t *testing.T) {
	e, org, _ := importSetup(t)
	a, b := importRow(2, "A", "2025-03-01"), importRow(3, "B", "2025-03-02")
	b.Outstanding, b.PaidDate = 0, dayPtr("2025-03-05")
	if _, err := billing.ImportInvoices(e.Ctx, e.Pool, org.ID, nil, []billing.InvoiceRow{a, b}, true); err != nil {
		t.Fatal(err)
	}

	c := importRow(4, "C", "2025-03-03")
	res, err := billing.ImportInvoices(e.Ctx, e.Pool, org.ID, nil, []billing.InvoiceRow{a, b, c}, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.New != 1 || res.Existing != 2 || len(res.Problems) != 0 {
		t.Fatalf("overlap result = %+v, want 1 new 2 existing", res)
	}
	if n := e.Count("invoices", "org_id = $1", org.ID); n != 3 {
		t.Errorf("invoices = %d, want 3", n)
	}

	changed := a
	changed.Outstanding, changed.PaidDate = 0, dayPtr("2025-03-09")
	res, err = billing.ImportInvoices(e.Ctx, e.Pool, org.ID, nil, []billing.InvoiceRow{changed}, false)
	if err != nil {
		t.Fatal(err)
	}
	assertProblems(t, res.Problems, []importers.Problem{
		{Row: 2, Field: "number", Message: "already imported with different values"},
	})
}

func assertProblems(t *testing.T, got, want []importers.Problem) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("problems = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("problem %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// A voided imported invoice frees its number for a corrected re-import.
func TestInvoiceImportAfterVoid(t *testing.T) {
	e, org, _ := importSetup(t)
	row := importRow(2, "A", "2025-03-01")
	if _, err := billing.ImportInvoices(e.Ctx, e.Pool, org.ID, nil, []billing.InvoiceRow{row}, true); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := e.Pool.QueryRow(e.Ctx, `SELECT id FROM invoices WHERE org_id = $1 AND external_ref = 'import:A'`, org.ID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := e.BillingService().Void(e.Ctx, org.ID, "acme", id); err != nil {
		t.Fatalf("Void: %v", err)
	}

	corrected := row
	corrected.Subtotal, corrected.GST, corrected.Total, corrected.Outstanding = 2000, 200, 2200, 2200
	res, err := billing.ImportInvoices(e.Ctx, e.Pool, org.ID, nil, []billing.InvoiceRow{corrected}, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.New != 1 || len(res.Problems) != 0 {
		t.Fatalf("re-import after void = %+v, want 1 new", res)
	}
	if n := e.Count("invoices", "org_id = $1 AND external_ref = 'import:A' AND status = 'open' AND total = 2200", org.ID); n != 1 {
		t.Errorf("corrected invoice rows = %d, want 1", n)
	}
	if n := e.Count("invoices", "id = $1 AND status = 'void' AND external_ref LIKE 'voided:%'", id); n != 1 {
		t.Errorf("voided invoice keeps a voided: reference = %d, want 1", n)
	}
}
