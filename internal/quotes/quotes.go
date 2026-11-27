package quotes

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"shigoto/internal/core"
	"shigoto/internal/timesheets"
)

// Field limits.
const (
	maxTitle       = 200
	maxDescription = 500
	maxTerms       = 4000
	maxLines       = 200
)

// invalidState is a 409 with a message safe to show.
func invalidState(msg string) error {
	return &core.PublicError{Err: core.ErrInvalidState, Msg: msg}
}

func invalidInput(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{core.ErrInvalidInput}, args...)...)
}

const quoteColumns = `id, org_id, contact_id, project_id, number, revision, parent_id, title, terms,
	status, issue_date, valid_until, subtotal, gst_total, total, pdf_file_id, sent_at,
	decided_at, decided_by, created_at`

func scanQuote(row pgx.Row) (Quote, error) {
	var q Quote
	var subtotal, gst, total int64
	err := row.Scan(&q.ID, &q.OrgID, &q.ContactID, &q.ProjectID, &q.Number, &q.Revision, &q.ParentID,
		&q.Title, &q.Terms, &q.Status, &q.IssueDate, &q.ValidUntil, &subtotal, &gst, &total,
		&q.PDFFileID, &q.SentAt, &q.DecidedAt, &q.DecidedBy, &q.CreatedAt)
	q.Subtotal, q.GSTTotal, q.Total = core.Cents(subtotal), core.Cents(gst), core.Cents(total)
	if errors.Is(err, pgx.ErrNoRows) {
		return Quote{}, core.ErrNotFound
	}
	return q, err
}

// priced is a validated line with its amounts.
type priced struct {
	NewLine
	code      core.GSTCode
	amount    core.Cents
	gstAmount core.Cents
}

// validateDraft checks d against orgID (contact, project, GST registration)
// and prices its lines with the invoice rounding rule.
func validateDraft(ctx context.Context, q core.Querier, orgID int64, d Draft) (Draft, []priced, error) {
	d.Title = strings.TrimSpace(d.Title)
	if d.Title == "" {
		return d, nil, invalidInput("title is required")
	}
	if utf8.RuneCountInString(d.Title) > maxTitle {
		return d, nil, invalidInput("title must be at most %d characters", maxTitle)
	}
	if utf8.RuneCountInString(d.Terms) > maxTerms {
		return d, nil, invalidInput("terms must be at most %d characters", maxTerms)
	}
	if d.IssueDate.IsZero() || d.ValidUntil.IsZero() {
		return d, nil, invalidInput("issue_date and valid_until are required")
	}
	if d.ValidUntil.Before(d.IssueDate) {
		return d, nil, invalidInput("valid_until must not be before issue_date")
	}
	if len(d.Lines) == 0 {
		return d, nil, invalidInput("quote must have at least one line")
	}
	if len(d.Lines) > maxLines {
		return d, nil, invalidInput("quote must have at most %d lines", maxLines)
	}

	contact, err := core.GetContact(ctx, q, orgID, d.ContactID)
	if errors.Is(err, core.ErrNotFound) {
		return d, nil, invalidInput("contact_id not found")
	}
	if err != nil {
		return d, nil, err
	}
	if contact.Kind != "client" {
		return d, nil, invalidInput("contact_id is not a client")
	}
	if d.ProjectID != nil {
		p, err := timesheets.GetProject(ctx, q, orgID, *d.ProjectID)
		if errors.Is(err, core.ErrNotFound) {
			return d, nil, invalidInput("project_id not found")
		}
		if err != nil {
			return d, nil, err
		}
		if p.ClientContactID != d.ContactID {
			return d, nil, invalidInput("project belongs to another client")
		}
	}

	settings, err := core.GetSettings(ctx, q, orgID)
	if err != nil {
		return d, nil, err
	}
	lines := make([]priced, len(d.Lines))
	for i, l := range d.Lines {
		if l.Kind != KindLabour && l.Kind != KindMaterial {
			return d, nil, invalidInput("line %d: kind must be labour or material", i)
		}
		l.Description = strings.TrimSpace(l.Description)
		if l.Description == "" {
			return d, nil, invalidInput("line %d: description is required", i)
		}
		if utf8.RuneCountInString(l.Description) > maxDescription {
			return d, nil, invalidInput("line %d: description must be at most %d characters", i, maxDescription)
		}
		// Same bounds as invoice lines, which keep LineAmount within int64.
		if l.QuantityMilli < 1 || l.QuantityMilli > 10_000_000 {
			return d, nil, invalidInput("line %d: quantity_milli must be from 1 to 10,000,000", i)
		}
		if l.UnitAmount < 0 || l.UnitAmount > 100_000_000_000 {
			return d, nil, invalidInput("line %d: unit_amount must be from 0 to 100,000,000,000", i)
		}
		code := core.GSTCode(l.GSTCode)
		switch code {
		case core.GST, core.FRE, core.NT:
		default:
			return d, nil, invalidInput("line %d: gst_code must be GST, FRE or N-T", i)
		}
		if code == core.GST && !settings.GSTRegistered {
			return d, nil, invalidInput("line %d: org is not GST-registered", i)
		}
		amount := core.LineAmount(l.QuantityMilli, l.UnitAmount)
		lines[i] = priced{NewLine: l, code: code, amount: amount, gstAmount: core.GSTOnExclusive(amount, code)}
	}
	return d, lines, nil
}

func totals(lines []priced) (subtotal, gst core.Cents) {
	for _, l := range lines {
		subtotal += l.amount
		gst += l.gstAmount
	}
	return subtotal, gst
}

func insertLines(ctx context.Context, q core.Querier, orgID, quoteID int64, lines []priced) error {
	for i, l := range lines {
		if _, err := q.Exec(ctx, `
			INSERT INTO quote_lines (org_id, quote_id, position, kind, description, quantity_milli,
				unit_amount, gst_code, gst_amount, amount)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			orgID, quoteID, i, l.Kind, l.Description, l.QuantityMilli, int64(l.UnitAmount),
			string(l.code), int64(l.gstAmount), int64(l.amount)); err != nil {
			return err
		}
	}
	return nil
}

func nextQuoteNumber(ctx context.Context, q core.Querier, orgID int64) (int, error) {
	var n int
	err := q.QueryRow(ctx, `
		INSERT INTO quote_counters (org_id, last_number) VALUES ($1, 1)
		ON CONFLICT (org_id) DO UPDATE SET last_number = quote_counters.last_number + 1
		RETURNING last_number`, orgID).Scan(&n)
	return n, err
}

// CreateDraft validates d and inserts it as a new draft quote with the
// org's next quote number.
func CreateDraft(ctx context.Context, q core.Querier, orgID int64, d Draft) (Quote, error) {
	d, lines, err := validateDraft(ctx, q, orgID, d)
	if err != nil {
		return Quote{}, err
	}
	number, err := nextQuoteNumber(ctx, q, orgID)
	if err != nil {
		return Quote{}, err
	}
	return insertQuote(ctx, q, orgID, d, lines, number, 1, nil)
}

func insertQuote(ctx context.Context, q core.Querier, orgID int64, d Draft, lines []priced, number, revision int, parentID *int64) (Quote, error) {
	subtotal, gst := totals(lines)
	created, err := scanQuote(q.QueryRow(ctx, `
		INSERT INTO quotes (org_id, contact_id, project_id, number, revision, parent_id, title, terms,
			issue_date, valid_until, subtotal, gst_total, total)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING `+quoteColumns,
		orgID, d.ContactID, d.ProjectID, number, revision, parentID, d.Title, d.Terms,
		d.IssueDate, d.ValidUntil, int64(subtotal), int64(gst), int64(subtotal+gst)))
	if isUniqueViolation(err, "quotes_one_draft") {
		return Quote{}, invalidState("a draft revision already exists")
	}
	if err != nil {
		return Quote{}, err
	}
	if err := insertLines(ctx, q, orgID, created.ID, lines); err != nil {
		return Quote{}, err
	}
	return GetQuote(ctx, q, orgID, created.ID)
}

// GetQuote returns quote id in orgID with its lines, or core.ErrNotFound.
func GetQuote(ctx context.Context, q core.Querier, orgID, id int64) (Quote, error) {
	qt, err := scanQuote(q.QueryRow(ctx, `SELECT `+quoteColumns+` FROM quotes WHERE org_id = $1 AND id = $2`, orgID, id))
	if err != nil {
		return Quote{}, err
	}
	qt.Lines, err = quoteLines(ctx, q, orgID, id)
	return qt, err
}

func quoteLines(ctx context.Context, q core.Querier, orgID, quoteID int64) ([]Line, error) {
	rows, err := q.Query(ctx, `
		SELECT id, kind, description, quantity_milli, unit_amount, gst_code, gst_amount, amount
		FROM quote_lines WHERE org_id = $1 AND quote_id = $2 ORDER BY position`, orgID, quoteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	lines := []Line{}
	for rows.Next() {
		var l Line
		var unit, gst, amount int64
		var code string
		if err := rows.Scan(&l.ID, &l.Kind, &l.Description, &l.QuantityMilli, &unit, &code, &gst, &amount); err != nil {
			return nil, err
		}
		l.UnitAmount, l.GSTCode, l.GSTAmount, l.Amount = core.Cents(unit), core.GSTCode(code), core.Cents(gst), core.Cents(amount)
		lines = append(lines, l)
	}
	return lines, rows.Err()
}

// lockQuote locks and returns quote id in orgID (without lines).
func lockQuote(ctx context.Context, q core.Querier, orgID, id int64) (Quote, error) {
	return scanQuote(q.QueryRow(ctx, `SELECT `+quoteColumns+` FROM quotes WHERE org_id = $1 AND id = $2 FOR UPDATE`, orgID, id))
}

// UpdateDraft replaces draft id's content (lines wholesale). Only drafts
// can change: anything else is a 409 "quote is not a draft".
func UpdateDraft(ctx context.Context, q core.Querier, orgID, id int64, d Draft) (Quote, error) {
	current, err := lockQuote(ctx, q, orgID, id)
	if err != nil {
		return Quote{}, err
	}
	if current.Status != StatusDraft {
		return Quote{}, invalidState("quote is not a draft")
	}
	d, lines, err := validateDraft(ctx, q, orgID, d)
	if err != nil {
		return Quote{}, err
	}
	subtotal, gst := totals(lines)
	if _, err := q.Exec(ctx, `
		UPDATE quotes SET contact_id = $3, project_id = $4, title = $5, terms = $6, issue_date = $7,
			valid_until = $8, subtotal = $9, gst_total = $10, total = $11
		WHERE org_id = $1 AND id = $2`,
		orgID, id, d.ContactID, d.ProjectID, d.Title, d.Terms, d.IssueDate, d.ValidUntil,
		int64(subtotal), int64(gst), int64(subtotal+gst)); err != nil {
		return Quote{}, err
	}
	if _, err := q.Exec(ctx, `DELETE FROM quote_lines WHERE org_id = $1 AND quote_id = $2`, orgID, id); err != nil {
		return Quote{}, err
	}
	if err := insertLines(ctx, q, orgID, id, lines); err != nil {
		return Quote{}, err
	}
	return GetQuote(ctx, q, orgID, id)
}

// DeleteDraft deletes draft id and returns the file ids of its
// attachments, which the caller deletes if unreferenced after commit.
func DeleteDraft(ctx context.Context, q core.Querier, orgID, id int64) error {
	_, err := DeleteDraftFiles(ctx, q, orgID, id)
	return err
}

// DeleteDraftFiles is DeleteDraft returning the attachment file ids the
// caller should garbage-collect after commit.
func DeleteDraftFiles(ctx context.Context, q core.Querier, orgID, id int64) ([]int64, error) {
	current, err := lockQuote(ctx, q, orgID, id)
	if err != nil {
		return nil, err
	}
	if current.Status != StatusDraft {
		return nil, invalidState("quote is not a draft")
	}
	rows, err := q.Query(ctx, `DELETE FROM quote_attachments WHERE org_id = $1 AND quote_id = $2 RETURNING file_id`, orgID, id)
	if err != nil {
		return nil, err
	}
	fileIDs, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, err
	}
	if _, err := q.Exec(ctx, `DELETE FROM quotes WHERE org_id = $1 AND id = $2`, orgID, id); err != nil {
		return nil, err
	}
	return fileIDs, nil
}

// Decline marks sent (including expired) quote id declined by userID.
func Decline(ctx context.Context, q core.Querier, orgID, userID, id int64, now time.Time) (Quote, error) {
	current, err := lockQuote(ctx, q, orgID, id)
	if err != nil {
		return Quote{}, err
	}
	if current.Status != StatusSent {
		return Quote{}, invalidState("quote is not open")
	}
	if _, err := q.Exec(ctx, `UPDATE quotes SET status = 'declined', decided_at = $3, decided_by = $4 WHERE org_id = $1 AND id = $2`,
		orgID, id, now, userID); err != nil {
		return Quote{}, err
	}
	return GetQuote(ctx, q, orgID, id)
}

// Revise copies sent, expired or declined quote id into a new draft
// revision (same number, revision + 1) issued today and valid for the same
// number of days as the original. The original is superseded only when the
// revision is sent.
func Revise(ctx context.Context, q core.Querier, orgID, id int64, today time.Time) (Quote, error) {
	current, err := lockQuote(ctx, q, orgID, id)
	if err != nil {
		return Quote{}, err
	}
	if current.Status != StatusSent && current.Status != StatusDeclined {
		return Quote{}, invalidState("only a sent or declined quote can be revised")
	}
	var accepted, latest int
	if err := q.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE status = 'accepted'), max(revision)
		FROM quotes WHERE org_id = $1 AND number = $2`, orgID, current.Number).Scan(&accepted, &latest); err != nil {
		return Quote{}, err
	}
	if accepted > 0 {
		return Quote{}, invalidState("another revision of this quote was accepted")
	}
	lines, err := quoteLines(ctx, q, orgID, id)
	if err != nil {
		return Quote{}, err
	}
	validDays := int(current.ValidUntil.Sub(current.IssueDate).Hours() / 24)
	d := Draft{
		ContactID: current.ContactID, ProjectID: current.ProjectID, Title: current.Title, Terms: current.Terms,
		IssueDate: today, ValidUntil: today.AddDate(0, 0, validDays),
	}
	for _, l := range lines {
		d.Lines = append(d.Lines, NewLine{Kind: l.Kind, Description: l.Description, QuantityMilli: l.QuantityMilli,
			UnitAmount: l.UnitAmount, GSTCode: string(l.GSTCode)})
	}
	d, validated, err := validateDraft(ctx, q, orgID, d)
	if err != nil {
		return Quote{}, err
	}
	return insertQuote(ctx, q, orgID, d, validated, current.Number, latest+1, &current.ID)
}

// ListFilter narrows ListQuotes. Limit defaults to 50 (max 200).
type ListFilter struct {
	ContactID *int64
	ProjectID *int64
	Status    string
	Limit     int
	Offset    int
}

// ListQuotes lists orgID's quotes (without lines), newest first, plus the
// total match count ignoring Limit/Offset.
func ListQuotes(ctx context.Context, q core.Querier, orgID int64, f ListFilter) ([]Quote, int, error) {
	where := " WHERE org_id = $1"
	args := []any{orgID}
	add := func(clause string, v any) {
		args = append(args, v)
		where += fmt.Sprintf(" AND %s $%d", clause, len(args))
	}
	if f.ContactID != nil {
		add("contact_id =", *f.ContactID)
	}
	if f.ProjectID != nil {
		add("project_id =", *f.ProjectID)
	}
	if f.Status != "" {
		add("status =", f.Status)
	}
	var total int
	if err := q.QueryRow(ctx, `SELECT count(*) FROM quotes`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	limit = min(limit, 200)
	args = append(args, limit, max(f.Offset, 0))
	rows, err := q.Query(ctx, fmt.Sprintf(`SELECT `+quoteColumns+` FROM quotes`+where+
		` ORDER BY created_at DESC, id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Quote{}
	for rows.Next() {
		qt, err := scanQuote(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, qt)
	}
	return out, total, rows.Err()
}

func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}
