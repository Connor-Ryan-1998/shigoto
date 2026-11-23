package clients

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"shigoto/internal/auth"
	"shigoto/internal/module"
)

const dateLayout = "2006-01-02"

// routeAdder mounts fn at pattern behind middleware mw.
type routeAdder func(pattern string, mw func(http.Handler) http.Handler, fn func(http.ResponseWriter, *http.Request, module.Deps))

// RegisterRoutes mounts the staff client-portal routes: welcome note,
// checklist and documents for a client contact. They are core (always on):
// a client may have general documents without any projects.
func RegisterRoutes(mux *http.ServeMux, d module.Deps) {
	read := d.Auth.RequireOrg(auth.PermInvoicesRead)
	write := d.Auth.RequireOrg(auth.PermContactsWrite)
	var handle routeAdder = func(pattern string, mw func(http.Handler) http.Handler, fn func(http.ResponseWriter, *http.Request, module.Deps)) {
		mux.Handle(pattern, mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fn(w, r, d) })))
	}

	handle("GET /api/v1/orgs/{org}/contacts/{id}/welcome", read, getWelcome)
	handle("PUT /api/v1/orgs/{org}/contacts/{id}/welcome", write, putWelcome)
	handle("GET /api/v1/orgs/{org}/contacts/{id}/checklist", read, listChecklistHandler)
	handle("POST /api/v1/orgs/{org}/contacts/{id}/checklist", write, createChecklistHandler)
	handle("PUT /api/v1/orgs/{org}/contacts/{id}/checklist/{itemId}", write, updateChecklistHandler)
	handle("DELETE /api/v1/orgs/{org}/contacts/{id}/checklist/{itemId}", write, deleteChecklistHandler)
	handle("POST /api/v1/orgs/{org}/contacts/{id}/checklist/{itemId}/done", write, doneChecklistHandler)

	registerDocumentRoutes(handle, read, write)
}

func pathInt(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return id, err == nil && id > 0
}

type welcomeResponse struct {
	Notes     string  `json:"notes"`
	UpdatedAt *string `json:"updated_at"`
}

func welcomeToResponse(w Welcome) welcomeResponse {
	return welcomeResponse{Notes: w.Notes, UpdatedAt: formatTime(w.UpdatedAt)}
}

func formatTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

func formatDate(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(dateLayout)
	return &s
}

func getWelcome(w http.ResponseWriter, r *http.Request, d module.Deps) {
	contactID, ok := pathInt(r, "id")
	if !ok {
		module.WriteError(w, http.StatusNotFound, "not found")
		return
	}
	p := auth.From(r.Context())
	wel, err := GetWelcome(r.Context(), d.Pool, p.OrgID, contactID)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	module.WriteJSON(w, http.StatusOK, welcomeToResponse(wel))
}

func putWelcome(w http.ResponseWriter, r *http.Request, d module.Deps) {
	contactID, ok := pathInt(r, "id")
	if !ok {
		module.WriteError(w, http.StatusNotFound, "not found")
		return
	}
	var body struct {
		Notes *string `json:"notes"`
	}
	if err := module.ReadJSON(r, &body); err != nil {
		module.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Notes == nil {
		module.WriteError(w, http.StatusUnprocessableEntity, "notes is required")
		return
	}
	p := auth.From(r.Context())
	var wel Welcome
	err := module.RunInTx(r.Context(), d.Pool, func(tx pgx.Tx) error {
		var err error
		wel, err = PutWelcome(r.Context(), tx, p.OrgID, contactID, p.UserID, *body.Notes)
		return err
	})
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	module.WriteJSON(w, http.StatusOK, welcomeToResponse(wel))
}

type checklistRequest struct {
	Action  string          `json:"action"`
	Owner   string          `json:"owner"`
	DueDate json.RawMessage `json:"due_date"`
}

// ChecklistItemResponse is the JSON shape of a checklist item, shared with
// the portal.
type ChecklistItemResponse struct {
	ID       int64   `json:"id"`
	Position int     `json:"position"`
	Action   string  `json:"action"`
	Owner    string  `json:"owner"`
	DueDate  *string `json:"due_date"`
	DoneAt   *string `json:"done_at"`
}

// ChecklistToResponse converts items to their JSON shape.
func ChecklistToResponse(items []ChecklistItem) []ChecklistItemResponse {
	out := make([]ChecklistItemResponse, len(items))
	for i, it := range items {
		out[i] = checklistItemToResponse(it)
	}
	return out
}

func checklistItemToResponse(it ChecklistItem) ChecklistItemResponse {
	return ChecklistItemResponse{ID: it.ID, Position: it.Position, Action: it.Action, Owner: it.Owner,
		DueDate: formatDate(it.DueDate), DoneAt: formatTime(it.DoneAt)}
}

func (b checklistRequest) item() (ChecklistItem, error) {
	it := ChecklistItem{Action: b.Action, Owner: b.Owner}
	if b.DueDate != nil && string(b.DueDate) != "null" {
		var s string
		if err := json.Unmarshal(b.DueDate, &s); err != nil {
			return it, invalid("due_date must be YYYY-MM-DD or null")
		}
		if s != "" {
			t, err := time.Parse(dateLayout, s)
			if err != nil {
				return it, invalid("due_date must be YYYY-MM-DD or null")
			}
			it.DueDate = &t
		}
	}
	return it, nil
}

func listChecklistHandler(w http.ResponseWriter, r *http.Request, d module.Deps) {
	contactID, ok := pathInt(r, "id")
	if !ok {
		module.WriteError(w, http.StatusNotFound, "not found")
		return
	}
	p := auth.From(r.Context())
	items, err := ListChecklist(r.Context(), d.Pool, p.OrgID, contactID)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	module.WriteJSON(w, http.StatusOK, ChecklistToResponse(items))
}

func createChecklistHandler(w http.ResponseWriter, r *http.Request, d module.Deps) {
	contactID, ok := pathInt(r, "id")
	if !ok {
		module.WriteError(w, http.StatusNotFound, "not found")
		return
	}
	var body checklistRequest
	if err := module.ReadJSON(r, &body); err != nil {
		module.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	it, err := body.item()
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	p := auth.From(r.Context())
	var created ChecklistItem
	err = module.RunInTx(r.Context(), d.Pool, func(tx pgx.Tx) error {
		var err error
		created, err = CreateChecklistItem(r.Context(), tx, p.OrgID, contactID, p.UserID, it)
		return err
	})
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	module.WriteJSON(w, http.StatusCreated, checklistItemToResponse(created))
}

func updateChecklistHandler(w http.ResponseWriter, r *http.Request, d module.Deps) {
	contactID, ok1 := pathInt(r, "id")
	itemID, ok2 := pathInt(r, "itemId")
	if !ok1 || !ok2 {
		module.WriteError(w, http.StatusNotFound, "not found")
		return
	}
	var body checklistRequest
	if err := module.ReadJSON(r, &body); err != nil {
		module.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	it, err := body.item()
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	p := auth.From(r.Context())
	var updated ChecklistItem
	err = module.RunInTx(r.Context(), d.Pool, func(tx pgx.Tx) error {
		if _, err := RequireClient(r.Context(), tx, p.OrgID, contactID); err != nil {
			return err
		}
		var err error
		updated, err = UpdateChecklistItem(r.Context(), tx, p.OrgID, contactID, p.UserID, itemID, it)
		return err
	})
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	module.WriteJSON(w, http.StatusOK, checklistItemToResponse(updated))
}

func doneChecklistHandler(w http.ResponseWriter, r *http.Request, d module.Deps) {
	contactID, ok1 := pathInt(r, "id")
	itemID, ok2 := pathInt(r, "itemId")
	if !ok1 || !ok2 {
		module.WriteError(w, http.StatusNotFound, "not found")
		return
	}
	var body struct {
		Done *bool `json:"done"`
	}
	if err := module.ReadJSON(r, &body); err != nil {
		module.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Done == nil {
		module.WriteError(w, http.StatusUnprocessableEntity, "done is required")
		return
	}
	p := auth.From(r.Context())
	var updated ChecklistItem
	err := module.RunInTx(r.Context(), d.Pool, func(tx pgx.Tx) error {
		var err error
		updated, err = SetChecklistDone(r.Context(), tx, p.OrgID, contactID, p.UserID, itemID, *body.Done)
		return err
	})
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	module.WriteJSON(w, http.StatusOK, checklistItemToResponse(updated))
}

func deleteChecklistHandler(w http.ResponseWriter, r *http.Request, d module.Deps) {
	contactID, ok1 := pathInt(r, "id")
	itemID, ok2 := pathInt(r, "itemId")
	if !ok1 || !ok2 {
		module.WriteError(w, http.StatusNotFound, "not found")
		return
	}
	p := auth.From(r.Context())
	err := module.RunInTx(r.Context(), d.Pool, func(tx pgx.Tx) error {
		return DeleteChecklistItem(r.Context(), tx, p.OrgID, contactID, p.UserID, itemID)
	})
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
