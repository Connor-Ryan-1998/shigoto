package billing

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"shigoto/internal/core"
	"shigoto/internal/importers"
)

// InvoiceImportFields are the fields an invoice CSV import reads.
var InvoiceImportFields = []importers.Field{
	{Key: "number", Required: true},
	{Key: "customer", Required: true},
	{Key: "issue_date", Required: true},
	{Key: "due_date", Required: true},
	{Key: "subtotal", Required: true},
	{Key: "gst", Required: true},
	{Key: "total", Required: true},
	{Key: "outstanding", Required: true},
	{Key: "paid_date"},
}

// InvoiceRow is one parsed invoice import row. PaidDate is set when the
// row gave one.
type InvoiceRow struct {
	Row                               int
	Number, Customer                  string
	IssueDate, DueDate                time.Time
	PaidDate                          *time.Time
	Subtotal, GST, Total, Outstanding core.Cents
}

// ImportResult summarises an invoice import or its preview.
type ImportResult struct {
	New        int                 `json:"new"`
	Existing   int                 `json:"existing"`
	TotalCents int64               `json:"total_cents"`
	Problems   []importers.Problem `json:"problems"`
}

// ImportRef is the external_ref an imported invoice with number carries.
func ImportRef(number string) string { return "import:" + number }

// ParseInvoiceRows converts mapped CSV rows into InvoiceRows, checking
// everything that needs no database. Rows with problems are left out.
// Problems are in row order; it never returns a nil problems slice.
func ParseInvoiceRows(rows []importers.Row, dateFormat string) ([]InvoiceRow, []importers.Problem) {
	out := []InvoiceRow{}
	problems := []importers.Problem{}
	seen := map[string]bool{}

	for _, r := range rows {
		var rowProblems []importers.Problem
		add := func(field, msg string) {
			rowProblems = append(rowProblems, importers.Problem{Row: r.Num, Field: field, Message: msg})
		}
		text := func(field string) string {
			v := strings.TrimSpace(r.Values[field])
			if v == "" {
				add(field, field+" is required")
			}
			return v
		}
		date := func(field string) time.Time {
			d, err := importers.ParseDate(dateFormat, r.Values[field])
			if err != nil {
				add(field, "invalid "+field)
			}
			return d
		}
		money := func(field string) core.Cents {
			raw := strings.TrimSpace(r.Values[field])
			c, err := importers.ParseAmountCents(raw)
			switch {
			case raw == "" || err != nil:
				add(field, "invalid "+field)
			case c < 0:
				add(field, field+" must not be negative")
			}
			return c
		}

		row := InvoiceRow{Row: r.Num}
		row.Number = text("number")
		row.Customer = text("customer")
		row.IssueDate = date("issue_date")
		row.DueDate = date("due_date")
		row.Subtotal = money("subtotal")
		row.GST = money("gst")
		row.Total = money("total")
		row.Outstanding = money("outstanding")
		if raw := strings.TrimSpace(r.Values["paid_date"]); raw != "" {
			if d, err := importers.ParseDate(dateFormat, raw); err != nil {
				add("paid_date", "invalid paid_date")
			} else {
				row.PaidDate = &d
			}
		}

		if len(rowProblems) == 0 {
			switch {
			case row.Total <= 0:
				add("total", "total must be more than zero")
			case row.Total != row.Subtotal+row.GST:
				add("total", "total must equal subtotal plus GST")
			}
			if row.Outstanding > row.Total {
				add("outstanding", "outstanding must be between 0 and total")
			}
			if row.DueDate.Before(row.IssueDate) {
				add("due_date", "due date is before issue date")
			}
			if row.Outstanding < row.Total && row.PaidDate == nil {
				add("paid_date", "paid date is required when anything has been paid")
			}
		}
		if row.Number != "" {
			key := strings.ToLower(row.Number)
			if seen[key] {
				add("number", "invoice number appears more than once")
			}
			seen[key] = true
		}

		if len(rowProblems) > 0 {
			problems = append(problems, rowProblems...)
			continue
		}
		out = append(out, row)
	}
	return out, problems
}

// ImportInvoices checks rows against orgID's data - customer contacts, GST
// registration, already-imported invoices - and, when apply is set and
// there are no problems, writes the new ones: each via core.ImportInvoice
// (in issue-date then row order, so numbers follow issue dates), a
// payment for any amount already paid, and an audit row. It never issues
// or sends anything. The caller owns the transaction; with problems,
// nothing is written.
func ImportInvoices(ctx context.Context, q core.Querier, orgID int64, actor *int64, rows []InvoiceRow, apply bool) (ImportResult, error) {
	res := ImportResult{Problems: []importers.Problem{}}
	settings, err := core.GetSettings(ctx, q, orgID)
	if err != nil {
		return ImportResult{}, err
	}

	type pending struct {
		row       InvoiceRow
		contactID int64
	}
	var toCreate []pending
	contacts := map[string][]int64{}

	for _, row := range rows {
		problem := func(field, msg string) {
			res.Problems = append(res.Problems, importers.Problem{Row: row.Row, Field: field, Message: msg})
		}

		existing, found, err := existingImport(ctx, q, orgID, ImportRef(row.Number))
		if err != nil {
			return ImportResult{}, err
		}
		if found {
			if !existing.matches(row) {
				problem("number", "already imported with different values")
				continue
			}
			res.Existing++
			continue
		}

		key := strings.ToLower(strings.TrimSpace(row.Customer))
		ids, ok := contacts[key]
		if !ok {
			ids, err = clientContactsNamed(ctx, q, orgID, key)
			if err != nil {
				return ImportResult{}, err
			}
			contacts[key] = ids
		}
		bad := false
		switch len(ids) {
		case 0:
			problem("customer", fmt.Sprintf("no client contact named %q", row.Customer))
			bad = true
		case 1:
		default:
			problem("customer", fmt.Sprintf("more than one client contact named %q", row.Customer))
			bad = true
		}
		if row.GST > 0 && !settings.GSTRegistered {
			problem("gst", "GST charged but organisation is not GST-registered")
			bad = true
		}
		if bad {
			continue
		}
		res.New++
		res.TotalCents += int64(row.Total)
		toCreate = append(toCreate, pending{row: row, contactID: ids[0]})
	}

	if !apply || len(res.Problems) > 0 {
		return res, nil
	}

	sort.SliceStable(toCreate, func(i, j int) bool {
		a, b := toCreate[i].row, toCreate[j].row
		if !a.IssueDate.Equal(b.IssueDate) {
			return a.IssueDate.Before(b.IssueDate)
		}
		return a.Row < b.Row
	})
	for _, p := range toCreate {
		row := p.row
		code := core.FRE
		if row.GST > 0 {
			code = core.GST
		}
		ref := ImportRef(row.Number)
		inv, err := core.ImportInvoice(ctx, q, orgID, core.ImportedInvoice{
			ContactID: p.contactID, ExternalRef: ref, IssueDate: row.IssueDate, DueDate: row.DueDate,
			Description: "Imported from ERPNext " + row.Number,
			Subtotal:    row.Subtotal, GST: row.GST, GSTCode: code,
		})
		if err != nil {
			return ImportResult{}, err
		}
		if paid := row.Total - row.Outstanding; paid > 0 {
			if _, _, err := core.RecordPayment(ctx, q, orgID, core.NewPayment{
				InvoiceID: inv.ID, Amount: paid, PaidOn: *row.PaidDate, Method: "other", ProviderRef: ref,
			}); err != nil {
				return ImportResult{}, err
			}
		}
		if err := core.Audit(ctx, q, orgID, actor, "invoice", inv.ID, "import", map[string]any{"external_ref": ref}); err != nil {
			return ImportResult{}, err
		}
	}
	return res, nil
}

// importedInvoice is what a re-import compares a row against.
type importedInvoice struct {
	issue, due                        time.Time
	subtotal, gst, total, outstanding core.Cents
}

func (e importedInvoice) matches(r InvoiceRow) bool {
	return e.issue.Equal(r.IssueDate) && e.due.Equal(r.DueDate) &&
		e.subtotal == r.Subtotal && e.gst == r.GST && e.total == r.Total && e.outstanding == r.Outstanding
}

func existingImport(ctx context.Context, q core.Querier, orgID int64, ref string) (importedInvoice, bool, error) {
	var e importedInvoice
	var subtotal, gst, total, paid int64
	err := q.QueryRow(ctx, `
		SELECT i.issue_date, i.due_date, i.subtotal, i.gst_total, i.total,
			(SELECT coalesce(sum(amount), 0) FROM payments p WHERE p.org_id = i.org_id AND p.invoice_id = i.id)
		FROM invoices i WHERE i.org_id = $1 AND i.external_ref = $2`,
		orgID, ref,
	).Scan(&e.issue, &e.due, &subtotal, &gst, &total, &paid)
	if errors.Is(err, pgx.ErrNoRows) {
		return importedInvoice{}, false, nil
	}
	if err != nil {
		return importedInvoice{}, false, err
	}
	e.subtotal, e.gst, e.total = core.Cents(subtotal), core.Cents(gst), core.Cents(total)
	e.outstanding = core.Cents(max(total-paid, 0))
	return e, true, nil
}

// clientContactsNamed returns the ids of orgID's client contacts whose
// trimmed, lower-cased name is key.
func clientContactsNamed(ctx context.Context, q core.Querier, orgID int64, key string) ([]int64, error) {
	rows, err := q.Query(ctx,
		`SELECT id FROM contacts WHERE org_id = $1 AND kind = 'client' AND lower(btrim(name)) = $2 ORDER BY id`,
		orgID, key)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}
