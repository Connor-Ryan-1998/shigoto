package property

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"shigoto/internal/auth"
	"shigoto/internal/core"
	"shigoto/internal/module"
)

// VisitResponse is the JSON shape of a visit. Times are RFC 3339 in the
// org's timezone so every viewer sees the org-local arrival window.
type VisitResponse struct {
	ID                int64          `json:"id"`
	Supplier          TicketSupplier `json:"supplier"`
	StartsAt          string         `json:"starts_at"`
	EndsAt            string         `json:"ends_at"`
	Status            string         `json:"status"`
	TenantConfirmedAt *time.Time     `json:"tenant_confirmed_at"`
	ChangeNote        string         `json:"change_note"`
	EntryState        string         `json:"entry_state"`
	EntryGrantedAt    *time.Time     `json:"entry_granted_at"`
	EntryWithdrawnAt  *time.Time     `json:"entry_withdrawn_at"`
}

// VisitToResponse converts v, showing times in loc.
func VisitToResponse(v Visit, loc *time.Location) VisitResponse {
	if loc == nil {
		loc = time.UTC
	}
	return VisitResponse{
		ID: v.ID, Supplier: TicketSupplier{ID: v.SupplierContactID, Name: v.SupplierName},
		StartsAt: v.StartsAt.In(loc).Format(time.RFC3339), EndsAt: v.EndsAt.In(loc).Format(time.RFC3339),
		Status: v.Status, TenantConfirmedAt: v.TenantConfirmedAt, ChangeNote: v.ChangeNote,
		EntryState: v.EntryState(), EntryGrantedAt: v.EntryGrantedAt, EntryWithdrawnAt: v.EntryWithdrawnAt,
	}
}

// WindowRequest is the JSON shape of an availability window.
type WindowRequest struct {
	Date  string  `json:"date"`
	Slot  string  `json:"slot"`
	Start *string `json:"start"`
	End   *string `json:"end"`
}

// WindowsFromRequest parses windows (dates YYYY-MM-DD).
func WindowsFromRequest(in []WindowRequest) ([]AvailabilityWindow, error) {
	out := make([]AvailabilityWindow, 0, len(in))
	for _, w := range in {
		d, err := time.Parse(dateLayout, w.Date)
		if err != nil {
			return nil, invalid("dates must be YYYY-MM-DD")
		}
		out = append(out, AvailabilityWindow{Date: d, Slot: w.Slot, Start: w.Start, End: w.End})
	}
	return out, nil
}

func windowsToResponse(ws []AvailabilityWindow) []WindowRequest {
	out := make([]WindowRequest, 0, len(ws))
	for _, w := range ws {
		out = append(out, WindowRequest{Date: w.Date.Format(dateLayout), Slot: w.Slot, Start: w.Start, End: w.End})
	}
	return out
}

// SchedulingDetail is the scheduling part of a ticket detail response:
// entry_preference, availability, open_visit and visits.
func SchedulingDetail(ctx context.Context, q core.Querier, orgID, ticketID int64, loc *time.Location) (map[string]any, error) {
	pref, err := EntryPreference(ctx, q, orgID, ticketID)
	if err != nil {
		return nil, err
	}
	ws, err := ListAvailability(ctx, q, orgID, ticketID)
	if err != nil {
		return nil, err
	}
	visits, err := ListVisits(ctx, q, orgID, ticketID)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"entry_preference": pref, "availability": windowsToResponse(ws), "open_visit": nil}
	vs := make([]VisitResponse, 0, len(visits))
	for _, v := range visits {
		r := VisitToResponse(v, loc)
		vs = append(vs, r)
		if v.Open() {
			out["open_visit"] = r
		}
	}
	out["visits"] = vs
	return out, nil
}

// WithOpenVisits converts tickets to responses carrying each open visit.
func WithOpenVisits(ctx context.Context, q core.Querier, orgID int64, tickets []Ticket, loc *time.Location) ([]TicketResponse, error) {
	out := make([]TicketResponse, 0, len(tickets))
	for _, t := range tickets {
		r := TicketToResponse(t)
		v, err := OpenVisit(ctx, q, orgID, t.ID)
		if err != nil {
			return nil, err
		}
		if v != nil {
			vr := VisitToResponse(*v, loc)
			r.OpenVisit = &vr
		}
		out = append(out, r)
	}
	return out, nil
}

type visitTimes struct {
	SupplierContactID *int64 `json:"supplier_contact_id"`
	StartsAt          string `json:"starts_at"`
	EndsAt            string `json:"ends_at"`
	Reason            string `json:"reason"`
}

func (b visitTimes) parse() (time.Time, time.Time, error) {
	s, err1 := time.Parse(time.RFC3339, b.StartsAt)
	e, err2 := time.Parse(time.RFC3339, b.EndsAt)
	if err1 != nil || err2 != nil {
		return time.Time{}, time.Time{}, invalid("starts_at and ends_at must be RFC 3339 times")
	}
	return s, e, nil
}

// VisitOnTicket checks visitID belongs to ticketID (else core.ErrNotFound).
func VisitOnTicket(ctx context.Context, q core.Querier, orgID, ticketID, visitID int64) error {
	_, err := GetVisit(ctx, q, orgID, ticketID, visitID)
	return err
}

func registerVisitRoutes(mux *http.ServeMux, d module.Deps) {
	write := func(h func(http.ResponseWriter, *http.Request, module.Deps)) http.Handler {
		return d.Auth.RequireOrg(auth.PermPropertyWrite)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h(w, r, d) }))
	}
	visitAction := func(status int, f func(r *http.Request, tx pgx.Tx, p auth.Principal, ticketID, visitID int64, body visitTimes) (Visit, error)) http.Handler {
		return write(ticketAction(func(r *http.Request, tx pgx.Tx, p auth.Principal, ticketID int64) (any, int, error) {
			var body visitTimes
			if r.ContentLength != 0 {
				if err := module.ReadJSON(r, &body); err != nil {
					return nil, 0, invalid("%v", err)
				}
			}
			var visitID int64
			if r.PathValue("visitId") != "" {
				id, ok := pathInt64(r, "visitId")
				if !ok {
					return nil, 0, core.ErrNotFound
				}
				if err := VisitOnTicket(r.Context(), tx, p.OrgID, ticketID, id); err != nil {
					return nil, 0, err
				}
				visitID = id
			}
			v, err := f(r, tx, p, ticketID, visitID, body)
			return VisitToResponse(v, d.Cfg.Location), status, err
		}))
	}
	mux.Handle("POST /api/v1/orgs/{org}/maintenance/{ticketId}/visits", visitAction(http.StatusCreated, func(r *http.Request, tx pgx.Tx, p auth.Principal, ticketID, id int64, b visitTimes) (Visit, error) {
		s, e, err := b.parse()
		if err != nil {
			return Visit{}, err
		}
		return BookVisit(r.Context(), tx, p.OrgID, ticketID, p.UserID, b.SupplierContactID, s, e)
	}))
	mux.Handle("PUT /api/v1/orgs/{org}/maintenance/{ticketId}/visits/{visitId}", visitAction(http.StatusOK, func(r *http.Request, tx pgx.Tx, p auth.Principal, ticketID, id int64, b visitTimes) (Visit, error) {
		s, e, err := b.parse()
		if err != nil {
			return Visit{}, err
		}
		return RescheduleVisit(r.Context(), tx, p.OrgID, id, p.UserID, s, e)
	}))
	mux.Handle("POST /api/v1/orgs/{org}/maintenance/{ticketId}/visits/{visitId}/cancel", visitAction(http.StatusOK, func(r *http.Request, tx pgx.Tx, p auth.Principal, ticketID, id int64, b visitTimes) (Visit, error) {
		return CancelVisit(r.Context(), tx, p.OrgID, id, p.UserID, b.Reason)
	}))
	mux.Handle("POST /api/v1/orgs/{org}/maintenance/{ticketId}/visits/{visitId}/done", visitAction(http.StatusOK, func(r *http.Request, tx pgx.Tx, p auth.Principal, ticketID, id int64, b visitTimes) (Visit, error) {
		return MarkVisitDone(r.Context(), tx, p.OrgID, id, p.UserID)
	}))
}
