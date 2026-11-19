package property

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"shigoto/internal/core"
)

// MaxAvailabilityWindows caps a ticket's tenant availability windows.
const MaxAvailabilityWindows = 10

// AvailabilityWindow is a time the tenant says suits them: a date and a
// morning (08:00-12:00), afternoon (12:00-17:00) or custom HH:MM range.
type AvailabilityWindow struct {
	Date  time.Time
	Slot  string
	Start *string
	End   *string
}

// Visit is a booked tradesperson visit. Entry permission belongs to the
// visit, never the ticket: see EntryState.
type Visit struct {
	ID                int64
	TicketID          int64
	SupplierContactID int64
	SupplierName      string
	StartsAt          time.Time
	EndsAt            time.Time
	Status            string
	TenantConfirmedAt *time.Time
	ChangeNote        string
	EntryMode         *string
	EntryGrantedAt    *time.Time
	EntryWithdrawnAt  *time.Time
	CreatedAt         time.Time
}

// Open reports whether v is booked, confirmed or awaiting a change.
func (v Visit) Open() bool {
	return v.Status == "booked" || v.Status == "confirmed" || v.Status == "change_requested"
}

// EntryState is what staff may rely on: "permitted" only for a confirmed
// visit with entry granted and not withdrawn; "tenant_home" when the
// tenant confirmed they'll be in; "withdrawn"; otherwise "unconfirmed".
func (v Visit) EntryState() string {
	switch {
	case v.EntryWithdrawnAt != nil:
		return "withdrawn"
	case v.Status != "confirmed" || v.EntryMode == nil:
		return "unconfirmed"
	default:
		return *v.EntryMode
	}
}

var timeOfDay = func(s string) bool {
	_, err := time.Parse("15:04", s)
	return err == nil
}

func validateWindows(ws []AvailabilityWindow, today time.Time) error {
	if len(ws) > MaxAvailabilityWindows {
		return invalid("at most %d times", MaxAvailabilityWindows)
	}
	for _, w := range ws {
		if w.Date.Before(today) {
			return invalid("times must be today or later")
		}
		switch w.Slot {
		case "morning", "afternoon":
		case "custom":
			if w.Start == nil || w.End == nil || !timeOfDay(*w.Start) || !timeOfDay(*w.End) || *w.Start >= *w.End {
				return invalid("custom times need a start before the end (HH:MM)")
			}
		default:
			return invalid("unknown time slot %q", w.Slot)
		}
	}
	return nil
}

func replaceAvailability(ctx context.Context, tx core.Querier, orgID, ticketID int64, ws []AvailabilityWindow) error {
	if _, err := tx.Exec(ctx, `DELETE FROM maintenance_availability WHERE org_id = $1 AND ticket_id = $2`, orgID, ticketID); err != nil {
		return err
	}
	for _, w := range ws {
		var start, end *string
		if w.Slot == "custom" {
			start, end = w.Start, w.End
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO maintenance_availability (org_id, ticket_id, date, slot, start_time, end_time)
			VALUES ($1, $2, $3, $4, $5::time, $6::time)`, orgID, ticketID, w.Date, w.Slot, start, end); err != nil {
			return err
		}
	}
	return nil
}

// SetAvailability replaces the ticket's availability windows. Allowed while
// the ticket is open and has no confirmed visit.
func SetAvailability(ctx context.Context, tx core.Querier, orgID, ticketID, userID int64, ws []AvailabilityWindow, today time.Time) error {
	if err := validateWindows(ws, today); err != nil {
		return err
	}
	t, err := lockTicket(ctx, tx, orgID, ticketID)
	if err != nil {
		return err
	}
	if t.Terminal() {
		return core.ErrInvalidState
	}
	if open, err := OpenVisit(ctx, tx, orgID, ticketID); err != nil {
		return err
	} else if open != nil && open.Status == "confirmed" {
		return core.ErrInvalidState
	}
	if err := replaceAvailability(ctx, tx, orgID, ticketID, ws); err != nil {
		return err
	}
	if _, err := addEvent(ctx, tx, orgID, ticketID, "availability_updated", &userID, "", map[string]any{"windows": len(ws)}, true); err != nil {
		return err
	}
	return core.Audit(ctx, tx, orgID, &userID, "maintenance_ticket", ticketID, "availability", map[string]any{"windows": len(ws)})
}

// ListAvailability returns the ticket's windows, earliest first.
func ListAvailability(ctx context.Context, q core.Querier, orgID, ticketID int64) ([]AvailabilityWindow, error) {
	rows, err := q.Query(ctx, `
		SELECT date, slot, to_char(start_time, 'HH24:MI'), to_char(end_time, 'HH24:MI')
		FROM maintenance_availability WHERE org_id = $1 AND ticket_id = $2
		ORDER BY date, coalesce(start_time, CASE slot WHEN 'morning' THEN '08:00'::time ELSE '12:00'::time END), id`, orgID, ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AvailabilityWindow{}
	for rows.Next() {
		var w AvailabilityWindow
		if err := rows.Scan(&w.Date, &w.Slot, &w.Start, &w.End); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// SetEntryPreference records the tenant's stated entry preference (nil
// clears it). It is never permission; see Visit.EntryState.
func SetEntryPreference(ctx context.Context, tx core.Querier, orgID, ticketID int64, pref *string) error {
	if pref != nil && *pref != "tenant_home" && *pref != "permitted" {
		return invalid("entry preference must be tenant_home or permitted")
	}
	tag, err := tx.Exec(ctx, `UPDATE maintenance_tickets SET entry_preference = $3 WHERE org_id = $1 AND id = $2`, orgID, ticketID, pref)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return core.ErrNotFound
	}
	return nil
}

// EntryPreference returns the ticket's stated preference, if any.
func EntryPreference(ctx context.Context, q core.Querier, orgID, ticketID int64) (*string, error) {
	var p *string
	err := q.QueryRow(ctx, `SELECT entry_preference FROM maintenance_tickets WHERE org_id = $1 AND id = $2`, orgID, ticketID).Scan(&p)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, core.ErrNotFound
	}
	return p, err
}

const visitSelect = `
	SELECT v.id, v.ticket_id, v.supplier_contact_id, coalesce(c.name, ''), v.starts_at, v.ends_at, v.status,
		v.tenant_confirmed_at, v.change_note, v.entry_mode, v.entry_granted_at, v.entry_withdrawn_at, v.created_at
	FROM maintenance_visits v LEFT JOIN contacts c ON c.id = v.supplier_contact_id`

func scanVisit(row pgx.Row) (Visit, error) {
	var v Visit
	err := row.Scan(&v.ID, &v.TicketID, &v.SupplierContactID, &v.SupplierName, &v.StartsAt, &v.EndsAt, &v.Status,
		&v.TenantConfirmedAt, &v.ChangeNote, &v.EntryMode, &v.EntryGrantedAt, &v.EntryWithdrawnAt, &v.CreatedAt)
	return v, err
}

// GetVisit returns visit id on ticketID in orgID, or core.ErrNotFound.
func GetVisit(ctx context.Context, q core.Querier, orgID, ticketID, id int64) (Visit, error) {
	v, err := scanVisit(q.QueryRow(ctx, visitSelect+` WHERE v.org_id = $1 AND v.ticket_id = $2 AND v.id = $3`, orgID, ticketID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Visit{}, core.ErrNotFound
	}
	return v, err
}

// OpenVisit returns the ticket's open visit, or nil.
func OpenVisit(ctx context.Context, q core.Querier, orgID, ticketID int64) (*Visit, error) {
	v, err := scanVisit(q.QueryRow(ctx, visitSelect+`
		WHERE v.org_id = $1 AND v.ticket_id = $2 AND v.status IN ('booked', 'confirmed', 'change_requested')`, orgID, ticketID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// ListVisits returns every visit on the ticket, newest first.
func ListVisits(ctx context.Context, q core.Querier, orgID, ticketID int64) ([]Visit, error) {
	rows, err := q.Query(ctx, visitSelect+` WHERE v.org_id = $1 AND v.ticket_id = $2 ORDER BY v.starts_at DESC, v.id DESC`, orgID, ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Visit{}
	for rows.Next() {
		v, err := scanVisit(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// lockVisit locks a visit and its (open) ticket.
func lockVisit(ctx context.Context, tx core.Querier, orgID, visitID int64) (Visit, Ticket, error) {
	var ticketID int64
	err := tx.QueryRow(ctx, `SELECT ticket_id FROM maintenance_visits WHERE org_id = $1 AND id = $2`, orgID, visitID).Scan(&ticketID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Visit{}, Ticket{}, core.ErrNotFound
	}
	if err != nil {
		return Visit{}, Ticket{}, err
	}
	t, err := lockTicket(ctx, tx, orgID, ticketID)
	if err != nil {
		return Visit{}, Ticket{}, err
	}
	if _, err := tx.Exec(ctx, `SELECT 1 FROM maintenance_visits WHERE id = $1 FOR UPDATE`, visitID); err != nil {
		return Visit{}, Ticket{}, err
	}
	v, err := GetVisit(ctx, tx, orgID, ticketID, visitID)
	if err != nil {
		return Visit{}, Ticket{}, err
	}
	if t.Terminal() {
		return Visit{}, Ticket{}, core.ErrInvalidState
	}
	return v, t, nil
}

func visitEvent(ctx context.Context, tx core.Querier, orgID int64, v Visit, kind string, userID int64, body string) error {
	if _, err := addEvent(ctx, tx, orgID, v.TicketID, kind, &userID, body, map[string]any{
		"visit_id": v.ID, "starts_at": v.StartsAt, "ends_at": v.EndsAt,
	}, true); err != nil {
		return err
	}
	return core.Audit(ctx, tx, orgID, &userID, "maintenance_visit", v.ID, kind, map[string]any{"ticket_id": v.TicketID})
}

func isOneOpenViolation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "maintenance_visits_one_open"
}

// BookVisit books the ticket's visit with supplierID (or the ticket's
// supplier) from starts to ends. The ticket becomes 'scheduled'. A ticket
// with an open visit gives core.ErrInvalidState.
func BookVisit(ctx context.Context, tx core.Querier, orgID, ticketID, userID int64, supplierID *int64, starts, ends time.Time) (Visit, error) {
	if !ends.After(starts) {
		return Visit{}, invalid("the visit must end after it starts")
	}
	t, err := lockTicket(ctx, tx, orgID, ticketID)
	if err != nil {
		return Visit{}, err
	}
	if t.Terminal() {
		return Visit{}, core.ErrInvalidState
	}
	if supplierID != nil && (t.SupplierContactID == nil || *supplierID != *t.SupplierContactID) {
		if t, err = AssignSupplier(ctx, tx, orgID, ticketID, userID, *supplierID); err != nil {
			return Visit{}, err
		}
	}
	if t.SupplierContactID == nil {
		return Visit{}, invalid("assign a tradesperson first")
	}
	if open, err := OpenVisit(ctx, tx, orgID, ticketID); err != nil {
		return Visit{}, err
	} else if open != nil {
		return Visit{}, core.ErrInvalidState
	}
	var id int64
	err = tx.QueryRow(ctx, `
		INSERT INTO maintenance_visits (org_id, ticket_id, supplier_contact_id, starts_at, ends_at)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`, orgID, ticketID, *t.SupplierContactID, starts, ends).Scan(&id)
	if isOneOpenViolation(err) {
		return Visit{}, core.ErrInvalidState
	}
	if err != nil {
		return Visit{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE maintenance_tickets SET status = 'scheduled' WHERE org_id = $1 AND id = $2`, orgID, ticketID); err != nil {
		return Visit{}, err
	}
	v, err := GetVisit(ctx, tx, orgID, ticketID, id)
	if err != nil {
		return Visit{}, err
	}
	return v, visitEvent(ctx, tx, orgID, v, "visit_booked", userID, "")
}

// RescheduleVisit moves an open visit. Any tenant confirmation and entry
// permission are cleared: consent never carries to a new time.
func RescheduleVisit(ctx context.Context, tx core.Querier, orgID, visitID, userID int64, starts, ends time.Time) (Visit, error) {
	if !ends.After(starts) {
		return Visit{}, invalid("the visit must end after it starts")
	}
	v, _, err := lockVisit(ctx, tx, orgID, visitID)
	if err != nil {
		return Visit{}, err
	}
	if !v.Open() {
		return Visit{}, core.ErrInvalidState
	}
	if _, err := tx.Exec(ctx, `
		UPDATE maintenance_visits SET starts_at = $2, ends_at = $3, status = 'booked', tenant_confirmed_at = NULL,
			change_note = '', entry_mode = NULL, entry_granted_at = NULL, entry_granted_by_user_id = NULL, entry_withdrawn_at = NULL
		WHERE id = $1`, visitID, starts, ends); err != nil {
		return Visit{}, err
	}
	v, err = GetVisit(ctx, tx, orgID, v.TicketID, visitID)
	if err != nil {
		return Visit{}, err
	}
	return v, visitEvent(ctx, tx, orgID, v, "visit_rescheduled", userID, "")
}

// CancelVisit cancels an open visit; the ticket returns to 'assigned'.
func CancelVisit(ctx context.Context, tx core.Querier, orgID, visitID, userID int64, reason string) (Visit, error) {
	v, _, err := lockVisit(ctx, tx, orgID, visitID)
	if err != nil {
		return Visit{}, err
	}
	if !v.Open() {
		return Visit{}, core.ErrInvalidState
	}
	return cancelVisitLocked(ctx, tx, orgID, v, userID, strings.TrimSpace(reason), "assigned")
}

func cancelVisitLocked(ctx context.Context, tx core.Querier, orgID int64, v Visit, userID int64, reason, ticketStatus string) (Visit, error) {
	if _, err := tx.Exec(ctx, `UPDATE maintenance_visits SET status = 'cancelled' WHERE id = $1`, v.ID); err != nil {
		return Visit{}, err
	}
	if ticketStatus != "" {
		if _, err := tx.Exec(ctx, `UPDATE maintenance_tickets SET status = $3 WHERE org_id = $1 AND id = $2`, orgID, v.TicketID, ticketStatus); err != nil {
			return Visit{}, err
		}
	}
	v.Status = "cancelled"
	return v, visitEvent(ctx, tx, orgID, v, "visit_cancelled", userID, reason)
}

// MarkVisitDone records that the visit happened. The ticket's status is
// unchanged: completing it is a separate step (a visit may only quote).
func MarkVisitDone(ctx context.Context, tx core.Querier, orgID, visitID, userID int64) (Visit, error) {
	v, _, err := lockVisit(ctx, tx, orgID, visitID)
	if err != nil {
		return Visit{}, err
	}
	if !v.Open() {
		return Visit{}, core.ErrInvalidState
	}
	if _, err := tx.Exec(ctx, `UPDATE maintenance_visits SET status = 'done' WHERE id = $1`, visitID); err != nil {
		return Visit{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE maintenance_tickets SET status = 'assigned' WHERE org_id = $1 AND id = $2`, orgID, v.TicketID); err != nil {
		return Visit{}, err
	}
	v.Status = "done"
	return v, visitEvent(ctx, tx, orgID, v, "visit_done", userID, "")
}

// ConfirmVisit is the tenant accepting a booked visit, choosing whether
// they'll be home or permit entry. Entry permission takes effect here.
func ConfirmVisit(ctx context.Context, tx core.Querier, orgID, visitID, userID int64, entryMode string) (Visit, error) {
	if entryMode != "tenant_home" && entryMode != "permitted" {
		return Visit{}, invalid("entry must be tenant_home or permitted")
	}
	v, _, err := lockVisit(ctx, tx, orgID, visitID)
	if err != nil {
		return Visit{}, err
	}
	if v.Status != "booked" && v.Status != "change_requested" {
		return Visit{}, core.ErrInvalidState
	}
	granted := "NULL"
	if entryMode == "permitted" {
		granted = "now()"
	}
	if _, err := tx.Exec(ctx, fmt.Sprintf(`
		UPDATE maintenance_visits SET status = 'confirmed', tenant_confirmed_at = now(), change_note = '',
			entry_mode = $2, entry_granted_at = %s, entry_granted_by_user_id = CASE WHEN $2 = 'permitted' THEN $3::bigint END,
			entry_withdrawn_at = NULL
		WHERE id = $1`, granted), visitID, entryMode, userID); err != nil {
		return Visit{}, err
	}
	v, err = GetVisit(ctx, tx, orgID, v.TicketID, visitID)
	if err != nil {
		return Visit{}, err
	}
	if err := visitEvent(ctx, tx, orgID, v, "visit_confirmed", userID, ""); err != nil {
		return Visit{}, err
	}
	if entryMode == "permitted" {
		return v, visitEvent(ctx, tx, orgID, v, "entry_granted", userID, "")
	}
	return v, nil
}

// RequestVisitChange is the tenant asking for a different time, with a
// required note and optionally new availability windows.
func RequestVisitChange(ctx context.Context, tx core.Querier, orgID, visitID, userID int64, note string, ws []AvailabilityWindow, today time.Time) (Visit, error) {
	note = strings.TrimSpace(note)
	if note == "" || len([]rune(note)) > 2000 {
		return Visit{}, invalid("tell us what would suit you (up to 2000 characters)")
	}
	if ws != nil {
		if err := validateWindows(ws, today); err != nil {
			return Visit{}, err
		}
	}
	v, _, err := lockVisit(ctx, tx, orgID, visitID)
	if err != nil {
		return Visit{}, err
	}
	if !v.Open() {
		return Visit{}, core.ErrInvalidState
	}
	if _, err := tx.Exec(ctx, `
		UPDATE maintenance_visits SET status = 'change_requested', change_note = $2, tenant_confirmed_at = NULL,
			entry_mode = NULL, entry_granted_at = NULL, entry_granted_by_user_id = NULL, entry_withdrawn_at = NULL
		WHERE id = $1`, visitID, note); err != nil {
		return Visit{}, err
	}
	if ws != nil {
		if err := replaceAvailability(ctx, tx, orgID, v.TicketID, ws); err != nil {
			return Visit{}, err
		}
	}
	v, err = GetVisit(ctx, tx, orgID, v.TicketID, visitID)
	if err != nil {
		return Visit{}, err
	}
	return v, visitEvent(ctx, tx, orgID, v, "visit_change_requested", userID, note)
}

// WithdrawEntry withdraws entry permission on a confirmed visit before it
// starts (now is the caller's clock).
func WithdrawEntry(ctx context.Context, tx core.Querier, orgID, visitID, userID int64, now time.Time) (Visit, error) {
	v, _, err := lockVisit(ctx, tx, orgID, visitID)
	if err != nil {
		return Visit{}, err
	}
	if v.EntryState() != "permitted" {
		return Visit{}, core.ErrInvalidState
	}
	if !now.Before(v.StartsAt) {
		return Visit{}, invalid("visit has started")
	}
	if _, err := tx.Exec(ctx, `UPDATE maintenance_visits SET entry_withdrawn_at = $2 WHERE id = $1`, visitID, now); err != nil {
		return Visit{}, err
	}
	v, err = GetVisit(ctx, tx, orgID, v.TicketID, visitID)
	if err != nil {
		return Visit{}, err
	}
	return v, visitEvent(ctx, tx, orgID, v, "entry_withdrawn", userID, "")
}

func init() {
	// Closing a ticket cancels its open visit in the same transaction.
	beforeClose = func(ctx context.Context, tx core.Querier, orgID, ticketID, userID int64) error {
		open, err := OpenVisit(ctx, tx, orgID, ticketID)
		if err != nil || open == nil {
			return err
		}
		_, err = cancelVisitLocked(ctx, tx, orgID, *open, userID, "", "")
		return err
	}
}
