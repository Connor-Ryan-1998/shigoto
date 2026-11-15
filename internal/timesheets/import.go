package timesheets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"shigoto/internal/auth"
	"shigoto/internal/billing"
	"shigoto/internal/core"
	"shigoto/internal/importers"
	"shigoto/internal/module"
)

// EntryImportFields are the fields a time-entry CSV import reads.
var EntryImportFields = []importers.Field{
	{Key: "date", Required: true},
	{Key: "hours", Required: true},
	{Key: "project", Required: true},
	{Key: "task", Required: true},
	{Key: "person", Required: true},
	{Key: "invoice", Required: true},
	{Key: "note"},
}

// EntryRow is one parsed time import row. Ref is its external_ref:
// "import:" + hex sha256 of the normalised row and its occurrence index
// among identical rows in the file, so re-importing the file (or an
// overlapping one) recognises rows it already wrote.
type EntryRow struct {
	Row                                  int
	WorkDate                             time.Time
	Minutes                              int
	Project, Task, Person, Invoice, Note string
	Ref                                  string
}

// EntryImportResult summarises a time import or its preview.
type EntryImportResult struct {
	New          int                 `json:"new"`
	Existing     int                 `json:"existing"`
	TotalMinutes int                 `json:"total_minutes"`
	Problems     []importers.Problem `json:"problems"`
}

var decimalHours = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`)

// ParseEntryRows converts mapped CSV rows into EntryRows, checking
// everything that needs no database; today is the org's current date.
// Rows with problems are left out. Problems are in row order and never nil.
func ParseEntryRows(rows []importers.Row, dateFormat string, today time.Time) ([]EntryRow, []importers.Problem) {
	out := []EntryRow{}
	problems := []importers.Problem{}
	seen := map[string]int{}

	for _, r := range rows {
		var rowProblems []importers.Problem
		add := func(field, msg string) {
			rowProblems = append(rowProblems, importers.Problem{Row: r.Num, Field: field, Message: msg})
		}
		text := func(field, msg string) string {
			v := strings.TrimSpace(r.Values[field])
			if v == "" {
				add(field, msg)
			}
			return v
		}

		row := EntryRow{Row: r.Num}
		d, err := importers.ParseDate(dateFormat, r.Values["date"])
		switch {
		case err != nil:
			add("date", "invalid date")
		case d.After(today):
			add("date", "date is in the future")
		}
		row.WorkDate = d

		hours := strings.TrimSpace(r.Values["hours"])
		if !decimalHours.MatchString(hours) {
			add("hours", "invalid hours")
		} else {
			h, _ := strconv.ParseFloat(hours, 64)
			row.Minutes = int(math.Round(h * 60))
			if row.Minutes < 1 || row.Minutes > 1440 {
				add("hours", "hours must be more than 0 and at most 24")
			}
		}

		row.Project = text("project", "project is required")
		row.Task = text("task", "task is required")
		row.Person = text("person", "person is required")
		row.Invoice = text("invoice", "invoice number is required")
		row.Note = strings.TrimSpace(r.Values["note"])
		if utf8.RuneCountInString(row.Note) > 1000 {
			add("note", "note must be at most 1000 characters")
		}

		if len(rowProblems) > 0 {
			problems = append(problems, rowProblems...)
			continue
		}
		key := strings.Join([]string{
			row.WorkDate.Format(dateLayout), strconv.Itoa(row.Minutes),
			strings.ToLower(row.Project), strings.ToLower(row.Task), strings.ToLower(row.Person),
			row.Invoice, row.Note,
		}, "|")
		n := seen[key]
		seen[key] = n + 1
		sum := sha256.Sum256([]byte(key + "|" + strconv.Itoa(n)))
		row.Ref = "import:" + hex.EncodeToString(sum[:])
		out = append(out, row)
	}
	return out, problems
}

// ImportEntries checks rows against orgID's projects, tasks, members and
// imported invoices and, when apply is set and there are no problems,
// inserts the new ones, each linked to its invoice's single line so it is
// already billed. Completed or cancelled projects and inactive tasks are
// allowed: this is history. Rows whose Ref already exists count as
// Existing. The caller owns the transaction; with problems, nothing is
// written.
func ImportEntries(ctx context.Context, q core.Querier, orgID int64, actor *int64, rows []EntryRow, apply bool) (EntryImportResult, error) {
	res := EntryImportResult{Problems: []importers.Problem{}}
	type pending struct {
		row                              EntryRow
		taskID, userID, line, existingID int64
	}
	var toCreate []pending
	lk := lookups{q: q, orgID: orgID,
		projects: map[string][]int64{}, tasks: map[string][]int64{}, users: map[string][]int64{}, lines: map[string]lineLookup{}}

	for _, row := range rows {
		problem := func(field, msg string) {
			res.Problems = append(res.Problems, importers.Problem{Row: row.Row, Field: field, Message: msg})
		}
		// An entry already imported is skipped while it is billed. One whose
		// invoice was voided since (Void unlinks its time) is linked again
		// to the row's invoice, so a corrected invoice re-import followed by
		// the same time file restores it without duplicating it.
		var existingID int64
		var existingLine *int64
		err := q.QueryRow(ctx, `SELECT id, invoice_line_id FROM time_entries WHERE org_id = $1 AND external_ref = $2`, orgID, row.Ref).Scan(&existingID, &existingLine)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			existingID = 0
		case err != nil:
			return EntryImportResult{}, err
		case existingLine != nil:
			res.Existing++
			continue
		}

		bad := false
		var taskID int64
		projects, err := lk.projectsNamed(ctx, row.Project)
		if err != nil {
			return EntryImportResult{}, err
		}
		switch len(projects) {
		case 0:
			problem("project", fmt.Sprintf("no project named %q", row.Project))
			bad = true
		case 1:
			tasks, err := lk.tasksNamed(ctx, projects[0], row.Task)
			if err != nil {
				return EntryImportResult{}, err
			}
			switch len(tasks) {
			case 0:
				problem("task", fmt.Sprintf("no task named %q in %s", row.Task, lk.projectName(ctx, projects[0], row.Project)))
				bad = true
			case 1:
				taskID = tasks[0]
			default:
				problem("task", fmt.Sprintf("more than one task named %q in %s", row.Task, lk.projectName(ctx, projects[0], row.Project)))
				bad = true
			}
		default:
			problem("project", fmt.Sprintf("more than one project named %q", row.Project))
			bad = true
		}

		users, err := lk.membersWithEmail(ctx, row.Person)
		if err != nil {
			return EntryImportResult{}, err
		}
		if len(users) != 1 {
			problem("person", fmt.Sprintf("no member with email %q", row.Person))
			bad = true
		}

		line, err := lk.invoiceLine(ctx, row.Invoice)
		if err != nil {
			return EntryImportResult{}, err
		}
		switch {
		case !line.found:
			problem("invoice", fmt.Sprintf("invoice %s has not been imported", row.Invoice))
			bad = true
		case len(line.ids) != 1:
			problem("invoice", fmt.Sprintf("invoice %s is not an imported single-line invoice", row.Invoice))
			bad = true
		}
		if bad {
			continue
		}
		res.New++
		res.TotalMinutes += row.Minutes
		toCreate = append(toCreate, pending{row: row, taskID: taskID, userID: users[0], line: line.ids[0], existingID: existingID})
	}

	if !apply || len(res.Problems) > 0 {
		return res, nil
	}
	for _, p := range toCreate {
		if p.existingID != 0 {
			if _, err := q.Exec(ctx, `UPDATE time_entries SET invoice_line_id = $1 WHERE org_id = $2 AND id = $3`, p.line, orgID, p.existingID); err != nil {
				return EntryImportResult{}, err
			}
			if err := core.Audit(ctx, q, orgID, actor, "time_entry", p.existingID, "import_relink", map[string]any{
				"external_ref": p.row.Ref, "invoice": p.row.Invoice,
			}); err != nil {
				return EntryImportResult{}, err
			}
			continue
		}
		var id int64
		if err := q.QueryRow(ctx, `
			INSERT INTO time_entries (org_id, task_id, user_id, work_date, minutes, note, invoice_line_id, external_ref)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
			orgID, p.taskID, p.userID, p.row.WorkDate, p.row.Minutes, p.row.Note, p.line, p.row.Ref,
		).Scan(&id); err != nil {
			return EntryImportResult{}, err
		}
		if err := core.Audit(ctx, q, orgID, actor, "time_entry", id, "import", map[string]any{
			"external_ref": p.row.Ref, "invoice": p.row.Invoice,
		}); err != nil {
			return EntryImportResult{}, err
		}
	}
	return res, nil
}

type lineLookup struct {
	found bool
	ids   []int64
}

// lookups caches name lookups for one import.
type lookups struct {
	q        core.Querier
	orgID    int64
	projects map[string][]int64
	tasks    map[string][]int64
	users    map[string][]int64
	lines    map[string]lineLookup
}

func (l *lookups) ids(ctx context.Context, cache map[string][]int64, key, sql string, args ...any) ([]int64, error) {
	if ids, ok := cache[key]; ok {
		return ids, nil
	}
	rows, err := l.q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, err
	}
	cache[key] = ids
	return ids, nil
}

func (l *lookups) projectsNamed(ctx context.Context, name string) ([]int64, error) {
	key := strings.ToLower(strings.TrimSpace(name))
	return l.ids(ctx, l.projects, key,
		`SELECT id FROM projects WHERE org_id = $1 AND lower(btrim(name)) = $2 ORDER BY id`, l.orgID, key)
}

func (l *lookups) tasksNamed(ctx context.Context, projectID int64, name string) ([]int64, error) {
	key := strings.ToLower(strings.TrimSpace(name))
	return l.ids(ctx, l.tasks, strconv.FormatInt(projectID, 10)+"|"+key,
		`SELECT id FROM project_tasks WHERE org_id = $1 AND project_id = $2 AND lower(btrim(name)) = $3 ORDER BY id`,
		l.orgID, projectID, key)
}

func (l *lookups) membersWithEmail(ctx context.Context, email string) ([]int64, error) {
	key := strings.TrimSpace(email)
	return l.ids(ctx, l.users, strings.ToLower(key),
		`SELECT u.id FROM users u JOIN memberships m ON m.user_id = u.id WHERE m.org_id = $1 AND u.email = $2`, l.orgID, key)
}

// projectName is the stored name of projectID, for messages; fallback if
// the lookup fails.
func (l *lookups) projectName(ctx context.Context, projectID int64, fallback string) string {
	p, err := GetProject(ctx, l.q, l.orgID, projectID)
	if err != nil {
		return fallback
	}
	return p.Name
}

func (l *lookups) invoiceLine(ctx context.Context, number string) (lineLookup, error) {
	if v, ok := l.lines[number]; ok {
		return v, nil
	}
	var invoiceID int64
	err := l.q.QueryRow(ctx, `SELECT id FROM invoices WHERE org_id = $1 AND external_ref = $2`,
		l.orgID, billing.ImportRef(number)).Scan(&invoiceID)
	if errors.Is(err, pgx.ErrNoRows) {
		l.lines[number] = lineLookup{}
		return lineLookup{}, nil
	}
	if err != nil {
		return lineLookup{}, err
	}
	rows, err := l.q.Query(ctx, `SELECT id FROM invoice_lines WHERE org_id = $1 AND invoice_id = $2 ORDER BY id`, l.orgID, invoiceID)
	if err != nil {
		return lineLookup{}, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return lineLookup{}, err
	}
	v := lineLookup{found: true, ids: ids}
	l.lines[number] = v
	return v, nil
}

// importEntries serves the time import preview (apply false) and apply.
func importEntries(w http.ResponseWriter, r *http.Request, d module.Deps, apply bool) {
	p := auth.From(r.Context())
	now := today(d)
	module.ServeImport(w, r, d.Pool, p.OrgID, EntryImportFields, apply,
		func(ctx context.Context, q core.Querier, rows []importers.Row, dateFormat string, apply bool) (any, []importers.Problem, error) {
			parsed, parseProblems := ParseEntryRows(rows, dateFormat, now)
			res, err := ImportEntries(ctx, q, p.OrgID, &p.UserID, parsed, apply && len(parseProblems) == 0)
			if err != nil {
				return nil, nil, err
			}
			res.Problems = module.MergeProblems(parseProblems, res.Problems)
			return res, res.Problems, nil
		})
}
