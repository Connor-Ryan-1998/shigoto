//go:build integration

package quotes_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"shigoto/internal/core"
	"shigoto/internal/module"
	"shigoto/internal/quotes"
	"shigoto/internal/testkit"
	"shigoto/internal/timesheets"
)

var acceptedAt = time.Date(2026, 12, 2, 0, 0, 0, 0, time.UTC)

func accept(t *testing.T, e *testkit.Env, orgID, userID, id int64) (quotes.Quote, timesheets.Project, error) {
	t.Helper()
	var q quotes.Quote
	var p timesheets.Project
	err := module.RunInTx(e.Ctx, e.Pool, func(tx pgx.Tx) error {
		var err error
		q, p, err = quotes.Accept(e.Ctx, tx, orgID, userID, id, acceptedAt)
		return err
	})
	return q, p, err
}

// sentQuote creates a quote from d and forces it to sent.
func sentQuote(t *testing.T, e *testkit.Env, orgID int64, d quotes.Draft) quotes.Quote {
	t.Helper()
	q, err := quotes.CreateDraft(e.Ctx, e.Pool, orgID, d)
	if err != nil {
		t.Fatal(err)
	}
	setStatus(t, e, q.ID, quotes.StatusSent)
	q.Status = quotes.StatusSent
	return q
}

func taskNames(t *testing.T, e *testkit.Env, orgID, projectID int64) map[string]*core.Cents {
	t.Helper()
	tasks, err := timesheets.ListTasks(e.Ctx, e.Pool, orgID, projectID, false)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]*core.Cents{}
	for _, tk := range tasks {
		out[tk.Name] = tk.HourlyRate
	}
	return out
}

func TestAcceptCreatesProject(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	e.Settings(org, testkit.GSTRegistered(true))
	user := e.User("owner@example.com")
	client := e.Contact(org, testkit.Name("Priya")).ID
	q := sentQuote(t, e, org.ID, draft(client,
		labour("Rough-in", 8000, 9500), labour("Fit-off", 4000, 9500), material("Cable", 1000, 18000)))

	got, p, err := accept(t, e, org.ID, user.ID, q.ID)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if p.Name != "Switchboard upgrade" || p.ClientContactID != client || p.Status != timesheets.StatusPending ||
		p.BillingMethod != timesheets.BillingFixed || p.HourlyRate != 0 || p.GSTCode != core.GST {
		t.Fatalf("project = %+v", p)
	}
	tasks := taskNames(t, e, org.ID, p.ID)
	if len(tasks) != 2 || tasks["Rough-in"] == nil || *tasks["Rough-in"] != 9500 || tasks["Fit-off"] == nil || *tasks["Fit-off"] != 9500 {
		t.Fatalf("tasks = %v, want Rough-in and Fit-off at 9500", tasks)
	}
	if got.Status != quotes.StatusAccepted || got.ProjectID == nil || *got.ProjectID != p.ID ||
		got.DecidedBy == nil || *got.DecidedBy != user.ID || got.DecidedAt == nil || !got.DecidedAt.Equal(acceptedAt) {
		t.Fatalf("quote = %+v", got)
	}
}

func TestAcceptNonGSTOrgProjectIsGSTFree(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	user := e.User("owner@example.com")
	q := sentQuote(t, e, org.ID, draft(e.Contact(org).ID, quotes.NewLine{Kind: quotes.KindLabour, Description: "Work", QuantityMilli: 1000, UnitAmount: 100, GSTCode: "FRE"}))
	_, p, err := accept(t, e, org.ID, user.ID, q.ID)
	if err != nil || p.GSTCode != core.FRE {
		t.Fatalf("project = %+v, %v; want GST code FRE", p, err)
	}
}

func TestAcceptExistingProject(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	e.Settings(org, testkit.GSTRegistered(true))
	user := e.User("owner@example.com")
	client := e.Contact(org).ID
	existing := e.Project(org, client, testkit.ProjectName("Big job"), testkit.ProjectRate(12000))
	d := draft(client, labour("Rough-in", 8000, 9500))
	d.ProjectID = &existing.ID
	q := sentQuote(t, e, org.ID, d)

	_, p, err := accept(t, e, org.ID, user.ID, q.ID)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if p.ID != existing.ID || p.BillingMethod != timesheets.BillingTM || p.HourlyRate != 12000 {
		t.Fatalf("project changed: %+v", p)
	}
	if tasks := taskNames(t, e, org.ID, p.ID); tasks["Rough-in"] == nil || *tasks["Rough-in"] != 9500 {
		t.Fatalf("tasks = %v", tasks)
	}
	if n := e.Count("projects", "org_id = $1", org.ID); n != 1 {
		t.Fatalf("%d projects, want 1", n)
	}
}

func TestAcceptBlockedOnClosedProject(t *testing.T) {
	for _, status := range []string{timesheets.StatusCompleted, timesheets.StatusCancelled} {
		t.Run(status, func(t *testing.T) {
			e := testkit.New(t)
			org := e.Org("acme")
			e.Settings(org, testkit.GSTRegistered(true))
			user := e.User("owner@example.com")
			client := e.Contact(org).ID
			closed := e.Project(org, client, testkit.ProjectStatus(status))
			d := draft(client, labour("Rough-in", 8000, 9500))
			d.ProjectID = &closed.ID
			q := sentQuote(t, e, org.ID, d)
			if _, _, err := accept(t, e, org.ID, user.ID, q.ID); !errors.Is(err, core.ErrInvalidState) || err.Error() != "project is completed or cancelled" {
				t.Fatalf("err = %v, want project is completed or cancelled", err)
			}
			if n := e.Count("project_tasks", "project_id = $1", closed.ID); n != 0 {
				t.Fatalf("%d tasks written", n)
			}
		})
	}
}

func TestAcceptNameClashes(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	e.Settings(org, testkit.GSTRegistered(true))
	user := e.User("owner@example.com")
	client := e.Contact(org).ID
	e.Project(org, client, testkit.ProjectName("Switchboard upgrade"))

	q := sentQuote(t, e, org.ID, draft(client, labour("Rough-in", 8000, 9500)))
	_, p, err := accept(t, e, org.ID, user.ID, q.ID)
	if err != nil || p.Name != "Switchboard upgrade (Q-0001)" {
		t.Fatalf("project = %q, %v; want suffixed name", p.Name, err)
	}

	// A second quote onto that project whose labour line clashes with a task.
	d := draft(client, labour("Rough-in", 2000, 9500))
	d.ProjectID = &p.ID
	q2 := sentQuote(t, e, org.ID, d)
	if _, _, err := accept(t, e, org.ID, user.ID, q2.ID); err != nil {
		t.Fatalf("Accept q2: %v", err)
	}
	if tasks := taskNames(t, e, org.ID, p.ID); tasks["Rough-in"] == nil || tasks["Rough-in (Q-0002)"] == nil {
		t.Fatalf("tasks = %v, want Rough-in and Rough-in (Q-0002)", tasks)
	}

	// Both the name and its suffixed form taken: refused, nothing written.
	e.Project(org, client, testkit.ProjectName("Switchboard upgrade (Q-0003)"))
	q3 := sentQuote(t, e, org.ID, draft(client, labour("Rough-in", 1000, 9500)))
	before := e.Count("projects", "org_id = $1", org.ID)
	if _, _, err := accept(t, e, org.ID, user.ID, q3.ID); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("double clash err = %v, want ErrInvalidInput", err)
	}
	if n := e.Count("projects", "org_id = $1", org.ID); n != before {
		t.Fatalf("projects %d -> %d after failed accept", before, n)
	}
	if got, _ := quotes.GetQuote(e.Ctx, e.Pool, org.ID, q3.ID); got.Status != quotes.StatusSent {
		t.Fatalf("q3 status = %q, want still sent", got.Status)
	}
}

func TestAcceptRevisionSuffix(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	e.Settings(org, testkit.GSTRegistered(true))
	user := e.User("owner@example.com")
	client := e.Contact(org).ID
	e.Project(org, client, testkit.ProjectName("Switchboard upgrade"))
	q := sentQuote(t, e, org.ID, draft(client, labour("Rough-in", 1000, 9500)))
	r2, err := quotes.Revise(e.Ctx, e.Pool, org.ID, q.ID, issued)
	if err != nil {
		t.Fatal(err)
	}
	setStatus(t, e, q.ID, quotes.StatusSuperseded)
	setStatus(t, e, r2.ID, quotes.StatusSent)
	_, p, err := accept(t, e, org.ID, user.ID, r2.ID)
	if err != nil || p.Name != "Switchboard upgrade (Q-0001-R2)" {
		t.Fatalf("project = %q, %v", p.Name, err)
	}
}

func TestAcceptExpired(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	e.Settings(org, testkit.GSTRegistered(true))
	user := e.User("owner@example.com")
	d := draft(e.Contact(org).ID, labour("Rough-in", 1000, 9500))
	d.IssueDate, d.ValidUntil = issued.AddDate(0, -2, 0), issued.AddDate(0, -1, 0)
	q := sentQuote(t, e, org.ID, d)
	if got, _, err := accept(t, e, org.ID, user.ID, q.ID); err != nil || got.Status != quotes.StatusAccepted {
		t.Fatalf("accept expired = %q, %v", got.Status, err)
	}
}

func TestAcceptStates(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	e.Settings(org, testkit.GSTRegistered(true))
	user := e.User("owner@example.com")
	client := e.Contact(org).ID
	for _, status := range []string{quotes.StatusDraft, quotes.StatusDeclined, quotes.StatusSuperseded, quotes.StatusAccepted} {
		q, err := quotes.CreateDraft(e.Ctx, e.Pool, org.ID, draft(client, labour("Rough-in", 1000, 9500)))
		if err != nil {
			t.Fatal(err)
		}
		setStatus(t, e, q.ID, status)
		if _, _, err := accept(t, e, org.ID, user.ID, q.ID); !errors.Is(err, core.ErrInvalidState) || err.Error() != "quote is not open" {
			t.Errorf("%s: err = %v, want quote is not open", status, err)
		}
	}
	if n := e.Count("projects", "org_id = $1", org.ID); n != 0 {
		t.Fatalf("%d projects written", n)
	}
}

func TestAcceptOnlyMaterials(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	e.Settings(org, testkit.GSTRegistered(true))
	user := e.User("owner@example.com")
	q := sentQuote(t, e, org.ID, draft(e.Contact(org).ID, material("Cable", 1000, 18000)))
	_, p, err := accept(t, e, org.ID, user.ID, q.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n := e.Count("project_tasks", "project_id = $1", p.ID); n != 0 {
		t.Fatalf("%d tasks, want none", n)
	}
}

func TestAcceptConcurrentOnlyOnce(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	e.Settings(org, testkit.GSTRegistered(true))
	user := e.User("owner@example.com")
	q := sentQuote(t, e, org.ID, draft(e.Contact(org).ID, labour("Rough-in", 1000, 9500)))

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = module.RunInTx(e.Ctx, e.Pool, func(tx pgx.Tx) error {
				_, _, err := quotes.Accept(e.Ctx, tx, org.ID, user.ID, q.ID, acceptedAt)
				return err
			})
		}()
	}
	wg.Wait()
	ok, refused := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, core.ErrInvalidState) && err.Error() == "quote is not open":
			refused++
		default:
			t.Fatalf("unexpected err %v", err)
		}
	}
	if ok != 1 || refused != 1 {
		t.Fatalf("ok=%d refused=%d, want 1/1", ok, refused)
	}
	if n := e.Count("projects", "org_id = $1", org.ID); n != 1 {
		t.Fatalf("%d projects, want 1", n)
	}
	if n := e.Count("project_tasks", "org_id = $1", org.ID); n != 1 {
		t.Fatalf("%d tasks, want 1", n)
	}
}
