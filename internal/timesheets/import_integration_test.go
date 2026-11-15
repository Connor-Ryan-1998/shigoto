//go:build integration

package timesheets_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"shigoto/internal/auth"
	"shigoto/internal/billing"
	"shigoto/internal/core"
	"shigoto/internal/importers"
	"shigoto/internal/testkit"
	"shigoto/internal/timesheets"
)

var importDay = time.Date(2025, 3, 3, 0, 0, 0, 0, time.UTC)
var importToday = time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)

type importFixture struct {
	e        *testkit.Env
	org      core.Org
	clientID int64
	owner    testkit.User
	task     timesheets.Task
	lineID   int64
}

// importFixture: owner, a completed "Website" project with task "Build",
// and imported invoice ACC-1 (one line).
func newImportFixture(t *testing.T) importFixture {
	e := testkit.New(t)
	org := e.Org("acme")
	owner := e.User("owner@example.com")
	e.Member(owner, org, auth.Owner)
	clientID := e.Contact(org, testkit.Kind("client"), testkit.Name("Acme Pty Ltd")).ID
	project := e.Project(org, clientID, testkit.ProjectName("Website"), testkit.ProjectStatus("completed"))
	task := e.Task(org, project.ID, testkit.TaskName("Build"))

	if _, err := billing.ImportInvoices(e.Ctx, e.Pool, org.ID, nil, []billing.InvoiceRow{{
		Row: 2, Number: "ACC-1", Customer: "Acme Pty Ltd", IssueDate: importDay, DueDate: importDay,
		Subtotal: 1000, Total: 1000, Outstanding: 1000,
	}}, true); err != nil {
		t.Fatalf("importing invoice: %v", err)
	}
	var lineID int64
	if err := e.Pool.QueryRow(e.Ctx, `SELECT l.id FROM invoice_lines l JOIN invoices i ON i.id = l.invoice_id WHERE i.external_ref = 'import:ACC-1'`).Scan(&lineID); err != nil {
		t.Fatalf("finding line: %v", err)
	}
	return importFixture{e: e, org: org, clientID: clientID, owner: owner, task: task, lineID: lineID}
}

func entryRow(row int, person, invoice string) timesheets.EntryRow {
	return timesheets.EntryRow{
		Row: row, WorkDate: importDay, Minutes: 90, Project: "website", Task: "BUILD",
		Person: person, Invoice: invoice, Note: "work", Ref: fmt.Sprintf("import:ref-%d", row),
	}
}

func TestEntryImportLinksToInvoiceLine(t *testing.T) {
	f := newImportFixture(t)
	res, err := timesheets.ImportEntries(f.e.Ctx, f.e.Pool, f.org.ID, nil,
		[]timesheets.EntryRow{entryRow(2, "OWNER@example.com", "ACC-1"), entryRow(3, "owner@example.com", "ACC-1")}, true)
	if err != nil {
		t.Fatalf("ImportEntries: %v", err)
	}
	if res.New != 2 || res.TotalMinutes != 180 || len(res.Problems) != 0 {
		t.Fatalf("result = %+v", res)
	}
	n := f.e.Count("time_entries", "org_id = $1 AND task_id = $2 AND user_id = $3 AND invoice_line_id = $4 AND minutes = 90 AND work_date = $5 AND external_ref LIKE 'import:%'",
		f.org.ID, f.task.ID, f.owner.ID, f.lineID, importDay)
	if n != 2 {
		t.Errorf("linked imported entries = %d, want 2", n)
	}
	if a := f.e.Count("audit_log", "org_id = $1 AND entity = 'time_entry' AND action = 'import'", f.org.ID); a != 2 {
		t.Errorf("audit rows = %d, want 2", a)
	}
}

func TestEntryImportBillingRunIgnoresImported(t *testing.T) {
	f := newImportFixture(t)
	// A billing run only offers active projects' time; reopen the project
	// so the test proves the billed link, not the status, keeps it out.
	if _, err := f.e.Pool.Exec(f.e.Ctx, `UPDATE projects SET status = 'in_progress' WHERE org_id = $1`, f.org.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := timesheets.ImportEntries(f.e.Ctx, f.e.Pool, f.org.ID, nil, []timesheets.EntryRow{entryRow(2, "owner@example.com", "ACC-1")}, true); err != nil {
		t.Fatal(err)
	}
	preview, err := timesheets.PreviewBilling(f.e.Ctx, f.e.Pool, f.org.ID, timesheets.PreviewInput{
		ClientContactID: f.clientID, From: importDay.AddDate(0, -1, 0), To: importToday,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Projects) != 0 {
		t.Errorf("billing preview = %+v, want nothing to bill", preview.Projects)
	}
}

func TestEntryImportBeforeInvoices(t *testing.T) {
	f := newImportFixture(t)
	before := f.e.Snapshot("time_entries", "audit_log")
	res, err := timesheets.ImportEntries(f.e.Ctx, f.e.Pool, f.org.ID, nil,
		[]timesheets.EntryRow{entryRow(2, "owner@example.com", "ACC-9"), entryRow(3, "owner@example.com", "ACC-1")}, true)
	if err != nil {
		t.Fatal(err)
	}
	want := []importers.Problem{{Row: 2, Field: "invoice", Message: "invoice ACC-9 has not been imported"}}
	if len(res.Problems) != 1 || res.Problems[0] != want[0] {
		t.Fatalf("problems = %+v, want %+v", res.Problems, want)
	}
	if f.e.Snapshot("time_entries", "audit_log") != before {
		t.Error("apply with problems wrote entries")
	}
}

func TestEntryImportMatchingProblems(t *testing.T) {
	f := newImportFixture(t)
	f.e.User("stranger@example.com") // a user, but not a member of acme
	noProject := entryRow(2, "owner@example.com", "ACC-1")
	noProject.Project = "Nope"
	noTask := entryRow(3, "owner@example.com", "ACC-1")
	noTask.Task = "Nope"
	notMember := entryRow(4, "stranger@example.com", "ACC-1")

	normal := f.e.Invoice(f.org, f.clientID)
	if _, err := f.e.Pool.Exec(f.e.Ctx, `UPDATE invoices SET external_ref = 'import:TWO' WHERE id = $1`, normal.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.e.Pool.Exec(f.e.Ctx, `INSERT INTO invoice_lines (invoice_id, org_id, description, quantity_milli, unit_amount, amount) VALUES ($1, $2, 'extra', 1000, 1, 1)`, normal.ID, f.org.ID); err != nil {
		t.Fatal(err)
	}
	twoLines := entryRow(5, "owner@example.com", "TWO")

	res, err := timesheets.ImportEntries(f.e.Ctx, f.e.Pool, f.org.ID, nil,
		[]timesheets.EntryRow{noProject, noTask, notMember, twoLines}, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []importers.Problem{
		{Row: 2, Field: "project", Message: `no project named "Nope"`},
		{Row: 3, Field: "task", Message: `no task named "Nope" in Website`},
		{Row: 4, Field: "person", Message: `no member with email "stranger@example.com"`},
		{Row: 5, Field: "invoice", Message: "invoice TWO is not an imported single-line invoice"},
	}
	if len(res.Problems) != len(want) {
		t.Fatalf("problems = %+v, want %+v", res.Problems, want)
	}
	for i := range want {
		if res.Problems[i] != want[i] {
			t.Errorf("problem %d = %+v, want %+v", i, res.Problems[i], want[i])
		}
	}
}

func TestEntryImportOtherUser(t *testing.T) {
	f := newImportFixture(t)
	staff := f.e.User("staff@example.com")
	f.e.Member(staff, f.org, auth.Staff)
	if _, err := timesheets.ImportEntries(f.e.Ctx, f.e.Pool, f.org.ID, nil, []timesheets.EntryRow{entryRow(2, "staff@example.com", "ACC-1")}, true); err != nil {
		t.Fatal(err)
	}
	if n := f.e.Count("time_entries", "org_id = $1 AND user_id = $2", f.org.ID, staff.ID); n != 1 {
		t.Errorf("staff entries = %d, want 1", n)
	}
}

const entryCSV = "Date,Hours,Project,Task,Employee,Invoice,Description\n" +
	"2025-03-03,1.5,Website,Build,owner@example.com,ACC-1,Design\n" +
	"2025-03-03,1.5,Website,Build,owner@example.com,ACC-1,Design\n"

const entryMapping = `{"columns":{"date":"Date","hours":"Hours","project":"Project","task":"Task","person":"Employee",` +
	`"invoice":"Invoice","note":"Description"},"date_format":"2006-01-02"}`

func TestEntryImportHTTPRerunNoop(t *testing.T) {
	f := newImportFixture(t)
	s := f.e.Login(f.owner, f.e.Handler())
	type body struct {
		New          int                 `json:"new"`
		Existing     int                 `json:"existing"`
		TotalMinutes int                 `json:"total_minutes"`
		Problems     []importers.Problem `json:"problems"`
	}

	before := f.e.Snapshot("time_entries")
	resp := s.UploadForm("/api/v1/orgs/acme/time-entries/import/preview", "t.csv", []byte(entryCSV), map[string]string{"mapping": entryMapping})
	var b body
	resp.JSON(&b)
	if resp.Code != http.StatusOK || b.New != 2 || b.TotalMinutes != 180 {
		t.Fatalf("preview = %d %s", resp.Code, resp.Body)
	}
	if f.e.Snapshot("time_entries") != before {
		t.Fatal("preview wrote entries")
	}

	for i, want := range []body{{New: 2, TotalMinutes: 180}, {Existing: 2}} {
		resp = s.UploadForm("/api/v1/orgs/acme/time-entries/import", "t.csv", []byte(entryCSV), map[string]string{"mapping": entryMapping})
		b = body{}
		resp.JSON(&b)
		if resp.Code != http.StatusOK || b.New != want.New || b.Existing != want.Existing {
			t.Fatalf("apply %d = %d %s, want %+v", i, resp.Code, resp.Body, want)
		}
	}
	if n := f.e.Count("time_entries", "org_id = $1", f.org.ID); n != 2 {
		t.Errorf("entries = %d, want 2", n)
	}
}

func TestEntryImportHTTPProblemsRollBack(t *testing.T) {
	f := newImportFixture(t)
	s := f.e.Login(f.owner, f.e.Handler())
	before := f.e.Snapshot("time_entries", "audit_log")
	csv := entryCSV + "2025-03-03,1,Website,Build,owner@example.com,ACC-404,\n"
	resp := s.UploadForm("/api/v1/orgs/acme/time-entries/import", "t.csv", []byte(csv), map[string]string{"mapping": entryMapping})
	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("apply = %d %s, want 422", resp.Code, resp.Body)
	}
	if f.e.Snapshot("time_entries", "audit_log") != before {
		t.Error("422 apply wrote entries")
	}
}
