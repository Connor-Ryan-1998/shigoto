package quotes

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"shigoto/internal/core"
	"shigoto/internal/timesheets"
)

// Accept accepts sent (including expired) quote id for userID at now, in
// tx. With no linked project it creates a pending, fixed-price project with
// no hourly rate for the quote's customer, named after the quote; with one,
// it adds to that project, which must not be completed or cancelled. Each
// labour line becomes a task named after its description with the line's
// rate as the task's rate; material lines create nothing. A project or
// task name already in use gets the quote number appended.
func Accept(ctx context.Context, tx pgx.Tx, orgID, userID, id int64, now time.Time) (Quote, timesheets.Project, error) {
	current, err := lockQuote(ctx, tx, orgID, id)
	if err != nil {
		return Quote{}, timesheets.Project{}, err
	}
	if current.Status != StatusSent {
		return Quote{}, timesheets.Project{}, invalidState("quote is not open")
	}
	suffix := " (" + current.Label() + ")"

	var project timesheets.Project
	if current.ProjectID != nil {
		project, err = timesheets.GetProject(ctx, tx, orgID, *current.ProjectID)
		if err != nil {
			return Quote{}, timesheets.Project{}, err
		}
		if project.Status == timesheets.StatusCompleted || project.Status == timesheets.StatusCancelled {
			return Quote{}, timesheets.Project{}, invalidState("project is completed or cancelled")
		}
	} else {
		settings, err := core.GetSettings(ctx, tx, orgID)
		if err != nil {
			return Quote{}, timesheets.Project{}, err
		}
		gst := core.FRE
		if settings.GSTRegistered {
			gst = core.GST
		}
		name, err := freeName(ctx, tx, current.Title, suffix,
			`SELECT EXISTS(SELECT 1 FROM projects WHERE org_id = $1 AND name = $2)`, orgID)
		if err != nil {
			return Quote{}, timesheets.Project{}, err
		}
		project, err = timesheets.CreateProject(ctx, tx, orgID, timesheets.Project{
			ClientContactID: current.ContactID, Name: name, GSTCode: gst,
			Status: timesheets.StatusPending, BillingMethod: timesheets.BillingFixed,
		})
		if err != nil {
			return Quote{}, timesheets.Project{}, err
		}
	}

	lines, err := quoteLines(ctx, tx, orgID, id)
	if err != nil {
		return Quote{}, timesheets.Project{}, err
	}
	for _, l := range lines {
		if l.Kind != KindLabour {
			continue
		}
		name, err := freeName(ctx, tx, l.Description, suffix,
			`SELECT EXISTS(SELECT 1 FROM project_tasks WHERE project_id = $1 AND name = $2)`, project.ID)
		if err != nil {
			return Quote{}, timesheets.Project{}, err
		}
		var rate *core.Cents
		if l.UnitAmount > 0 {
			r := l.UnitAmount
			rate = &r
		}
		if _, err := timesheets.CreateTask(ctx, tx, orgID, project.ID, timesheets.Task{Name: name, HourlyRate: rate}); err != nil {
			return Quote{}, timesheets.Project{}, err
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE quotes SET status = 'accepted', project_id = $3, decided_at = $4, decided_by = $5
		WHERE org_id = $1 AND id = $2`, orgID, id, project.ID, now, userID); err != nil {
		if isUniqueViolation(err, "quotes_one_accepted") {
			return Quote{}, timesheets.Project{}, invalidState("quote is not open")
		}
		return Quote{}, timesheets.Project{}, err
	}
	accepted, err := GetQuote(ctx, tx, orgID, id)
	return accepted, project, err
}

// maxNameTries bounds freeName's search for an unused name.
const maxNameTries = 100

// freeName returns base if unused, else base+suffix, else base+suffix+" 2",
// " 3" and so on (existsSQL: $1 = scope, $2 = name), so repeated or
// clashing names never stop a quote being accepted.
func freeName(ctx context.Context, tx pgx.Tx, base, suffix, existsSQL string, scope int64) (string, error) {
	for i := 0; i < maxNameTries; i++ {
		name := base
		switch {
		case i == 1:
			name = base + suffix
		case i > 1:
			name = fmt.Sprintf("%s%s %d", base, suffix, i)
		}
		var exists bool
		if err := tx.QueryRow(ctx, existsSQL, scope, name).Scan(&exists); err != nil {
			return "", err
		}
		if !exists {
			return name, nil
		}
	}
	return "", fmt.Errorf("%w: no free name for %q", core.ErrInvalidInput, base)
}
