//go:build integration

// Package timesheets_test exercises the timesheets store, plus a few tests
// that go through the real HTTP handlers, so it needs to import httpapi
// and auth alongside timesheets without an import cycle.
package timesheets_test

import (
	"errors"
	"testing"
	"time"

	"shigoto/internal/core"
	"shigoto/internal/testkit"
	"shigoto/internal/timesheets"
)

// withStatus returns p with Status replaced, for a full-replace
// UpdateProject call that only changes the status.
func withStatus(p timesheets.Project, status string) timesheets.Project {
	p.Status = status
	return p
}

func TestCreateProjectDefaultsAndDetails(t *testing.T) {
	e := testkit.New(t)
	pool, ctx := e.Pool, e.Ctx
	org := e.Org("acme")
	client := e.Contact(org, testkit.Name("A Client")).ID

	proj := e.Project(org, client)
	if proj.Status != timesheets.StatusInProgress || proj.BillingMethod != timesheets.BillingTM || !proj.Active {
		t.Fatalf("defaults = %q/%q/active=%v, want in_progress/tm/true", proj.Status, proj.BillingMethod, proj.Active)
	}

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)
	p, err := timesheets.CreateProject(ctx, pool, org.ID, timesheets.Project{
		ClientContactID: client, Name: "Fixed job", HourlyRate: 15000, GSTCode: core.GST,
		Status: timesheets.StatusPending, Description: "Scope doc", StartDate: &start, EndDate: &end,
		BillingMethod: timesheets.BillingFixed,
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	got, err := timesheets.GetProject(ctx, pool, org.ID, p.ID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if got.Status != timesheets.StatusPending || !got.Active || got.Description != "Scope doc" ||
		got.BillingMethod != timesheets.BillingFixed || !got.StartDate.Equal(start) || !got.EndDate.Equal(end) {
		t.Fatalf("round trip = %+v", got)
	}

	bad := []timesheets.Project{
		{ClientContactID: client, Name: "Bad status", HourlyRate: 1, GSTCode: core.GST, Status: "paused"},
		{ClientContactID: client, Name: "Bad method", HourlyRate: 1, GSTCode: core.GST, BillingMethod: "retainer"},
		{ClientContactID: client, Name: "Bad dates", HourlyRate: 1, GSTCode: core.GST, StartDate: &end, EndDate: &start},
	}
	for _, b := range bad {
		if _, err := timesheets.CreateProject(ctx, pool, org.ID, b); !errors.Is(err, core.ErrInvalidInput) {
			t.Fatalf("%s: err = %v, want ErrInvalidInput", b.Name, err)
		}
	}
}

func TestUpdateProjectStatusDrivesActive(t *testing.T) {
	e := testkit.New(t)
	pool, ctx := e.Pool, e.Ctx
	org := e.Org("acme")
	client := e.Contact(org, testkit.Name("A Client")).ID
	proj := e.Project(org, client)

	for status, active := range map[string]bool{
		timesheets.StatusCancelled: false, timesheets.StatusPending: true,
		timesheets.StatusCompleted: false, timesheets.StatusInProgress: true,
	} {
		updated, err := timesheets.UpdateProject(ctx, pool, org.ID, proj.ID, withStatus(proj, status))
		if err != nil {
			t.Fatalf("UpdateProject(%s): %v", status, err)
		}
		if updated.Active != active {
			t.Fatalf("status %s: Active = %v, want %v", status, updated.Active, active)
		}
	}
}

func TestUpdateProjectCustomerLockedOnceBilled(t *testing.T) {
	e := testkit.New(t)
	pool, ctx := e.Pool, e.Ctx
	org := e.Org("acme")
	client := e.Contact(org, testkit.Name("A Client")).ID
	other := e.Contact(org, testkit.Name("Other Client")).ID
	tenant := e.Contact(org, testkit.Kind("tenant"), testkit.Name("A Tenant")).ID
	proj := e.Project(org, client)

	moved := proj
	moved.ClientContactID = tenant
	if _, err := timesheets.UpdateProject(ctx, pool, org.ID, proj.ID, moved); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("move to tenant: err = %v, want ErrInvalidInput", err)
	}
	moved.ClientContactID = other
	updated, err := timesheets.UpdateProject(ctx, pool, org.ID, proj.ID, moved)
	if err != nil || updated.ClientContactID != other {
		t.Fatalf("move unbilled project: %+v, %v", updated, err)
	}

	// Bill some time, then the customer is locked.
	task := e.Task(org, proj.ID, testkit.TaskName("Build"))
	userID := e.User("billed@example.com").ID
	day := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	e.Entry(org, userID, timesheets.Entry{TaskID: task.ID, WorkDate: day, Minutes: 60}, day)
	if _, err := billAll(t, ctx, e.BillingService(), org.ID, timesheets.RunInput{ClientContactID: other, From: day, To: day, DueInDays: 14}); err != nil {
		t.Fatalf("RunBilling: %v", err)
	}
	moved.ClientContactID = client
	if _, err := timesheets.UpdateProject(ctx, pool, org.ID, proj.ID, moved); !errors.Is(err, core.ErrInvalidState) {
		t.Fatalf("move billed project: err = %v, want ErrInvalidState", err)
	}
}

func TestCreateProjectRequiresClientContact(t *testing.T) {
	e := testkit.New(t)
	pool, ctx := e.Pool, e.Ctx
	org := e.Org("acme")

	tenant := e.Contact(org, testkit.Kind("tenant"), testkit.Name("A Tenant")).ID
	if _, err := timesheets.CreateProject(ctx, pool, org.ID, timesheets.Project{
		ClientContactID: tenant, Name: "Bad client kind", HourlyRate: 10000, GSTCode: core.GST,
	}); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("project with tenant contact: err = %v, want ErrInvalidInput", err)
	}

	other := e.Org("other")
	otherClient := e.Contact(other, testkit.Name("Other Org Client")).ID
	if _, err := timesheets.CreateProject(ctx, pool, org.ID, timesheets.Project{
		ClientContactID: otherClient, Name: "Cross org client", HourlyRate: 10000, GSTCode: core.GST,
	}); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("project with cross-org client: err = %v, want ErrInvalidInput", err)
	}
}

func TestCreateProjectDuplicateName422(t *testing.T) {
	e := testkit.New(t)
	pool, ctx := e.Pool, e.Ctx
	org := e.Org("acme")
	client := e.Contact(org, testkit.Name("A Client")).ID

	if _, err := timesheets.CreateProject(ctx, pool, org.ID, timesheets.Project{
		ClientContactID: client, Name: "Website rebuild", HourlyRate: 10000, GSTCode: core.GST,
	}); err != nil {
		t.Fatalf("first CreateProject: %v", err)
	}
	if _, err := timesheets.CreateProject(ctx, pool, org.ID, timesheets.Project{
		ClientContactID: client, Name: "Website rebuild", HourlyRate: 15000, GSTCode: core.GST,
	}); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("duplicate name: err = %v, want ErrInvalidInput", err)
	}
}

func TestUpdateProjectComplete(t *testing.T) {
	e := testkit.New(t)
	pool, ctx := e.Pool, e.Ctx
	org := e.Org("acme")
	client := e.Contact(org, testkit.Name("A Client")).ID
	proj := e.Project(org, client)

	updated, err := timesheets.UpdateProject(ctx, pool, org.ID, proj.ID, withStatus(proj, timesheets.StatusCompleted))
	if err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}
	if updated.Active {
		t.Fatalf("Active = true, want false")
	}
	if updated.ClientContactID != client {
		t.Fatalf("ClientContactID = %d, want unchanged %d", updated.ClientContactID, client)
	}

	got, err := timesheets.GetProject(ctx, pool, org.ID, proj.ID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if got.Active {
		t.Fatalf("persisted Active = true, want false")
	}
}

func TestCreateTaskDuplicateName422(t *testing.T) {
	e := testkit.New(t)
	pool, ctx := e.Pool, e.Ctx
	org := e.Org("acme")
	client := e.Contact(org, testkit.Name("A Client")).ID
	proj := e.Project(org, client)

	if _, err := timesheets.CreateTask(ctx, pool, org.ID, proj.ID, timesheets.Task{Name: "Design"}); err != nil {
		t.Fatalf("first CreateTask: %v", err)
	}
	if _, err := timesheets.CreateTask(ctx, pool, org.ID, proj.ID, timesheets.Task{Name: "Design"}); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("duplicate task name: err = %v, want ErrInvalidInput", err)
	}
}

// TestEffectiveRate is a pure unit test (no DB needed), kept alongside the
// store tests above rather than in its own file per the plan.
func TestEffectiveRate(t *testing.T) {
	project := timesheets.Project{HourlyRate: 10000}

	noOverride := timesheets.Task{HourlyRate: nil}
	if got := noOverride.EffectiveRate(project); got != project.HourlyRate {
		t.Fatalf("EffectiveRate with no override = %d, want project rate %d", got, project.HourlyRate)
	}

	override := core.Cents(15000)
	withOverride := timesheets.Task{HourlyRate: &override}
	if got := withOverride.EffectiveRate(project); got != override {
		t.Fatalf("EffectiveRate with override = %d, want override %d", got, override)
	}
}

func TestProjectClientFieldsRoundTrip(t *testing.T) {
	e := testkit.New(t)
	pool, ctx := e.Pool, e.Ctx
	org := e.Org("acme")
	client := e.Contact(org, testkit.Name("A Client")).ID

	p, err := timesheets.CreateProject(ctx, pool, org.ID, timesheets.Project{
		ClientContactID: client, Name: "Site", HourlyRate: 15000, GSTCode: core.GST,
		Summary: "Rebuild site", Goals: "Bookings live\nFast mobile", Stakeholders: "Priya Nair — Sponsor",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	got, err := timesheets.GetProject(ctx, pool, org.ID, p.ID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if got.Summary != "Rebuild site" || got.Goals != "Bookings live\nFast mobile" || got.Stakeholders != "Priya Nair — Sponsor" {
		t.Fatalf("round trip = %+v", got)
	}

	got.Summary, got.Goals, got.Stakeholders = "New", "One", "Sam — IT"
	updated, err := timesheets.UpdateProject(ctx, pool, org.ID, p.ID, got)
	if err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}
	if updated.Summary != "New" || updated.Goals != "One" || updated.Stakeholders != "Sam — IT" {
		t.Fatalf("update = %+v", updated)
	}

	long := got
	long.Summary = string(make([]rune, 4001))
	if _, err := timesheets.UpdateProject(ctx, pool, org.ID, p.ID, long); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("4001-char summary err = %v, want ErrInvalidInput", err)
	}
}

func TestUpdateProjectClientLockedByDocuments(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	a := e.Contact(org, testkit.Name("A")).ID
	b := e.Contact(org, testkit.Name("B")).ID
	p := e.Project(org, a)
	var fileID int64
	if err := e.Pool.QueryRow(e.Ctx, `INSERT INTO files (org_id, sha256, mime, size_bytes, original_name)
		VALUES ($1, '\x01', 'application/pdf', 1, 'x.pdf') RETURNING id`, org.ID).Scan(&fileID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Pool.Exec(e.Ctx, `INSERT INTO client_documents (org_id, contact_id, project_id, title, kind, file_id, file_sha256)
		VALUES ($1, $2, $3, 'SOW', 'signature', $4, '\x01')`, org.ID, a, p.ID, fileID); err != nil {
		t.Fatal(err)
	}
	p.ClientContactID = b
	if _, err := timesheets.UpdateProject(e.Ctx, e.Pool, org.ID, p.ID, p); !errors.Is(err, core.ErrInvalidState) {
		t.Fatalf("client change with documents err = %v, want ErrInvalidState", err)
	}
}

// TestFixedProjectWithoutRate covers the nullable hourly rate: a
// fixed-price project may have none (0, stored as NULL), a time-and-
// materials project may not.
func TestFixedProjectWithoutRate(t *testing.T) {
	e := testkit.New(t)
	pool, ctx := e.Pool, e.Ctx
	org := e.Org("acme")
	client := e.Contact(org, testkit.Name("A Client")).ID

	p, err := timesheets.CreateProject(ctx, pool, org.ID, timesheets.Project{
		ClientContactID: client, Name: "Quoted job", GSTCode: core.GST,
		BillingMethod: timesheets.BillingFixed,
	})
	if err != nil {
		t.Fatalf("CreateProject fixed without rate: %v", err)
	}
	got, err := timesheets.GetProject(ctx, pool, org.ID, p.ID)
	if err != nil || got.HourlyRate != 0 {
		t.Fatalf("GetProject = %+v, %v; want HourlyRate 0", got, err)
	}
	if n := e.Count("projects", "id = $1 AND hourly_rate IS NULL", p.ID); n != 1 {
		t.Fatalf("stored hourly_rate is not NULL")
	}

	_, err = timesheets.CreateProject(ctx, pool, org.ID, timesheets.Project{
		ClientContactID: client, Name: "Hourly job", GSTCode: core.GST,
	})
	if !errors.Is(err, core.ErrInvalidInput) || err.Error() != "invalid input: hourly_rate must be positive" {
		t.Fatalf("tm without rate err = %v, want hourly_rate must be positive", err)
	}

	got.BillingMethod = timesheets.BillingTM
	if _, err := timesheets.UpdateProject(ctx, pool, org.ID, p.ID, got); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("switch to tm without rate err = %v, want ErrInvalidInput", err)
	}
}

// TestFixedProjectNullRateReaders checks every reader of the project rate
// copes with a fixed-price project that has none.
func TestFixedProjectNullRateReaders(t *testing.T) {
	e := testkit.New(t)
	pool, ctx := e.Pool, e.Ctx
	org := e.Org("acme")
	userID := e.User("reader@example.com").ID
	client := e.Contact(org, testkit.Name("A Client")).ID
	p := e.Project(org, client, testkit.ProjectName("Quoted job"), testkit.ProjectRate(0), testkit.FixedPrice())
	task := e.Task(org, p.ID, testkit.TaskName("Build"))
	day := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	e.Entry(org, userID, timesheets.Entry{TaskID: task.ID, WorkDate: day, Minutes: 60}, day)

	entries, err := timesheets.ListEntries(ctx, pool, org.ID, timesheets.EntryFilter{ProjectID: &p.ID})
	if err != nil || len(entries) != 1 || entries[0].Rate != 0 {
		t.Fatalf("ListEntries = %+v, %v; want one entry at rate 0", entries, err)
	}
	if _, err := timesheets.Module().Dashboard(ctx, pool, org.ID, day); err != nil {
		t.Fatalf("dashboard: %v", err)
	}
	preview, err := timesheets.PreviewBilling(ctx, pool, org.ID, timesheets.PreviewInput{ClientContactID: client, From: day, To: day})
	if err != nil {
		t.Fatalf("PreviewBilling: %v", err)
	}
	if len(preview.Projects) != 0 {
		t.Fatalf("preview = %+v, want nothing to bill", preview)
	}
}
