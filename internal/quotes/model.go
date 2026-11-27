// Package quotes is the quotes module: itemised quotes for a client
// contact with labour and material lines, a PDF frozen when the quote is
// sent, internal attachments, and accepting a quote into a timesheets
// project. Quotes never touch invoices, payments, recon or reports.
package quotes

import (
	"fmt"
	"time"

	"shigoto/internal/core"
)

// Stored statuses. Expired is never stored: see DisplayStatus.
const (
	StatusDraft      = "draft"
	StatusSent       = "sent"
	StatusAccepted   = "accepted"
	StatusDeclined   = "declined"
	StatusSuperseded = "superseded"
	// DisplayExpired is the display status of a sent quote past its
	// valid-until date. It is a nudge, not a lock: staff can still accept
	// or decline it.
	DisplayExpired = "expired"
)

// Line kinds. A labour line is hours (QuantityMilli = hours × 1000) at an
// hourly rate (UnitAmount); accepting a quote turns each into a task.
const (
	KindLabour   = "labour"
	KindMaterial = "material"
)

// Line is one stored quote line.
type Line struct {
	ID            int64
	Kind          string
	Description   string
	QuantityMilli int64
	UnitAmount    core.Cents
	GSTCode       core.GSTCode
	GSTAmount     core.Cents
	Amount        core.Cents
}

// Quote is one quote revision. Lines is filled by GetQuote, not by
// ListQuotes.
type Quote struct {
	ID         int64
	OrgID      int64
	ContactID  int64
	ProjectID  *int64
	Number     int
	Revision   int
	ParentID   *int64
	Title      string
	Terms      string
	Status     string
	IssueDate  time.Time
	ValidUntil time.Time
	Subtotal   core.Cents
	GSTTotal   core.Cents
	Total      core.Cents
	PDFFileID  *int64
	SentAt     *time.Time
	DecidedAt  *time.Time
	DecidedBy  *int64
	CreatedAt  time.Time
	Lines      []Line
}

// NewLine is one line of a draft being created or replaced.
type NewLine struct {
	Kind          string
	Description   string
	QuantityMilli int64
	UnitAmount    core.Cents
	GSTCode       string
}

// Draft is the editable content of a draft quote.
type Draft struct {
	ContactID  int64
	ProjectID  *int64
	Title      string
	Terms      string
	IssueDate  time.Time
	ValidUntil time.Time
	Lines      []NewLine
}

// FormatNumber is a quote's display number: Q-0007, or Q-0007-R2 for a
// later revision.
func FormatNumber(number, revision int) string {
	if revision > 1 {
		return fmt.Sprintf("Q-%04d-R%d", number, revision)
	}
	return fmt.Sprintf("Q-%04d", number)
}

// Label is q's display number.
func (q Quote) Label() string { return FormatNumber(q.Number, q.Revision) }

// DisplayStatus is q's status as shown to people: a sent quote whose
// valid-until date is before today shows as expired.
func (q Quote) DisplayStatus(today time.Time) string {
	if q.Status == StatusSent && q.ValidUntil.Before(today) {
		return DisplayExpired
	}
	return q.Status
}
