// Package clients owns the client portal records staff maintain for a
// client contact: the welcome note, the kickoff checklist, documents shared
// with or sent for signature to the client, and the signatures themselves.
// Every function is scoped by org and contact; only contacts of kind
// "client" take these records.
package clients

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"shigoto/internal/core"
)

// ErrNotClient means the contact exists in the org but isn't a client.
var ErrNotClient error = &core.PublicError{Err: core.ErrInvalidInput, Msg: "contact must be a client"}

const (
	maxNotes          = 4000
	maxChecklistText  = 500
	maxChecklistItems = 20
)

func invalid(msg string) error { return &core.PublicError{Err: core.ErrInvalidInput, Msg: msg} }

// RequireClient returns contactID's contact in orgID. It is core.ErrNotFound
// when the contact doesn't exist in orgID and ErrNotClient when it isn't a
// client.
func RequireClient(ctx context.Context, q core.Querier, orgID, contactID int64) (core.Contact, error) {
	c, err := core.GetContact(ctx, q, orgID, contactID)
	if err != nil {
		return core.Contact{}, err
	}
	if c.Kind != "client" {
		return core.Contact{}, ErrNotClient
	}
	return c, nil
}

// Welcome is a client's welcome note. UpdatedAt is nil until staff first
// save one.
type Welcome struct {
	Notes     string
	UpdatedAt *time.Time
}

// GetWelcome returns contactID's welcome note, empty when none is saved.
func GetWelcome(ctx context.Context, q core.Querier, orgID, contactID int64) (Welcome, error) {
	if _, err := RequireClient(ctx, q, orgID, contactID); err != nil {
		return Welcome{}, err
	}
	var w Welcome
	var at time.Time
	err := q.QueryRow(ctx, `SELECT notes, updated_at FROM client_welcome WHERE org_id = $1 AND contact_id = $2`,
		orgID, contactID).Scan(&w.Notes, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return Welcome{}, nil
	}
	if err != nil {
		return Welcome{}, err
	}
	w.UpdatedAt = &at
	return w, nil
}

// PutWelcome saves contactID's welcome note (at most 4000 characters).
func PutWelcome(ctx context.Context, q core.Querier, orgID, contactID, userID int64, notes string) (Welcome, error) {
	if _, err := RequireClient(ctx, q, orgID, contactID); err != nil {
		return Welcome{}, err
	}
	if utf8.RuneCountInString(notes) > maxNotes {
		return Welcome{}, invalid(fmt.Sprintf("notes must be at most %d characters", maxNotes))
	}
	var w Welcome
	var at time.Time
	err := q.QueryRow(ctx, `
		INSERT INTO client_welcome (org_id, contact_id, notes, updated_by)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (org_id, contact_id) DO UPDATE
			SET notes = EXCLUDED.notes, updated_by = EXCLUDED.updated_by, updated_at = now()
		RETURNING notes, updated_at`, orgID, contactID, notes, userID).Scan(&w.Notes, &at)
	if err != nil {
		return Welcome{}, err
	}
	w.UpdatedAt = &at
	if err := core.Audit(ctx, q, orgID, &userID, "client_welcome", contactID, "update", nil); err != nil {
		return Welcome{}, err
	}
	return w, nil
}

// ChecklistItem is one action staff need from the client before kickoff.
// DoneAt is set when staff confirm the client has done it.
type ChecklistItem struct {
	ID       int64
	Position int
	Action   string
	Owner    string
	DueDate  *time.Time
	DoneAt   *time.Time
}

const checklistColumns = `id, position, action, owner, due_date, done_at`

func scanChecklistItem(row pgx.Row) (ChecklistItem, error) {
	var it ChecklistItem
	err := row.Scan(&it.ID, &it.Position, &it.Action, &it.Owner, &it.DueDate, &it.DoneAt)
	return it, err
}

func validateChecklistItem(it *ChecklistItem) error {
	it.Action = strings.TrimSpace(it.Action)
	it.Owner = strings.TrimSpace(it.Owner)
	if it.Action == "" {
		return invalid("action is required")
	}
	if utf8.RuneCountInString(it.Action) > maxChecklistText {
		return invalid(fmt.Sprintf("action must be at most %d characters", maxChecklistText))
	}
	if utf8.RuneCountInString(it.Owner) > maxChecklistText {
		return invalid(fmt.Sprintf("owner must be at most %d characters", maxChecklistText))
	}
	return nil
}

// ListChecklist returns contactID's checklist in position order.
func ListChecklist(ctx context.Context, q core.Querier, orgID, contactID int64) ([]ChecklistItem, error) {
	if _, err := RequireClient(ctx, q, orgID, contactID); err != nil {
		return nil, err
	}
	return listChecklist(ctx, q, orgID, contactID)
}

func listChecklist(ctx context.Context, q core.Querier, orgID, contactID int64) ([]ChecklistItem, error) {
	rows, err := q.Query(ctx, `SELECT `+checklistColumns+` FROM client_checklist_items
		WHERE org_id = $1 AND contact_id = $2 ORDER BY position, id`, orgID, contactID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ChecklistItem{}
	for rows.Next() {
		it, err := scanChecklistItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// CreateChecklistItem appends it to contactID's checklist. A contact holds
// at most 20 items.
func CreateChecklistItem(ctx context.Context, q core.Querier, orgID, contactID, userID int64, it ChecklistItem) (ChecklistItem, error) {
	if _, err := RequireClient(ctx, q, orgID, contactID); err != nil {
		return ChecklistItem{}, err
	}
	if err := validateChecklistItem(&it); err != nil {
		return ChecklistItem{}, err
	}
	// Lock the contact row so concurrent creates can't both pass the cap.
	if _, err := q.Exec(ctx, `SELECT 1 FROM contacts WHERE org_id = $1 AND id = $2 FOR UPDATE`, orgID, contactID); err != nil {
		return ChecklistItem{}, err
	}
	var count, next int
	if err := q.QueryRow(ctx, `SELECT count(*), coalesce(max(position), 0) + 1 FROM client_checklist_items
		WHERE org_id = $1 AND contact_id = $2`, orgID, contactID).Scan(&count, &next); err != nil {
		return ChecklistItem{}, err
	}
	if count >= maxChecklistItems {
		return ChecklistItem{}, invalid("checklist is full")
	}
	created, err := scanChecklistItem(q.QueryRow(ctx, `
		INSERT INTO client_checklist_items (org_id, contact_id, position, action, owner, due_date)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING `+checklistColumns,
		orgID, contactID, next, it.Action, it.Owner, it.DueDate))
	if err != nil {
		return ChecklistItem{}, err
	}
	if err := core.Audit(ctx, q, orgID, &userID, "client_checklist_item", created.ID, "create", nil); err != nil {
		return ChecklistItem{}, err
	}
	return created, nil
}

// UpdateChecklistItem replaces id's action, owner and due date. id must
// belong to contactID, otherwise core.ErrNotFound.
func UpdateChecklistItem(ctx context.Context, q core.Querier, orgID, contactID, userID, id int64, it ChecklistItem) (ChecklistItem, error) {
	if err := validateChecklistItem(&it); err != nil {
		return ChecklistItem{}, err
	}
	updated, err := scanChecklistItem(q.QueryRow(ctx, `
		UPDATE client_checklist_items SET action = $1, owner = $2, due_date = $3
		WHERE org_id = $4 AND contact_id = $5 AND id = $6 RETURNING `+checklistColumns,
		it.Action, it.Owner, it.DueDate, orgID, contactID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return ChecklistItem{}, core.ErrNotFound
	}
	if err != nil {
		return ChecklistItem{}, err
	}
	if err := core.Audit(ctx, q, orgID, &userID, "client_checklist_item", id, "update", nil); err != nil {
		return ChecklistItem{}, err
	}
	return updated, nil
}

// SetChecklistDone marks id done (staff confirmed it) or not done.
func SetChecklistDone(ctx context.Context, q core.Querier, orgID, contactID, userID, id int64, done bool) (ChecklistItem, error) {
	var doneBy *int64
	if done {
		doneBy = &userID
	}
	updated, err := scanChecklistItem(q.QueryRow(ctx, `
		UPDATE client_checklist_items
		SET done_at = CASE WHEN $1 THEN coalesce(done_at, now()) END, done_by = $2
		WHERE org_id = $3 AND contact_id = $4 AND id = $5 RETURNING `+checklistColumns,
		done, doneBy, orgID, contactID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return ChecklistItem{}, core.ErrNotFound
	}
	if err != nil {
		return ChecklistItem{}, err
	}
	action := "undone"
	if done {
		action = "done"
	}
	if err := core.Audit(ctx, q, orgID, &userID, "client_checklist_item", id, action, nil); err != nil {
		return ChecklistItem{}, err
	}
	return updated, nil
}

// DeleteChecklistItem removes id from contactID's checklist.
func DeleteChecklistItem(ctx context.Context, q core.Querier, orgID, contactID, userID, id int64) error {
	tag, err := q.Exec(ctx, `DELETE FROM client_checklist_items WHERE org_id = $1 AND contact_id = $2 AND id = $3`,
		orgID, contactID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return core.ErrNotFound
	}
	return core.Audit(ctx, q, orgID, &userID, "client_checklist_item", id, "delete", nil)
}
