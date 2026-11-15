//go:build integration

package httpapi_test

import (
	"bytes"
	"net/http"
	"testing"

	"shigoto/internal/auth"
	"shigoto/internal/importers"
	"shigoto/internal/testkit"
)

const invoiceMapping = `{"columns":{"number":"Invoice","customer":"Customer","issue_date":"Date","due_date":"Due",` +
	`"subtotal":"Net","gst":"Tax","total":"Total","outstanding":"Outstanding","paid_date":"Paid"},"date_format":"2006-01-02"}`

const invoiceCSV = "Invoice,Customer,Date,Due,Net,Tax,Total,Outstanding,Paid\n" +
	"ACC-1,Acme Pty Ltd,2025-03-01,2025-03-15,1000.00,100.00,1100.00,0,2025-03-10\n" +
	"ACC-2,Acme Pty Ltd,2025-03-02,2025-03-16,500.00,50.00,550.00,550.00,\n"

type importBody struct {
	New        int                 `json:"new"`
	Existing   int                 `json:"existing"`
	TotalCents int64               `json:"total_cents"`
	Error      string              `json:"error"`
	Problems   []importers.Problem `json:"problems"`
}

func importOwner(t *testing.T) (*testkit.Env, testkit.Session, int64) {
	e := testkit.New(t)
	org := e.Org("acme")
	e.Settings(org, testkit.GSTRegistered(true))
	e.Contact(org, testkit.Kind("client"), testkit.Name("Acme Pty Ltd"))
	owner := e.User("owner@example.com")
	e.Member(owner, org, auth.Owner)
	return e, e.Login(owner, e.Handler()), org.ID
}

func TestInvoiceImportPreviewThenApply(t *testing.T) {
	e, s, orgID := importOwner(t)
	tables := []string{"invoices", "invoice_lines", "payments", "invoice_counters", "audit_log"}
	before := e.Snapshot(tables...)

	resp := s.UploadForm("/api/v1/orgs/acme/invoices/import/preview", "inv.csv", []byte(invoiceCSV), map[string]string{"mapping": invoiceMapping})
	var body importBody
	resp.JSON(&body)
	if resp.Code != http.StatusOK || body.New != 2 || body.TotalCents != 165000 || len(body.Problems) != 0 {
		t.Fatalf("preview = %d %+v", resp.Code, body)
	}
	if e.Snapshot(tables...) != before {
		t.Fatal("preview wrote to the database")
	}

	resp = s.UploadForm("/api/v1/orgs/acme/invoices/import", "inv.csv", []byte(invoiceCSV), map[string]string{"mapping": invoiceMapping})
	body = importBody{}
	resp.JSON(&body)
	if resp.Code != http.StatusOK || body.New != 2 {
		t.Fatalf("apply = %d %s", resp.Code, resp.Body)
	}
	if n := e.Count("invoices", "org_id = $1 AND external_ref LIKE 'import:%'", orgID); n != 2 {
		t.Errorf("imported invoices = %d, want 2", n)
	}

	resp = s.UploadForm("/api/v1/orgs/acme/invoices/import", "inv.csv", []byte(invoiceCSV), map[string]string{"mapping": invoiceMapping})
	body = importBody{}
	resp.JSON(&body)
	if resp.Code != http.StatusOK || body.New != 0 || body.Existing != 2 {
		t.Fatalf("re-apply = %d %+v, want 0 new 2 existing", resp.Code, body)
	}
}

func TestInvoiceImportApplyWithProblemsWritesNothing(t *testing.T) {
	e, s, _ := importOwner(t)
	tables := []string{"invoices", "invoice_lines", "payments", "invoice_counters", "audit_log"}
	before := e.Snapshot(tables...)
	csv := invoiceCSV + "ACC-3,Nobody,2025-03-02,2025-03-16,1.00,0,1.00,1.00,\n"

	resp := s.UploadForm("/api/v1/orgs/acme/invoices/import", "inv.csv", []byte(csv), map[string]string{"mapping": invoiceMapping})
	var body importBody
	resp.JSON(&body)
	if resp.Code != http.StatusUnprocessableEntity || body.Error != "import has problems; preview again" {
		t.Fatalf("apply = %d %s", resp.Code, resp.Body)
	}
	want := []importers.Problem{{Row: 4, Field: "customer", Message: `no client contact named "Nobody"`}}
	if len(body.Problems) != 1 || body.Problems[0] != want[0] {
		t.Errorf("problems = %+v, want %+v", body.Problems, want)
	}
	if e.Snapshot(tables...) != before {
		t.Error("apply with problems wrote to the database")
	}
}

func TestInvoiceImportParseAndDatabaseProblemsMerge(t *testing.T) {
	_, s, _ := importOwner(t)
	csv := "Invoice,Customer,Date,Due,Net,Tax,Total,Outstanding,Paid\n" +
		"ACC-1,Nobody,2025-03-01,2025-03-15,1.00,0,1.00,1.00,\n" +
		"ACC-2,Acme Pty Ltd,2025-03-01,2025-03-15,1.00,0,2.00,2.00,\n"
	resp := s.UploadForm("/api/v1/orgs/acme/invoices/import/preview", "inv.csv", []byte(csv), map[string]string{"mapping": invoiceMapping})
	var body importBody
	resp.JSON(&body)
	want := []importers.Problem{
		{Row: 2, Field: "customer", Message: `no client contact named "Nobody"`},
		{Row: 3, Field: "total", Message: "total must equal subtotal plus GST"},
	}
	if resp.Code != http.StatusOK || len(body.Problems) != 2 || body.Problems[0] != want[0] || body.Problems[1] != want[1] {
		t.Fatalf("preview = %d %+v, want problems %+v", resp.Code, body, want)
	}
}

func TestInvoiceImportFileProblems(t *testing.T) {
	_, s, _ := importOwner(t)
	resp := s.UploadForm("/api/v1/orgs/acme/invoices/import/preview", "inv.csv", []byte("Wrong\n1\n"), map[string]string{"mapping": invoiceMapping})
	var body importBody
	resp.JSON(&body)
	if resp.Code != http.StatusOK || len(body.Problems) == 0 || body.Problems[0].Row != 0 {
		t.Fatalf("preview of wrong headers = %d %+v, want whole-file problems", resp.Code, body)
	}
}

func TestInvoiceImportUploadErrors(t *testing.T) {
	_, s, _ := importOwner(t)
	path := "/api/v1/orgs/acme/invoices/import/preview"

	big := bytes.Repeat([]byte("x"), 6<<20)
	if resp := s.UploadForm(path, "big.csv", big, map[string]string{"mapping": invoiceMapping}); resp.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("6 MiB upload = %d, want 413", resp.Code)
	}
	if resp := s.UploadForm(path, "inv.csv", []byte(invoiceCSV), nil); resp.Code != http.StatusUnprocessableEntity || !bytes.Contains(resp.Body, []byte("mapping is required")) {
		t.Errorf("missing mapping = %d %s, want 422 mapping is required", resp.Code, resp.Body)
	}
	if resp := s.UploadForm(path, "inv.csv", []byte(invoiceCSV), map[string]string{"mapping": "{"}); resp.Code != http.StatusUnprocessableEntity || !bytes.Contains(resp.Body, []byte("invalid mapping")) {
		t.Errorf("bad mapping = %d %s, want 422 invalid mapping", resp.Code, resp.Body)
	}
	if resp := s.Do(http.MethodPost, path, map[string]string{}); resp.Code != http.StatusBadRequest {
		t.Errorf("JSON body = %d, want 400", resp.Code)
	}
}
