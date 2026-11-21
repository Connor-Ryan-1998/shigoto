//go:build integration

package timesheets_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"shigoto/internal/core"
	"shigoto/internal/testkit"
	"shigoto/internal/timesheets"
)

type projectDashboardBody struct {
	UnbilledMinutes  int   `json:"unbilled_minutes"`
	UnbilledEstimate int64 `json:"unbilled_estimate"`
	ProjectCount     int   `json:"project_count"`
	Projects         []struct {
		ID              int64  `json:"id"`
		Name            string `json:"name"`
		ClientName      string `json:"client_name"`
		BilledMinutes   int    `json:"billed_minutes"`
		UnbilledMinutes int    `json:"unbilled_minutes"`
	} `json:"projects"`
}

func readProjectDashboard(t *testing.T, e *testkit.Env, org core.Org) projectDashboardBody {
	t.Helper()
	value, err := timesheets.Module().Dashboard(e.Ctx, e.Pool, org.ID, testkit.Date("2026-11-21"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var body projectDashboardBody
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}
	return body
}

func TestDashboardActiveProjects(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	client := e.Contact(org, testkit.Name("Client A"))
	user := e.User("owner@example.com")
	day := testkit.Date("2026-11-01")
	for _, c := range []struct {
		name, status string
		fixed        bool
	}{
		{"Pending", "pending", false}, {"In progress", "in_progress", false},
		{"Completed", "completed", false}, {"Cancelled", "cancelled", false},
		{"Fixed", "in_progress", true},
	} {
		opts := []testkit.ProjectOption{testkit.ProjectName(c.name), testkit.ProjectRate(12000)}
		if c.fixed {
			opts = append(opts, testkit.FixedPrice())
		}
		p := e.Project(org, client.ID, opts...)
		task := e.Task(org, p.ID)
		e.Entry(org, user.ID, timesheets.Entry{TaskID: task.ID, WorkDate: day, Minutes: 60}, day)
		if _, err := e.Pool.Exec(e.Ctx, "UPDATE projects SET status=$1 WHERE id=$2", c.status, p.ID); err != nil {
			t.Fatal(err)
		}
		if c.name == "In progress" {
			if _, err := e.Pool.Exec(e.Ctx, "UPDATE project_tasks SET active=false WHERE id=$1", task.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	e.Project(org, client.ID, testkit.ProjectName("No time"))
	other := e.Org("other")
	p := e.Project(other, e.Contact(other).ID)
	task := e.Task(other, p.ID)
	e.Entry(other, user.ID, timesheets.Entry{TaskID: task.ID, WorkDate: day, Minutes: 999}, day)
	body := readProjectDashboard(t, e, org)
	if body.UnbilledMinutes != 120 || body.UnbilledEstimate != 24000 || body.ProjectCount != 2 || len(body.Projects) != 2 {
		t.Fatalf("active scope: %+v", body)
	}
	if body.Projects[0].Name != "Pending" || body.Projects[1].Name != "In progress" || body.Projects[0].ClientName != "Client A" {
		t.Fatalf("project names/order: %+v", body.Projects)
	}
}

func TestDashboardProjectBillingProgress(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	client := e.Contact(org)
	user := e.User("owner@example.com")
	day := testkit.Date("2026-11-01")
	for i, status := range []string{"draft", "issued", "fully billed"} {
		p := e.Project(org, client.ID, testkit.ProjectName(status))
		task := e.Task(org, p.ID)
		entry := e.Entry(org, user.ID, timesheets.Entry{TaskID: task.ID, WorkDate: day, Minutes: 90}, day)
		inv := e.Invoice(org, client.ID, testkit.Amount(18000))
		if status == "issued" {
			e.Issue(org, inv.ID, day)
		}
		if _, err := e.Pool.Exec(e.Ctx, `UPDATE time_entries SET invoice_line_id=(SELECT id FROM invoice_lines WHERE invoice_id=$1 LIMIT 1) WHERE id=$2`, inv.ID, entry.ID); err != nil {
			t.Fatal(err)
		}
		if i < 2 {
			e.Entry(org, user.ID, timesheets.Entry{TaskID: task.ID, WorkDate: day, Minutes: 30}, day)
		}
	}
	body := readProjectDashboard(t, e, org)
	if body.ProjectCount != 2 || body.UnbilledMinutes != 60 {
		t.Fatalf("billed filtering: %+v", body)
	}
	for _, p := range body.Projects {
		if p.BilledMinutes != 90 || p.UnbilledMinutes != 30 {
			t.Fatalf("draft/issued progress: %+v", p)
		}
	}
}

func TestDashboardProjectTotalsAndRanking(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	empty := readProjectDashboard(t, e, org)
	if empty.Projects == nil || len(empty.Projects) != 0 || empty.ProjectCount != 0 || empty.UnbilledMinutes != 0 || empty.UnbilledEstimate != 0 {
		t.Fatalf("empty: %+v", empty)
	}
	client := e.Contact(org)
	user := e.User("owner@example.com")
	day := testkit.Date("2026-11-01")
	for i := 1; i <= 6; i++ {
		p := e.Project(org, client.ID, testkit.ProjectName(fmt.Sprintf("Project %d", i)), testkit.ProjectRate(100))
		task := e.Task(org, p.ID, testkit.TaskRate(101))
		e.Entry(org, user.ID, timesheets.Entry{TaskID: task.ID, WorkDate: day, Minutes: i}, day)
	}
	body := readProjectDashboard(t, e, org)
	// Per-task rounded cents: 2 + 3 + 5 + 7 + 8 + 10 = 35.
	if body.UnbilledMinutes != 21 || body.UnbilledEstimate != 35 || body.ProjectCount != 6 || len(body.Projects) != 5 {
		t.Fatalf("totals/top five: %+v", body)
	}
	if body.Projects[0].Name != "Project 6" || body.Projects[4].Name != "Project 2" {
		t.Fatalf("ranking: %+v", body.Projects)
	}
}
