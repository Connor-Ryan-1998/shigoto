package portal

import (
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"shigoto/internal/auth"
	"shigoto/internal/core"
	"shigoto/internal/module"
	"shigoto/internal/property"
)

// registerMaintenanceRoutes mounts tenant repair tickets: list and raise
// (with pending media uploads), and per ticket read, comment, add media,
// cancel and download media. Tenant-only; a ticket is visible to every
// tenant on its lease.
func registerMaintenanceRoutes(mux *http.ServeMux, d module.Deps) {
	tenant := func(h func(http.ResponseWriter, *http.Request, module.Deps, auth.Principal)) http.Handler {
		return d.Auth.RequirePortal(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := auth.From(r.Context())
			if !d.Cfg.Modules["property"] || p.Role != auth.Tenant || p.ContactID == nil {
				module.WriteError(w, http.StatusNotFound, "not found")
				return
			}
			h(w, r, d, p)
		}))
	}
	ticket := func(h func(http.ResponseWriter, *http.Request, module.Deps, auth.Principal, property.Ticket)) http.Handler {
		return tenant(func(w http.ResponseWriter, r *http.Request, d module.Deps, p auth.Principal) {
			t, ok := tenantTicket(w, r, d, p)
			if ok {
				h(w, r, d, p, t)
			}
		})
	}
	mux.Handle("GET /api/v1/portal/{org}/maintenance", tenant(listTenantTickets))
	mux.Handle("POST /api/v1/portal/{org}/maintenance", tenant(createTenantTicket))
	mux.Handle("POST /api/v1/portal/{org}/maintenance/uploads", tenant(uploadPending))
	mux.Handle("DELETE /api/v1/portal/{org}/maintenance/uploads/{pendingId}", tenant(deletePending))
	mux.Handle("GET /api/v1/portal/{org}/maintenance/{ticketId}", ticket(getTenantTicket))
	mux.Handle("POST /api/v1/portal/{org}/maintenance/{ticketId}/comments", ticket(commentTenantTicket))
	mux.Handle("POST /api/v1/portal/{org}/maintenance/{ticketId}/cancel", ticket(cancelTenantTicket))
	mux.Handle("POST /api/v1/portal/{org}/maintenance/{ticketId}/files", ticket(uploadTenantTicketFile))
	mux.Handle("PUT /api/v1/portal/{org}/maintenance/{ticketId}/availability", ticket(setTenantAvailability))
	mux.Handle("PUT /api/v1/portal/{org}/maintenance/{ticketId}/entry-preference", ticket(setTenantEntryPreference))
	mux.Handle("POST /api/v1/portal/{org}/maintenance/{ticketId}/visits/{visitId}/confirm", ticket(tenantVisitAction("confirm")))
	mux.Handle("POST /api/v1/portal/{org}/maintenance/{ticketId}/visits/{visitId}/request-change", ticket(tenantVisitAction("request-change")))
	mux.Handle("POST /api/v1/portal/{org}/maintenance/{ticketId}/visits/{visitId}/withdraw-entry", ticket(tenantVisitAction("withdraw-entry")))
	mux.Handle("GET /api/v1/portal/{org}/maintenance/{ticketId}/files/{fileId}", ticket(func(w http.ResponseWriter, r *http.Request, d module.Deps, p auth.Principal, t property.Ticket) {
		fileID, ok := pathID(r, "fileId")
		if !ok {
			module.WriteError(w, http.StatusNotFound, "not found")
			return
		}
		property.ServeTicketFile(w, r, d, p.OrgID, t.ID, fileID)
	}))
}

// tenantTicket loads the {ticketId} ticket if it is on a lease the caller
// is listed on; otherwise it writes a bare 404.
func tenantTicket(w http.ResponseWriter, r *http.Request, d module.Deps, p auth.Principal) (property.Ticket, bool) {
	id, ok := pathID(r, "ticketId")
	if !ok {
		module.WriteError(w, http.StatusNotFound, "not found")
		return property.Ticket{}, false
	}
	t, err := property.GetTicket(r.Context(), d.Pool, p.OrgID, id)
	if err == nil && t.LeaseID != nil {
		var on bool
		on, err = property.LeaseHasContact(r.Context(), d.Pool, p.OrgID, *t.LeaseID, *p.ContactID)
		if err == nil && on {
			return t, true
		}
	}
	if err != nil && !errors.Is(err, core.ErrNotFound) {
		module.WriteCoreError(w, err)
		return property.Ticket{}, false
	}
	module.WriteError(w, http.StatusNotFound, "not found")
	return property.Ticket{}, false
}

type tenantLease struct {
	ID              int64  `json:"id"`
	PropertyAddress string `json:"property_address"`
	Active          bool   `json:"active"`
}

func listTenantTickets(w http.ResponseWriter, r *http.Request, d module.Deps, p auth.Principal) {
	ctx := r.Context()
	leases, err := ListLeases(ctx, d.Pool, p.OrgID, *p.ContactID, property.TodayIn(d.Cfg.Location))
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	ids := make([]int64, 0, len(leases))
	outLeases := make([]tenantLease, 0, len(leases))
	for _, l := range leases {
		ids = append(ids, l.ID)
		addr := l.PropertyAddress
		if addr == "" {
			addr = l.PropertyName
		}
		outLeases = append(outLeases, tenantLease{ID: l.ID, PropertyAddress: addr, Active: l.Active})
	}
	tickets, err := property.ListTickets(ctx, d.Pool, p.OrgID, property.TicketFilter{LeaseIDs: ids})
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	out, err := property.WithOpenVisits(ctx, d.Pool, p.OrgID, tickets, d.Cfg.Location)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	var phone string
	if err := d.Pool.QueryRow(ctx, `SELECT emergency_phone FROM orgs WHERE id = $1`, p.OrgID).Scan(&phone); err != nil {
		module.WriteCoreError(w, err)
		return
	}
	module.WriteJSON(w, http.StatusOK, map[string]any{"emergency_phone": phone, "leases": outLeases, "tickets": out})
}

func createTenantTicket(w http.ResponseWriter, r *http.Request, d module.Deps, p auth.Principal) {
	var body struct {
		property.TicketRequest
		EntryPreference *string                  `json:"entry_preference"`
		Availability    []property.WindowRequest `json:"availability"`
	}
	if err := module.ReadJSON(r, &body); err != nil {
		module.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	windows, err := property.WindowsFromRequest(body.Availability)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	if body.LeaseID == nil {
		module.WriteError(w, http.StatusUnprocessableEntity, "invalid input: lease is required")
		return
	}
	ctx := r.Context()
	on, err := property.LeaseHasContact(ctx, d.Pool, p.OrgID, *body.LeaseID, *p.ContactID)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	if !on {
		module.WriteError(w, http.StatusNotFound, "not found")
		return
	}
	lease, err := property.GetLease(ctx, d.Pool, p.OrgID, *body.LeaseID)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	var t property.Ticket
	err = module.RunInTx(ctx, d.Pool, func(tx pgx.Tx) error {
		var err error
		t, err = property.CreateTicket(ctx, tx, p.OrgID, property.NewTicket{
			PropertyID: lease.PropertyID, LeaseID: body.LeaseID, ContactID: p.ContactID, UserID: p.UserID,
			Title: body.Title, Description: body.Description, Category: body.Category, Urgency: body.Urgency,
			AccessNotes: body.AccessNotes, PendingFileIDs: body.PendingFileIDs,
			EntryPreference: body.EntryPreference, Availability: windows,
		}, property.TodayIn(d.Cfg.Location))
		return err
	})
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	module.WriteJSON(w, http.StatusCreated, property.TicketToResponse(t))
}

func uploadPending(w http.ResponseWriter, r *http.Request, d module.Deps, p auth.Principal) {
	var pendingID int64
	f, ok := module.HandleUpload(w, r, d, core.MediaPolicy(d.Cfg.UploadMaxBytes), func(tx pgx.Tx, f core.File) error {
		var err error
		pendingID, err = property.SavePendingFile(r.Context(), tx, p.OrgID, p.UserID, f)
		return err
	})
	if !ok {
		return
	}
	module.WriteJSON(w, http.StatusCreated, map[string]any{"pending_id": pendingID, "file": module.FileToResponse(f)})
}

func deletePending(w http.ResponseWriter, r *http.Request, d module.Deps, p auth.Principal) {
	id, ok := pathID(r, "pendingId")
	if !ok {
		module.WriteError(w, http.StatusNotFound, "not found")
		return
	}
	if err := property.DeletePendingFile(r.Context(), d.Pool, p.OrgID, p.UserID, id); err != nil {
		module.WriteCoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func getTenantTicket(w http.ResponseWriter, r *http.Request, d module.Deps, p auth.Principal, t property.Ticket) {
	ctx := r.Context()
	files, err := property.ListTicketFiles(ctx, d.Pool, p.OrgID, t.ID)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	events, err := property.ListTicketEvents(ctx, d.Pool, p.OrgID, t.ID, true)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	resp, err := property.SchedulingDetail(ctx, d.Pool, p.OrgID, t.ID, d.Cfg.Location)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	resp["ticket"], resp["files"], resp["events"] = property.TicketToResponse(t), property.FilesToResponse(files), property.EventsToResponse(events)
	module.WriteJSON(w, http.StatusOK, resp)
}

func commentTenantTicket(w http.ResponseWriter, r *http.Request, d module.Deps, p auth.Principal, t property.Ticket) {
	var body struct {
		Body string `json:"body"`
	}
	if err := module.ReadJSON(r, &body); err != nil {
		module.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	var ev property.TicketEvent
	err := module.RunInTx(r.Context(), d.Pool, func(tx pgx.Tx) error {
		var err error
		ev, err = property.AddTicketEvent(r.Context(), tx, p.OrgID, t.ID, property.TicketEvent{Kind: "comment", Body: body.Body, ActorUserID: &p.UserID})
		return err
	})
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	module.WriteJSON(w, http.StatusCreated, property.EventToResponse(ev))
}

func cancelTenantTicket(w http.ResponseWriter, r *http.Request, d module.Deps, p auth.Principal, t property.Ticket) {
	var body struct {
		Reason string `json:"reason"`
	}
	if err := module.ReadJSON(r, &body); err != nil {
		module.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	var out property.Ticket
	err := module.RunInTx(r.Context(), d.Pool, func(tx pgx.Tx) error {
		var err error
		out, err = property.CancelTicket(r.Context(), tx, p.OrgID, t.ID, p.UserID, body.Reason, true)
		return err
	})
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	module.WriteJSON(w, http.StatusOK, property.TicketToResponse(out))
}

func uploadTenantTicketFile(w http.ResponseWriter, r *http.Request, d module.Deps, p auth.Principal, t property.Ticket) {
	f, ok := module.HandleUpload(w, r, d, core.MediaPolicy(d.Cfg.UploadMaxBytes), func(tx pgx.Tx, f core.File) error {
		return property.AttachTicketFile(r.Context(), tx, p.OrgID, t.ID, f.ID, p.UserID)
	})
	if !ok {
		return
	}
	module.WriteJSON(w, http.StatusCreated, module.FileToResponse(f))
}

func setTenantAvailability(w http.ResponseWriter, r *http.Request, d module.Deps, p auth.Principal, t property.Ticket) {
	var body struct {
		Windows []property.WindowRequest `json:"windows"`
	}
	if err := module.ReadJSON(r, &body); err != nil {
		module.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	windows, err := property.WindowsFromRequest(body.Windows)
	if err == nil {
		err = module.RunInTx(r.Context(), d.Pool, func(tx pgx.Tx) error {
			return property.SetAvailability(r.Context(), tx, p.OrgID, t.ID, p.UserID, windows, property.TodayIn(d.Cfg.Location))
		})
	}
	writeScheduling(w, r, d, p, t, err)
}

func setTenantEntryPreference(w http.ResponseWriter, r *http.Request, d module.Deps, p auth.Principal, t property.Ticket) {
	var body struct {
		EntryPreference *string `json:"entry_preference"`
	}
	if err := module.ReadJSON(r, &body); err != nil {
		module.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	err := module.RunInTx(r.Context(), d.Pool, func(tx pgx.Tx) error {
		return property.SetEntryPreference(r.Context(), tx, p.OrgID, t.ID, body.EntryPreference)
	})
	writeScheduling(w, r, d, p, t, err)
}

// writeScheduling responds with the ticket's scheduling detail after a
// successful change, or the mapped error.
func writeScheduling(w http.ResponseWriter, r *http.Request, d module.Deps, p auth.Principal, t property.Ticket, err error) {
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	resp, err := property.SchedulingDetail(r.Context(), d.Pool, p.OrgID, t.ID, d.Cfg.Location)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	module.WriteJSON(w, http.StatusOK, resp)
}

// tenantVisitAction handles a tenant's confirm, request-change or
// withdraw-entry on a visit of the ticket.
func tenantVisitAction(action string) func(http.ResponseWriter, *http.Request, module.Deps, auth.Principal, property.Ticket) {
	return func(w http.ResponseWriter, r *http.Request, d module.Deps, p auth.Principal, t property.Ticket) {
		visitID, ok := pathID(r, "visitId")
		if !ok {
			module.WriteError(w, http.StatusNotFound, "not found")
			return
		}
		var body struct {
			EntryMode string                   `json:"entry_mode"`
			Note      string                   `json:"note"`
			Windows   []property.WindowRequest `json:"windows"`
		}
		if r.ContentLength != 0 {
			if err := module.ReadJSON(r, &body); err != nil {
				module.WriteError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		ctx := r.Context()
		var v property.Visit
		err := module.RunInTx(ctx, d.Pool, func(tx pgx.Tx) error {
			if err := property.VisitOnTicket(ctx, tx, p.OrgID, t.ID, visitID); err != nil {
				return err
			}
			var err error
			switch action {
			case "confirm":
				v, err = property.ConfirmVisit(ctx, tx, p.OrgID, visitID, p.UserID, body.EntryMode)
			case "request-change":
				var windows []property.AvailabilityWindow
				if body.Windows != nil {
					if windows, err = property.WindowsFromRequest(body.Windows); err != nil {
						return err
					}
				}
				v, err = property.RequestVisitChange(ctx, tx, p.OrgID, visitID, p.UserID, body.Note, windows, property.TodayIn(d.Cfg.Location))
			default:
				v, err = property.WithdrawEntry(ctx, tx, p.OrgID, visitID, p.UserID, time.Now())
			}
			return err
		})
		if err != nil {
			module.WriteCoreError(w, err)
			return
		}
		module.WriteJSON(w, http.StatusOK, property.VisitToResponse(v, d.Cfg.Location))
	}
}
