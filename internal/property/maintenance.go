package property

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"shigoto/internal/core"
)

// MaxTicketFiles caps the media on one ticket (attached plus pending).
const MaxTicketFiles = 20

// TicketCategories and TicketUrgencies are the accepted values.
var (
	TicketCategories = []string{"plumbing", "electrical", "appliance", "heating_cooling", "doors_windows_locks", "pests", "garden_exterior", "other"}
	TicketUrgencies  = []string{"routine", "soon", "urgent"}
)

// Ticket is a maintenance (repair) request on a property.
type Ticket struct {
	ID                int64
	OrgID             int64
	Ref               string
	PropertyID        int64
	PropertyAddress   string
	LeaseID           *int64
	RaisedByContactID *int64
	RaisedByUserID    *int64
	RaisedByName      string
	SupplierContactID *int64
	SupplierName      string
	Title             string
	Description       string
	Category          string
	Urgency           string
	Status            string
	AccessNotes       string
	CancelReason      string
	FileCount         int
	ViewedAt          *time.Time
	CompletedAt       *time.Time
	CancelledAt       *time.Time
	CreatedAt         time.Time
}

// Terminal reports whether t is completed or cancelled.
func (t Ticket) Terminal() bool { return t.Status == "completed" || t.Status == "cancelled" }

// TicketEvent is one entry in a ticket's timeline. Notes are staff-only;
// every other kind is shown to tenants.
type TicketEvent struct {
	ID            int64
	Kind          string
	Body          string
	Data          map[string]any
	ActorUserID   *int64
	ActorName     string
	TenantVisible bool
	CreatedAt     time.Time
}

// NewTicket is a ticket to create. ContactID is set when a tenant raises
// it through the portal; then LeaseID is required, must include that
// contact, and must be active. PendingFileIDs are the raiser's own pending
// uploads to attach.
type NewTicket struct {
	PropertyID     int64
	LeaseID        *int64
	ContactID      *int64
	UserID         int64
	Title          string
	Description    string
	Category       string
	Urgency        string
	AccessNotes    string
	PendingFileIDs []int64
	// EntryPreference ("tenant_home" or "permitted") is only a stated
	// preference; Availability are times that suit the tenant.
	EntryPreference *string
	Availability    []AvailabilityWindow
}

// TicketFilter selects tickets for ListTickets. A non-nil LeaseIDs limits
// to those leases (an empty slice matches nothing).
type TicketFilter struct {
	PropertyID *int64
	LeaseIDs   []int64
	OpenOnly   bool
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", core.ErrInvalidInput, fmt.Sprintf(format, args...))
}

func oneOf(v string, set []string) bool {
	for _, s := range set {
		if v == s {
			return true
		}
	}
	return false
}

const ticketSelect = `
	SELECT t.id, t.org_id, t.ref_no, t.property_id, coalesce(nullif(p.address, ''), p.name), t.lease_id,
		t.raised_by_contact_id, t.raised_by_user_id,
		coalesce(rc.name, ru.name, ''), t.supplier_contact_id, coalesce(sc.name, ''),
		t.title, t.description, t.category, t.urgency, t.status, t.access_notes, t.cancel_reason,
		(SELECT count(*) FROM maintenance_ticket_files f WHERE f.ticket_id = t.id),
		t.viewed_at, t.completed_at, t.cancelled_at, t.created_at
	FROM maintenance_tickets t
	JOIN properties p ON p.id = t.property_id
	LEFT JOIN contacts rc ON rc.id = t.raised_by_contact_id
	LEFT JOIN users ru ON ru.id = t.raised_by_user_id
	LEFT JOIN contacts sc ON sc.id = t.supplier_contact_id`

func scanTicket(row pgx.Row) (Ticket, error) {
	var t Ticket
	var ref int
	err := row.Scan(&t.ID, &t.OrgID, &ref, &t.PropertyID, &t.PropertyAddress, &t.LeaseID,
		&t.RaisedByContactID, &t.RaisedByUserID, &t.RaisedByName, &t.SupplierContactID, &t.SupplierName,
		&t.Title, &t.Description, &t.Category, &t.Urgency, &t.Status, &t.AccessNotes, &t.CancelReason,
		&t.FileCount, &t.ViewedAt, &t.CompletedAt, &t.CancelledAt, &t.CreatedAt)
	t.Ref = fmt.Sprintf("R-%04d", ref)
	return t, err
}

// GetTicket returns ticket id in orgID, or core.ErrNotFound.
func GetTicket(ctx context.Context, q core.Querier, orgID, id int64) (Ticket, error) {
	t, err := scanTicket(q.QueryRow(ctx, ticketSelect+` WHERE t.org_id = $1 AND t.id = $2`, orgID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Ticket{}, core.ErrNotFound
	}
	return t, err
}

// ListTickets returns orgID's tickets matching f: open ones (urgent first)
// before closed ones, newest first within each.
func ListTickets(ctx context.Context, q core.Querier, orgID int64, f TicketFilter) ([]Ticket, error) {
	where := []string{"t.org_id = $1"}
	args := []any{orgID}
	if f.PropertyID != nil {
		args = append(args, *f.PropertyID)
		where = append(where, fmt.Sprintf("t.property_id = $%d", len(args)))
	}
	if f.LeaseIDs != nil {
		args = append(args, f.LeaseIDs)
		where = append(where, fmt.Sprintf("t.lease_id = ANY($%d)", len(args)))
	}
	if f.OpenOnly {
		where = append(where, "t.status NOT IN ('completed', 'cancelled')")
	}
	rows, err := q.Query(ctx, ticketSelect+` WHERE `+strings.Join(where, " AND ")+`
		ORDER BY t.status IN ('completed', 'cancelled'), t.urgency = 'urgent' DESC, t.created_at DESC, t.id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Ticket{}
	for rows.Next() {
		t, err := scanTicket(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// CreateTicket validates in and creates the ticket with its 'created'
// event, attaching in.PendingFileIDs (which must be in.UserID's own pending
// uploads in orgID, else core.ErrNotFound).
func CreateTicket(ctx context.Context, tx core.Querier, orgID int64, in NewTicket, today time.Time) (Ticket, error) {
	in.Title = strings.TrimSpace(in.Title)
	in.Description = strings.TrimSpace(in.Description)
	in.AccessNotes = strings.TrimSpace(in.AccessNotes)
	switch {
	case in.Title == "" || len([]rune(in.Title)) > 120:
		return Ticket{}, invalid("title must be 1 to 120 characters")
	case in.Description == "" || len([]rune(in.Description)) > 4000:
		return Ticket{}, invalid("description must be 1 to 4000 characters")
	case !oneOf(in.Category, TicketCategories):
		return Ticket{}, invalid("unknown category %q", in.Category)
	case !oneOf(in.Urgency, TicketUrgencies):
		return Ticket{}, invalid("unknown urgency %q", in.Urgency)
	case len([]rune(in.AccessNotes)) > 1000:
		return Ticket{}, invalid("access notes must be at most 1000 characters")
	case len(in.PendingFileIDs) > MaxTicketFiles:
		return Ticket{}, invalid("too many files")
	case in.ContactID != nil && in.LeaseID == nil:
		return Ticket{}, invalid("lease is required")
	case in.EntryPreference != nil && *in.EntryPreference != "tenant_home" && *in.EntryPreference != "permitted":
		return Ticket{}, invalid("entry preference must be tenant_home or permitted")
	}
	if err := validateWindows(in.Availability, today); err != nil {
		return Ticket{}, err
	}
	if err := requireProperty(ctx, tx, orgID, in.PropertyID); err != nil {
		return Ticket{}, err
	}
	if in.LeaseID != nil {
		lease, err := GetLease(ctx, tx, orgID, *in.LeaseID)
		if err != nil {
			return Ticket{}, err
		}
		if in.ContactID != nil {
			on, err := LeaseHasContact(ctx, tx, orgID, lease.ID, *in.ContactID)
			if err != nil {
				return Ticket{}, err
			}
			if !on {
				return Ticket{}, core.ErrNotFound
			}
			if !LeaseActive(lease, today) {
				return Ticket{}, invalid("lease is not active")
			}
		}
		if lease.PropertyID != in.PropertyID {
			return Ticket{}, invalid("lease is not on this property")
		}
	}

	var refNo int
	if err := tx.QueryRow(ctx, `UPDATE orgs SET maintenance_seq = maintenance_seq + 1 WHERE id = $1 RETURNING maintenance_seq`, orgID).Scan(&refNo); err != nil {
		return Ticket{}, err
	}
	var id int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO maintenance_tickets (org_id, ref_no, property_id, lease_id, raised_by_contact_id, raised_by_user_id,
			title, description, category, urgency, access_notes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING id`,
		orgID, refNo, in.PropertyID, in.LeaseID, in.ContactID, in.UserID,
		in.Title, in.Description, in.Category, in.Urgency, in.AccessNotes,
	).Scan(&id); err != nil {
		return Ticket{}, err
	}

	if len(in.PendingFileIDs) > 0 {
		rows, err := tx.Query(ctx, `
			DELETE FROM maintenance_pending_files
			WHERE org_id = $1 AND user_id = $2 AND id = ANY($3)
			RETURNING file_id`, orgID, in.UserID, in.PendingFileIDs)
		if err != nil {
			return Ticket{}, err
		}
		var fileIDs []int64
		for rows.Next() {
			var f int64
			if err := rows.Scan(&f); err != nil {
				rows.Close()
				return Ticket{}, err
			}
			fileIDs = append(fileIDs, f)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return Ticket{}, err
		}
		if len(fileIDs) != len(uniq(in.PendingFileIDs)) {
			return Ticket{}, core.ErrNotFound
		}
		for _, f := range fileIDs {
			if _, err := tx.Exec(ctx, `
				INSERT INTO maintenance_ticket_files (ticket_id, file_id, uploaded_by_user_id) VALUES ($1, $2, $3)
				ON CONFLICT DO NOTHING`, id, f, in.UserID); err != nil {
				return Ticket{}, err
			}
		}
	}

	if in.EntryPreference != nil {
		if err := SetEntryPreference(ctx, tx, orgID, id, in.EntryPreference); err != nil {
			return Ticket{}, err
		}
	}
	if err := replaceAvailability(ctx, tx, orgID, id, in.Availability); err != nil {
		return Ticket{}, err
	}
	if _, err := addEvent(ctx, tx, orgID, id, "created", &in.UserID, "", nil, true); err != nil {
		return Ticket{}, err
	}
	if err := core.Audit(ctx, tx, orgID, &in.UserID, "maintenance_ticket", id, "create", map[string]any{
		"property_id": in.PropertyID, "lease_id": in.LeaseID, "urgency": in.Urgency, "category": in.Category,
	}); err != nil {
		return Ticket{}, err
	}
	return GetTicket(ctx, tx, orgID, id)
}

func uniq(ids []int64) []int64 {
	seen := map[int64]bool{}
	var out []int64
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// lockTicket locks ticket id and returns it.
func lockTicket(ctx context.Context, tx core.Querier, orgID, id int64) (Ticket, error) {
	var locked int64
	err := tx.QueryRow(ctx, `SELECT id FROM maintenance_tickets WHERE org_id = $1 AND id = $2 FOR UPDATE`, orgID, id).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return Ticket{}, core.ErrNotFound
	}
	if err != nil {
		return Ticket{}, err
	}
	return GetTicket(ctx, tx, orgID, id)
}

func addEvent(ctx context.Context, tx core.Querier, orgID, ticketID int64, kind string, actor *int64, body string, data map[string]any, tenantVisible bool) (TicketEvent, error) {
	if data == nil {
		data = map[string]any{}
	}
	ev := TicketEvent{Kind: kind, Body: body, Data: data, ActorUserID: actor, TenantVisible: tenantVisible}
	err := tx.QueryRow(ctx, `
		INSERT INTO maintenance_events (org_id, ticket_id, kind, actor_user_id, body, data, tenant_visible)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id, created_at`,
		orgID, ticketID, kind, actor, body, data, tenantVisible).Scan(&ev.ID, &ev.CreatedAt)
	return ev, err
}

// MarkViewed moves a 'new' ticket to 'viewed' and records it once; on any
// other status it changes nothing. Concurrent callers produce one event.
func MarkViewed(ctx context.Context, tx core.Querier, orgID, id, userID int64) (Ticket, error) {
	tag, err := tx.Exec(ctx, `
		UPDATE maintenance_tickets SET status = 'viewed', viewed_at = now()
		WHERE org_id = $1 AND id = $2 AND status = 'new'`, orgID, id)
	if err != nil {
		return Ticket{}, err
	}
	if tag.RowsAffected() == 1 {
		if _, err := addEvent(ctx, tx, orgID, id, "viewed", &userID, "", nil, true); err != nil {
			return Ticket{}, err
		}
		if err := core.Audit(ctx, tx, orgID, &userID, "maintenance_ticket", id, "viewed", nil); err != nil {
			return Ticket{}, err
		}
	}
	return GetTicket(ctx, tx, orgID, id)
}

// AssignSupplier sets the ticket's supplier (a supplier contact in orgID,
// else core.ErrInvalidInput). A new or viewed ticket becomes 'assigned'.
func AssignSupplier(ctx context.Context, tx core.Querier, orgID, id, userID, supplierID int64) (Ticket, error) {
	t, err := lockTicket(ctx, tx, orgID, id)
	if err != nil {
		return Ticket{}, err
	}
	if t.Terminal() {
		return Ticket{}, core.ErrInvalidState
	}
	var kind, name string
	err = tx.QueryRow(ctx, `SELECT kind, name FROM contacts WHERE org_id = $1 AND id = $2`, orgID, supplierID).Scan(&kind, &name)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && kind != "supplier") {
		return Ticket{}, invalid("supplier must be a supplier contact")
	}
	if err != nil {
		return Ticket{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE maintenance_tickets SET supplier_contact_id = $3,
			status = CASE WHEN status IN ('new', 'viewed') THEN 'assigned' ELSE status END,
			viewed_at = coalesce(viewed_at, now())
		WHERE org_id = $1 AND id = $2`, orgID, id, supplierID); err != nil {
		return Ticket{}, err
	}
	if _, err := addEvent(ctx, tx, orgID, id, "assigned", &userID, "", map[string]any{"supplier": name}, true); err != nil {
		return Ticket{}, err
	}
	if err := core.Audit(ctx, tx, orgID, &userID, "maintenance_ticket", id, "assign", map[string]any{"supplier_contact_id": supplierID}); err != nil {
		return Ticket{}, err
	}
	return GetTicket(ctx, tx, orgID, id)
}

// CompleteTicket marks an open ticket completed, with an optional note
// shown to tenants.
func CompleteTicket(ctx context.Context, tx core.Querier, orgID, id, userID int64, note string) (Ticket, error) {
	return closeTicket(ctx, tx, orgID, id, userID, "completed", strings.TrimSpace(note), false)
}

// CancelTicket cancels an open ticket with a reason. A tenant (byTenant)
// may only cancel while it is new or viewed.
func CancelTicket(ctx context.Context, tx core.Querier, orgID, id, userID int64, reason string, byTenant bool) (Ticket, error) {
	return closeTicket(ctx, tx, orgID, id, userID, "cancelled", strings.TrimSpace(reason), byTenant)
}

func closeTicket(ctx context.Context, tx core.Querier, orgID, id, userID int64, to, body string, byTenant bool) (Ticket, error) {
	if len([]rune(body)) > 4000 {
		return Ticket{}, invalid("note must be at most 4000 characters")
	}
	t, err := lockTicket(ctx, tx, orgID, id)
	if err != nil {
		return Ticket{}, err
	}
	if t.Terminal() || (byTenant && t.Status != "new" && t.Status != "viewed") {
		return Ticket{}, core.ErrInvalidState
	}
	if err := beforeClose(ctx, tx, orgID, id, userID); err != nil {
		return Ticket{}, err
	}
	sql := `UPDATE maintenance_tickets SET status = 'completed', completed_at = now() WHERE org_id = $1 AND id = $2`
	if to == "cancelled" {
		sql = `UPDATE maintenance_tickets SET status = 'cancelled', cancelled_at = now(), cancel_reason = $3 WHERE org_id = $1 AND id = $2`
	}
	args := []any{orgID, id}
	if to == "cancelled" {
		args = append(args, body)
	}
	if _, err := tx.Exec(ctx, sql, args...); err != nil {
		return Ticket{}, err
	}
	if _, err := addEvent(ctx, tx, orgID, id, to, &userID, body, map[string]any{"by_tenant": byTenant}, true); err != nil {
		return Ticket{}, err
	}
	if err := core.Audit(ctx, tx, orgID, &userID, "maintenance_ticket", id, to, map[string]any{"by_tenant": byTenant}); err != nil {
		return Ticket{}, err
	}
	return GetTicket(ctx, tx, orgID, id)
}

// beforeClose is a hook for work that must happen when a ticket closes
// (cancelling its open visit, once visits exist).
var beforeClose = func(ctx context.Context, tx core.Querier, orgID, ticketID, userID int64) error { return nil }

// AddTicketEvent adds a comment (shown to tenants) or note (staff only).
// Comments on a closed ticket give core.ErrInvalidState; staff notes are
// always allowed.
func AddTicketEvent(ctx context.Context, tx core.Querier, orgID, ticketID int64, ev TicketEvent) (TicketEvent, error) {
	body := strings.TrimSpace(ev.Body)
	if ev.Kind != "comment" && ev.Kind != "note" {
		return TicketEvent{}, invalid("kind must be comment or note")
	}
	if body == "" || len([]rune(body)) > 4000 {
		return TicketEvent{}, invalid("message must be 1 to 4000 characters")
	}
	t, err := lockTicket(ctx, tx, orgID, ticketID)
	if err != nil {
		return TicketEvent{}, err
	}
	if ev.Kind == "comment" && t.Terminal() {
		return TicketEvent{}, core.ErrInvalidState
	}
	out, err := addEvent(ctx, tx, orgID, ticketID, ev.Kind, ev.ActorUserID, body, nil, ev.Kind == "comment")
	if err != nil {
		return TicketEvent{}, err
	}
	if err := core.Audit(ctx, tx, orgID, ev.ActorUserID, "maintenance_ticket", ticketID, ev.Kind, nil); err != nil {
		return TicketEvent{}, err
	}
	return out, nil
}

// AttachTicketFile attaches an org file to an open ticket, enforcing
// MaxTicketFiles.
func AttachTicketFile(ctx context.Context, tx core.Querier, orgID, ticketID, fileID, userID int64) error {
	t, err := lockTicket(ctx, tx, orgID, ticketID)
	if err != nil {
		return err
	}
	if t.Terminal() {
		return core.ErrInvalidState
	}
	if t.FileCount >= MaxTicketFiles {
		return invalid("too many files")
	}
	if _, err := core.GetFile(ctx, tx, orgID, fileID); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO maintenance_ticket_files (ticket_id, file_id, uploaded_by_user_id) VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING`, ticketID, fileID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	if _, err := addEvent(ctx, tx, orgID, ticketID, "file_added", &userID, "", map[string]any{"file_id": fileID}, true); err != nil {
		return err
	}
	return core.Audit(ctx, tx, orgID, &userID, "maintenance_ticket", ticketID, "file_added", map[string]any{"file_id": fileID})
}

// SavePendingFile records f as userID's pending upload (not yet on a
// ticket), enforcing MaxTicketFiles across their pending uploads.
func SavePendingFile(ctx context.Context, tx core.Querier, orgID, userID int64, f core.File) (int64, error) {
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM maintenance_pending_files WHERE org_id = $1 AND user_id = $2`, orgID, userID).Scan(&n); err != nil {
		return 0, err
	}
	if n >= MaxTicketFiles {
		return 0, invalid("too many files")
	}
	var id int64
	err := tx.QueryRow(ctx, `INSERT INTO maintenance_pending_files (org_id, file_id, user_id) VALUES ($1, $2, $3) RETURNING id`,
		orgID, f.ID, userID).Scan(&id)
	return id, err
}

// DeletePendingFile removes userID's own pending upload, or core.ErrNotFound.
func DeletePendingFile(ctx context.Context, tx core.Querier, orgID, userID, pendingID int64) error {
	tag, err := tx.Exec(ctx, `DELETE FROM maintenance_pending_files WHERE org_id = $1 AND user_id = $2 AND id = $3`, orgID, userID, pendingID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return core.ErrNotFound
	}
	return nil
}

// ListTicketEvents returns the ticket's timeline, oldest first; with
// tenantView only tenant-visible events.
func ListTicketEvents(ctx context.Context, q core.Querier, orgID, ticketID int64, tenantView bool) ([]TicketEvent, error) {
	rows, err := q.Query(ctx, `
		SELECT e.id, e.kind, e.body, e.data, e.actor_user_id, coalesce(u.name, ''), e.tenant_visible, e.created_at
		FROM maintenance_events e LEFT JOIN users u ON u.id = e.actor_user_id
		WHERE e.org_id = $1 AND e.ticket_id = $2 AND (e.tenant_visible OR NOT $3)
		ORDER BY e.created_at, e.id`, orgID, ticketID, tenantView)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TicketEvent{}
	for rows.Next() {
		var ev TicketEvent
		if err := rows.Scan(&ev.ID, &ev.Kind, &ev.Body, &ev.Data, &ev.ActorUserID, &ev.ActorName, &ev.TenantVisible, &ev.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// ListTicketFiles returns the files attached to a ticket, oldest first.
func ListTicketFiles(ctx context.Context, q core.Querier, orgID, ticketID int64) ([]core.File, error) {
	rows, err := q.Query(ctx, `
		SELECT f.id, f.org_id, f.sha256, f.mime, f.size_bytes, f.original_name, f.created_at
		FROM maintenance_ticket_files tf JOIN files f ON f.id = tf.file_id
		JOIN maintenance_tickets t ON t.id = tf.ticket_id
		WHERE t.org_id = $1 AND tf.ticket_id = $2 AND f.org_id = $1
		ORDER BY tf.created_at, f.id`, orgID, ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []core.File{}
	for rows.Next() {
		var f core.File
		if err := rows.Scan(&f.ID, &f.OrgID, &f.SHA256, &f.MIME, &f.SizeBytes, &f.OriginalName, &f.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// RepairItem is one open repair on the dashboard.
type RepairItem struct {
	TicketID        int64  `json:"ticket_id"`
	PropertyID      int64  `json:"property_id"`
	PropertyAddress string `json:"property_address"`
	Ref             string `json:"ref"`
	Title           string `json:"title"`
	Urgency         string `json:"urgency"`
	Status          string `json:"status"`
}

// RepairsSummary is the dashboard's open-repairs card: the count of open
// tickets and the five most pressing.
type RepairsSummary struct {
	OpenCount int          `json:"open_count"`
	Items     []RepairItem `json:"items"`
}

// OpenRepairs summarises orgID's open tickets, most urgent first, then new
// before acknowledged, then oldest first.
func OpenRepairs(ctx context.Context, q core.Querier, orgID int64) (RepairsSummary, error) {
	s := RepairsSummary{Items: []RepairItem{}}
	if err := q.QueryRow(ctx, `SELECT count(*) FROM maintenance_tickets WHERE org_id = $1 AND status NOT IN ('completed', 'cancelled')`, orgID).Scan(&s.OpenCount); err != nil {
		return s, err
	}
	rows, err := q.Query(ctx, `
		SELECT t.id, t.property_id, coalesce(nullif(p.address, ''), p.name), t.ref_no, t.title, t.urgency, t.status
		FROM maintenance_tickets t JOIN properties p ON p.id = t.property_id
		WHERE t.org_id = $1 AND t.status NOT IN ('completed', 'cancelled')
		ORDER BY CASE t.urgency WHEN 'urgent' THEN 0 WHEN 'soon' THEN 1 ELSE 2 END,
			t.status <> 'new', t.created_at, t.id
		LIMIT 5`, orgID)
	if err != nil {
		return s, err
	}
	defer rows.Close()
	for rows.Next() {
		var it RepairItem
		var ref int
		if err := rows.Scan(&it.TicketID, &it.PropertyID, &it.PropertyAddress, &ref, &it.Title, &it.Urgency, &it.Status); err != nil {
			return s, err
		}
		it.Ref = fmt.Sprintf("R-%04d", ref)
		s.Items = append(s.Items, it)
	}
	return s, rows.Err()
}
