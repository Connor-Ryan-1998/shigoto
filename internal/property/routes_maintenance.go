package property

import (
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"shigoto/internal/auth"
	"shigoto/internal/core"
	"shigoto/internal/module"
)

// TicketSupplier is a ticket's supplier as shown in responses.
type TicketSupplier struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// TicketResponse is the JSON shape of a ticket, shared by staff and portal.
type TicketResponse struct {
	ID              int64           `json:"id"`
	Ref             string          `json:"ref"`
	PropertyID      int64           `json:"property_id"`
	PropertyAddress string          `json:"property_address"`
	LeaseID         *int64          `json:"lease_id"`
	Title           string          `json:"title"`
	Description     string          `json:"description"`
	Category        string          `json:"category"`
	Urgency         string          `json:"urgency"`
	Status          string          `json:"status"`
	Supplier        *TicketSupplier `json:"supplier"`
	AccessNotes     string          `json:"access_notes"`
	RaisedByName    string          `json:"raised_by_name"`
	FileCount       int             `json:"file_count"`
	CancelReason    string          `json:"cancel_reason"`
	CreatedAt       time.Time       `json:"created_at"`
	ViewedAt        *time.Time      `json:"viewed_at"`
	CompletedAt     *time.Time      `json:"completed_at"`
	CancelledAt     *time.Time      `json:"cancelled_at"`
}

// TicketToResponse converts t to its JSON shape.
func TicketToResponse(t Ticket) TicketResponse {
	r := TicketResponse{
		ID: t.ID, Ref: t.Ref, PropertyID: t.PropertyID, PropertyAddress: t.PropertyAddress, LeaseID: t.LeaseID,
		Title: t.Title, Description: t.Description, Category: t.Category, Urgency: t.Urgency, Status: t.Status,
		AccessNotes: t.AccessNotes, RaisedByName: t.RaisedByName, FileCount: t.FileCount, CancelReason: t.CancelReason,
		CreatedAt: t.CreatedAt, ViewedAt: t.ViewedAt, CompletedAt: t.CompletedAt, CancelledAt: t.CancelledAt,
	}
	if t.SupplierContactID != nil {
		r.Supplier = &TicketSupplier{ID: *t.SupplierContactID, Name: t.SupplierName}
	}
	return r
}

// EventResponse is the JSON shape of a timeline event.
type EventResponse struct {
	ID            int64          `json:"id"`
	Kind          string         `json:"kind"`
	Body          string         `json:"body"`
	Data          map[string]any `json:"data"`
	ActorName     string         `json:"actor_name"`
	TenantVisible bool           `json:"tenant_visible"`
	CreatedAt     time.Time      `json:"created_at"`
}

// EventsToResponse converts events to their JSON shape.
func EventsToResponse(events []TicketEvent) []EventResponse {
	out := make([]EventResponse, 0, len(events))
	for _, e := range events {
		out = append(out, EventToResponse(e))
	}
	return out
}

// EventToResponse converts e to its JSON shape.
func EventToResponse(e TicketEvent) EventResponse {
	return EventResponse{ID: e.ID, Kind: e.Kind, Body: e.Body, Data: e.Data, ActorName: e.ActorName, TenantVisible: e.TenantVisible, CreatedAt: e.CreatedAt}
}

// FilesToResponse converts files to their JSON shape.
func FilesToResponse(files []core.File) []module.FileResponse {
	out := make([]module.FileResponse, 0, len(files))
	for _, f := range files {
		out = append(out, module.FileToResponse(f))
	}
	return out
}

func registerMaintenanceRoutes(mux *http.ServeMux, d module.Deps) {
	read := func(h func(http.ResponseWriter, *http.Request, module.Deps)) http.Handler {
		return d.Auth.RequireOrg(auth.PermPropertyRead)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h(w, r, d) }))
	}
	write := func(h func(http.ResponseWriter, *http.Request, module.Deps)) http.Handler {
		return d.Auth.RequireOrg(auth.PermPropertyWrite)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h(w, r, d) }))
	}
	mux.Handle("GET /api/v1/orgs/{org}/properties/{id}/maintenance", read(listPropertyTickets))
	mux.Handle("POST /api/v1/orgs/{org}/properties/{id}/maintenance", write(createStaffTicket))
	mux.Handle("GET /api/v1/orgs/{org}/maintenance/{ticketId}", read(getStaffTicket))
	mux.Handle("POST /api/v1/orgs/{org}/maintenance/{ticketId}/viewed", write(ticketAction(func(r *http.Request, tx pgx.Tx, p auth.Principal, id int64) (any, int, error) {
		t, err := MarkViewed(r.Context(), tx, p.OrgID, id, p.UserID)
		return TicketToResponse(t), http.StatusOK, err
	})))
	mux.Handle("PUT /api/v1/orgs/{org}/maintenance/{ticketId}/supplier", write(ticketAction(func(r *http.Request, tx pgx.Tx, p auth.Principal, id int64) (any, int, error) {
		var body struct {
			SupplierContactID int64 `json:"supplier_contact_id"`
		}
		if err := module.ReadJSON(r, &body); err != nil {
			return nil, 0, invalid("%v", err)
		}
		t, err := AssignSupplier(r.Context(), tx, p.OrgID, id, p.UserID, body.SupplierContactID)
		return TicketToResponse(t), http.StatusOK, err
	})))
	mux.Handle("POST /api/v1/orgs/{org}/maintenance/{ticketId}/complete", write(ticketAction(func(r *http.Request, tx pgx.Tx, p auth.Principal, id int64) (any, int, error) {
		var body struct {
			Note string `json:"note"`
		}
		if err := module.ReadJSON(r, &body); err != nil {
			return nil, 0, invalid("%v", err)
		}
		t, err := CompleteTicket(r.Context(), tx, p.OrgID, id, p.UserID, body.Note)
		return TicketToResponse(t), http.StatusOK, err
	})))
	mux.Handle("POST /api/v1/orgs/{org}/maintenance/{ticketId}/cancel", write(ticketAction(func(r *http.Request, tx pgx.Tx, p auth.Principal, id int64) (any, int, error) {
		var body struct {
			Reason string `json:"reason"`
		}
		if err := module.ReadJSON(r, &body); err != nil {
			return nil, 0, invalid("%v", err)
		}
		t, err := CancelTicket(r.Context(), tx, p.OrgID, id, p.UserID, body.Reason, false)
		return TicketToResponse(t), http.StatusOK, err
	})))
	mux.Handle("POST /api/v1/orgs/{org}/maintenance/{ticketId}/events", write(ticketAction(func(r *http.Request, tx pgx.Tx, p auth.Principal, id int64) (any, int, error) {
		var body struct {
			Kind string `json:"kind"`
			Body string `json:"body"`
		}
		if err := module.ReadJSON(r, &body); err != nil {
			return nil, 0, invalid("%v", err)
		}
		ev, err := AddTicketEvent(r.Context(), tx, p.OrgID, id, TicketEvent{Kind: body.Kind, Body: body.Body, ActorUserID: &p.UserID})
		return EventToResponse(ev), http.StatusCreated, err
	})))
	mux.Handle("POST /api/v1/orgs/{org}/maintenance/{ticketId}/files", write(uploadStaffTicketFile))
	mux.Handle("GET /api/v1/orgs/{org}/maintenance/{ticketId}/files/{fileId}", read(downloadStaffTicketFile))
}

// ticketAction runs f for the {ticketId} ticket in a transaction and
// writes its result (or mapped error).
func ticketAction(f func(r *http.Request, tx pgx.Tx, p auth.Principal, id int64) (any, int, error)) func(http.ResponseWriter, *http.Request, module.Deps) {
	return func(w http.ResponseWriter, r *http.Request, d module.Deps) {
		id, ok := pathInt64(r, "ticketId")
		if !ok {
			module.WriteError(w, http.StatusNotFound, "not found")
			return
		}
		p := auth.From(r.Context())
		var out any
		var status int
		err := module.RunInTx(r.Context(), d.Pool, func(tx pgx.Tx) error {
			var err error
			out, status, err = f(r, tx, p, id)
			return err
		})
		if err != nil {
			module.WriteCoreError(w, err)
			return
		}
		module.WriteJSON(w, status, out)
	}
}

func listPropertyTickets(w http.ResponseWriter, r *http.Request, d module.Deps) {
	id, ok := module.PathID(r)
	if !ok {
		module.WriteError(w, http.StatusNotFound, "not found")
		return
	}
	orgID := auth.From(r.Context()).OrgID
	if err := requireProperty(r.Context(), d.Pool, orgID, id); err != nil {
		module.WriteCoreError(w, err)
		return
	}
	tickets, err := ListTickets(r.Context(), d.Pool, orgID, TicketFilter{PropertyID: &id, OpenOnly: r.URL.Query().Get("status") != "all"})
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	out := make([]TicketResponse, 0, len(tickets))
	for _, t := range tickets {
		out = append(out, TicketToResponse(t))
	}
	module.WriteJSON(w, http.StatusOK, out)
}

// TicketRequest is the body for raising a ticket.
type TicketRequest struct {
	LeaseID        *int64  `json:"lease_id"`
	Title          string  `json:"title"`
	Description    string  `json:"description"`
	Category       string  `json:"category"`
	Urgency        string  `json:"urgency"`
	AccessNotes    string  `json:"access_notes"`
	PendingFileIDs []int64 `json:"pending_file_ids"`
}

func createStaffTicket(w http.ResponseWriter, r *http.Request, d module.Deps) {
	propertyID, ok := module.PathID(r)
	if !ok {
		module.WriteError(w, http.StatusNotFound, "not found")
		return
	}
	var body TicketRequest
	if err := module.ReadJSON(r, &body); err != nil {
		module.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	p := auth.From(r.Context())
	var t Ticket
	err := module.RunInTx(r.Context(), d.Pool, func(tx pgx.Tx) error {
		var err error
		t, err = CreateTicket(r.Context(), tx, p.OrgID, NewTicket{
			PropertyID: propertyID, LeaseID: body.LeaseID, UserID: p.UserID,
			Title: body.Title, Description: body.Description, Category: body.Category, Urgency: body.Urgency,
			AccessNotes: body.AccessNotes,
		}, today(d))
		return err
	})
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	module.WriteJSON(w, http.StatusCreated, TicketToResponse(t))
}

type ticketTenant struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Phone string `json:"phone"`
}

func getStaffTicket(w http.ResponseWriter, r *http.Request, d module.Deps) {
	id, ok := pathInt64(r, "ticketId")
	if !ok {
		module.WriteError(w, http.StatusNotFound, "not found")
		return
	}
	ctx, orgID := r.Context(), auth.From(r.Context()).OrgID
	t, err := GetTicket(ctx, d.Pool, orgID, id)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	tenants := []ticketTenant{}
	if t.LeaseID != nil {
		rows, err := d.Pool.Query(ctx, `
			SELECT c.id, c.name, coalesce(c.email, ''), coalesce(c.phone, '')
			FROM lease_tenants lt JOIN contacts c ON c.id = lt.contact_id
			WHERE lt.lease_id = $1 AND c.org_id = $2 ORDER BY lt.is_primary DESC, c.name`, *t.LeaseID, orgID)
		if err != nil {
			module.WriteCoreError(w, err)
			return
		}
		for rows.Next() {
			var tt ticketTenant
			if err := rows.Scan(&tt.ID, &tt.Name, &tt.Email, &tt.Phone); err != nil {
				rows.Close()
				module.WriteCoreError(w, err)
				return
			}
			tenants = append(tenants, tt)
		}
		rows.Close()
	}
	files, err := ListTicketFiles(ctx, d.Pool, orgID, id)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	events, err := ListTicketEvents(ctx, d.Pool, orgID, id, false)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	module.WriteJSON(w, http.StatusOK, map[string]any{
		"ticket": TicketToResponse(t), "tenants": tenants,
		"files": FilesToResponse(files), "events": EventsToResponse(events),
	})
}

func uploadStaffTicketFile(w http.ResponseWriter, r *http.Request, d module.Deps) {
	id, ok := pathInt64(r, "ticketId")
	if !ok {
		module.WriteError(w, http.StatusNotFound, "not found")
		return
	}
	p := auth.From(r.Context())
	if _, err := GetTicket(r.Context(), d.Pool, p.OrgID, id); err != nil {
		module.WriteCoreError(w, err)
		return
	}
	f, ok := module.HandleUpload(w, r, d, core.MediaPolicy(d.Cfg.UploadMaxBytes), func(tx pgx.Tx, f core.File) error {
		return AttachTicketFile(r.Context(), tx, p.OrgID, id, f.ID, p.UserID)
	})
	if !ok {
		return
	}
	module.WriteJSON(w, http.StatusCreated, module.FileToResponse(f))
}

func downloadStaffTicketFile(w http.ResponseWriter, r *http.Request, d module.Deps) {
	id, ok1 := pathInt64(r, "ticketId")
	fileID, ok2 := pathInt64(r, "fileId")
	if !ok1 || !ok2 {
		module.WriteError(w, http.StatusNotFound, "not found")
		return
	}
	ServeTicketFile(w, r, d, auth.From(r.Context()).OrgID, id, fileID)
}

// ServeTicketFile serves fileID if it is attached to ticketID, inline for
// media when ?inline=1.
func ServeTicketFile(w http.ResponseWriter, r *http.Request, d module.Deps, orgID, ticketID, fileID int64) {
	files, err := ListTicketFiles(r.Context(), d.Pool, orgID, ticketID)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	for _, f := range files {
		if f.ID == fileID {
			module.ServeFile(w, r, d.Store, f, r.URL.Query().Get("inline") == "1")
			return
		}
	}
	module.WriteError(w, http.StatusNotFound, "not found")
}
