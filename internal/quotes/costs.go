package quotes

import (
	"context"
	"time"

	"shigoto/internal/core"
	"shigoto/internal/timesheets"
)

// CostLine is one expense line tagged to a project, ex GST.
type CostLine struct {
	ExpenseID   int64
	SpentOn     time.Time
	Description string
	Category    string
	Supplier    string
	Amount      core.Cents
}

// Costs compares a project's quoted, invoiced and actual spend. Every
// figure excludes GST so the three compare on the same basis.
type Costs struct {
	// Quoted sums every accepted quote linked to the project; declined and
	// superseded quotes never count.
	QuotedLabour    core.Cents
	QuotedMaterials core.Cents
	QuotedTotal     core.Cents
	// Invoiced sums the project's lines on open and paid invoices.
	Invoiced core.Cents
	// ExpensesTotal sums expense lines tagged to the project.
	ExpensesTotal core.Cents
	Expenses      []CostLine
}

// ProjectCosts returns projectID's costs in orgID, or core.ErrNotFound.
func ProjectCosts(ctx context.Context, q core.Querier, orgID, projectID int64) (Costs, error) {
	if _, err := timesheets.GetProject(ctx, q, orgID, projectID); err != nil {
		return Costs{}, err
	}
	var c Costs
	var labour, materials, invoiced int64
	if err := q.QueryRow(ctx, `
		SELECT coalesce(sum(l.amount) FILTER (WHERE l.kind = 'labour'), 0),
			coalesce(sum(l.amount) FILTER (WHERE l.kind = 'material'), 0)
		FROM quote_lines l JOIN quotes qt ON qt.id = l.quote_id AND qt.org_id = l.org_id
		WHERE qt.org_id = $1 AND qt.project_id = $2 AND qt.status = 'accepted'`,
		orgID, projectID).Scan(&labour, &materials); err != nil {
		return Costs{}, err
	}
	// invoice_lines.amount excludes GST (core.Totals adds GST on top).
	if err := q.QueryRow(ctx, `
		SELECT coalesce(sum(l.amount), 0)
		FROM invoice_lines l JOIN invoices i ON i.id = l.invoice_id AND i.org_id = l.org_id
		WHERE l.org_id = $1 AND l.project_id = $2 AND i.status IN ('open', 'paid')`,
		orgID, projectID).Scan(&invoiced); err != nil {
		return Costs{}, err
	}
	c.QuotedLabour, c.QuotedMaterials = core.Cents(labour), core.Cents(materials)
	c.QuotedTotal = c.QuotedLabour + c.QuotedMaterials
	c.Invoiced = core.Cents(invoiced)

	// expense_lines.amount includes GST; subtract it.
	rows, err := q.Query(ctx, `
		SELECT e.id, e.spent_on, e.description, cat.name, coalesce(s.name, ''), l.amount - l.gst_amount
		FROM expense_lines l
		JOIN expenses e ON e.id = l.expense_id AND e.org_id = l.org_id
		JOIN categories cat ON cat.id = l.category_id
		LEFT JOIN contacts s ON s.id = e.supplier_contact_id
		WHERE l.org_id = $1 AND l.project_id = $2
		ORDER BY e.spent_on DESC, l.id DESC`, orgID, projectID)
	if err != nil {
		return Costs{}, err
	}
	defer rows.Close()
	c.Expenses = []CostLine{}
	for rows.Next() {
		var l CostLine
		var amount int64
		if err := rows.Scan(&l.ExpenseID, &l.SpentOn, &l.Description, &l.Category, &l.Supplier, &amount); err != nil {
			return Costs{}, err
		}
		l.Amount = core.Cents(amount)
		c.ExpensesTotal += l.Amount
		c.Expenses = append(c.Expenses, l)
	}
	return c, rows.Err()
}
