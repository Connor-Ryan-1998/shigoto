//go:build integration

package property_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"shigoto/internal/core"
	"shigoto/internal/property"
)

var (
	visitStart = time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC)
	visitEnd   = visitStart.Add(2 * time.Hour)
)

func strp(s string) *string { return &s }

func (w ticketWorld) assigned(t *testing.T) property.Ticket {
	t.Helper()
	tk := w.tenantTicket(t)
	if err := w.tx(t, func(tx pgx.Tx) error {
		_, err := property.AssignSupplier(w.e.Ctx, tx, w.org.ID, tk.ID, w.staff.ID, w.supplier)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return tk
}

func (w ticketWorld) book(t *testing.T, ticketID int64) property.Visit {
	t.Helper()
	var v property.Visit
	if err := w.tx(t, func(tx pgx.Tx) error {
		var err error
		v, err = property.BookVisit(w.e.Ctx, tx, w.org.ID, ticketID, w.staff.ID, nil, visitStart, visitEnd)
		return err
	}); err != nil {
		t.Fatalf("BookVisit: %v", err)
	}
	return v
}

func (w ticketWorld) do(t *testing.T, f func(tx pgx.Tx) (property.Visit, error)) (property.Visit, error) {
	t.Helper()
	var v property.Visit
	err := w.tx(t, func(tx pgx.Tx) error {
		var err error
		v, err = f(tx)
		return err
	})
	return v, err
}

func TestBookVisitSchedulesTicket(t *testing.T) {
	w := newTicketWorld(t)
	tk := w.assigned(t)
	v := w.book(t, tk.ID)
	got, _ := property.GetTicket(w.e.Ctx, w.e.Pool, w.org.ID, tk.ID)
	if v.Status != "booked" || v.SupplierContactID != w.supplier || got.Status != "scheduled" || v.EntryState() != "unconfirmed" {
		t.Fatalf("visit %+v ticket %s", v, got.Status)
	}
}

func TestBookVisitRequiresSupplier(t *testing.T) {
	w := newTicketWorld(t)
	tk := w.tenantTicket(t)
	if _, err := w.do(t, func(tx pgx.Tx) (property.Visit, error) {
		return property.BookVisit(w.e.Ctx, tx, w.org.ID, tk.ID, w.staff.ID, nil, visitStart, visitEnd)
	}); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("err = %v", err)
	}
	if _, err := w.do(t, func(tx pgx.Tx) (property.Visit, error) {
		return property.BookVisit(w.e.Ctx, tx, w.org.ID, tk.ID, w.staff.ID, &w.supplier, visitEnd, visitStart)
	}); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("end before start: %v", err)
	}
}

func TestOneOpenVisit(t *testing.T) {
	w := newTicketWorld(t)
	tk := w.assigned(t)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = w.do(t, func(tx pgx.Tx) (property.Visit, error) {
				return property.BookVisit(w.e.Ctx, tx, w.org.ID, tk.ID, w.staff.ID, nil, visitStart, visitEnd)
			})
		}(i)
	}
	wg.Wait()
	ok, conflict := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, core.ErrInvalidState):
			conflict++
		default:
			t.Fatalf("unexpected: %v", err)
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatalf("ok %d conflict %d", ok, conflict)
	}
}

func TestConfirmWithEntryAndPreference(t *testing.T) {
	w := newTicketWorld(t)
	tk := w.assigned(t)
	if err := w.tx(t, func(tx pgx.Tx) error {
		return property.SetEntryPreference(w.e.Ctx, tx, w.org.ID, tk.ID, strp("permitted"))
	}); err != nil {
		t.Fatal(err)
	}
	v := w.book(t, tk.ID)
	if v.EntryState() != "unconfirmed" {
		t.Fatalf("preference treated as permission: %s", v.EntryState())
	}
	v, err := w.do(t, func(tx pgx.Tx) (property.Visit, error) {
		return property.ConfirmVisit(w.e.Ctx, tx, w.org.ID, v.ID, w.tenantUser.ID, "permitted")
	})
	if err != nil || v.Status != "confirmed" || v.EntryState() != "permitted" || v.EntryGrantedAt == nil || v.TenantConfirmedAt == nil {
		t.Fatalf("confirm: %+v %v", v, err)
	}
	if _, err := w.do(t, func(tx pgx.Tx) (property.Visit, error) {
		return property.ConfirmVisit(w.e.Ctx, tx, w.org.ID, v.ID, w.tenantUser.ID, "sure")
	}); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("bad mode: %v", err)
	}
}

func TestRescheduleResetsEntry(t *testing.T) {
	w := newTicketWorld(t)
	tk := w.assigned(t)
	v := w.book(t, tk.ID)
	if _, err := w.do(t, func(tx pgx.Tx) (property.Visit, error) {
		return property.ConfirmVisit(w.e.Ctx, tx, w.org.ID, v.ID, w.tenantUser.ID, "permitted")
	}); err != nil {
		t.Fatal(err)
	}
	v, err := w.do(t, func(tx pgx.Tx) (property.Visit, error) {
		return property.RescheduleVisit(w.e.Ctx, tx, w.org.ID, v.ID, w.staff.ID, visitStart.Add(24*time.Hour), visitEnd.Add(24*time.Hour))
	})
	if err != nil || v.Status != "booked" || v.EntryState() != "unconfirmed" || v.EntryMode != nil || v.TenantConfirmedAt != nil || v.EntryGrantedAt != nil {
		t.Fatalf("reschedule: %+v %v", v, err)
	}
}

func TestWithdrawEntry(t *testing.T) {
	w := newTicketWorld(t)
	tk := w.assigned(t)
	v := w.book(t, tk.ID)
	if _, err := w.do(t, func(tx pgx.Tx) (property.Visit, error) {
		return property.ConfirmVisit(w.e.Ctx, tx, w.org.ID, v.ID, w.tenantUser.ID, "permitted")
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.do(t, func(tx pgx.Tx) (property.Visit, error) {
		return property.WithdrawEntry(w.e.Ctx, tx, w.org.ID, v.ID, w.tenantUser.ID, visitStart.Add(5*time.Minute))
	}); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("after start: %v", err)
	}
	got, err := w.do(t, func(tx pgx.Tx) (property.Visit, error) {
		return property.WithdrawEntry(w.e.Ctx, tx, w.org.ID, v.ID, w.tenantUser.ID, visitStart.Add(-time.Hour))
	})
	if err != nil || got.EntryState() != "withdrawn" {
		t.Fatalf("withdraw: %+v %v", got, err)
	}
}

func TestCancelVisitReturnsAssigned(t *testing.T) {
	w := newTicketWorld(t)
	tk := w.assigned(t)
	v := w.book(t, tk.ID)
	if _, err := w.do(t, func(tx pgx.Tx) (property.Visit, error) {
		return property.CancelVisit(w.e.Ctx, tx, w.org.ID, v.ID, w.staff.ID, "plumber sick")
	}); err != nil {
		t.Fatal(err)
	}
	got, _ := property.GetTicket(w.e.Ctx, w.e.Pool, w.org.ID, tk.ID)
	open, _ := property.OpenVisit(w.e.Ctx, w.e.Pool, w.org.ID, tk.ID)
	if got.Status != "assigned" || open != nil {
		t.Fatalf("ticket %s open %+v", got.Status, open)
	}
	// A new visit can be booked once the old one is cancelled.
	w.book(t, tk.ID)
}

func TestRequestChangeNoteOnly(t *testing.T) {
	w := newTicketWorld(t)
	tk := w.assigned(t)
	v := w.book(t, tk.ID)
	if _, err := w.do(t, func(tx pgx.Tx) (property.Visit, error) {
		return property.RequestVisitChange(w.e.Ctx, tx, w.org.ID, v.ID, w.tenantUser.ID, "  ", nil, today)
	}); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("blank note: %v", err)
	}
	got, err := w.do(t, func(tx pgx.Tx) (property.Visit, error) {
		return property.RequestVisitChange(w.e.Ctx, tx, w.org.ID, v.ID, w.tenantUser.ID, "Working that day", nil, today)
	})
	if err != nil || got.Status != "change_requested" || got.ChangeNote != "Working that day" {
		t.Fatalf("change: %+v %v", got, err)
	}
}

func TestAvailabilityRules(t *testing.T) {
	w := newTicketWorld(t)
	tk := w.assigned(t)
	set := func(ws []property.AvailabilityWindow) error {
		return w.tx(t, func(tx pgx.Tx) error {
			return property.SetAvailability(w.e.Ctx, tx, w.org.ID, tk.ID, w.tenantUser.ID, ws, today)
		})
	}
	tomorrow := today.AddDate(0, 0, 1)
	if err := set([]property.AvailabilityWindow{{Date: today.AddDate(0, 0, -1), Slot: "morning"}}); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("past date: %v", err)
	}
	if err := set([]property.AvailabilityWindow{{Date: tomorrow, Slot: "custom", Start: strp("15:00"), End: strp("14:00")}}); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("custom backwards: %v", err)
	}
	var eleven []property.AvailabilityWindow
	for range 11 {
		eleven = append(eleven, property.AvailabilityWindow{Date: tomorrow, Slot: "afternoon"})
	}
	if err := set(eleven); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("11 windows: %v", err)
	}
	if err := set([]property.AvailabilityWindow{{Date: tomorrow, Slot: "morning"}, {Date: tomorrow, Slot: "custom", Start: strp("15:00"), End: strp("17:30")}}); err != nil {
		t.Fatal(err)
	}
	ws, _ := property.ListAvailability(w.e.Ctx, w.e.Pool, w.org.ID, tk.ID)
	if len(ws) != 2 || ws[1].Start == nil || *ws[1].Start != "15:00" {
		t.Fatalf("availability = %+v", ws)
	}
	v := w.book(t, tk.ID)
	if _, err := w.do(t, func(tx pgx.Tx) (property.Visit, error) {
		return property.ConfirmVisit(w.e.Ctx, tx, w.org.ID, v.ID, w.tenantUser.ID, "tenant_home")
	}); err != nil {
		t.Fatal(err)
	}
	if err := set([]property.AvailabilityWindow{{Date: tomorrow, Slot: "morning"}}); !errors.Is(err, core.ErrInvalidState) {
		t.Fatalf("after confirm: %v", err)
	}
}

func TestCompletingTicketCancelsOpenVisit(t *testing.T) {
	w := newTicketWorld(t)
	tk := w.assigned(t)
	v := w.book(t, tk.ID)
	if err := w.tx(t, func(tx pgx.Tx) error {
		_, err := property.CompleteTicket(w.e.Ctx, tx, w.org.ID, tk.ID, w.staff.ID, "")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	visits, _ := property.ListVisits(w.e.Ctx, w.e.Pool, w.org.ID, tk.ID)
	if len(visits) != 1 || visits[0].ID != v.ID || visits[0].Status != "cancelled" {
		t.Fatalf("visits = %+v", visits)
	}
	if n := w.e.Count("maintenance_events", "ticket_id = $1 AND kind = 'visit_cancelled'", tk.ID); n != 1 {
		t.Fatalf("visit_cancelled events = %d", n)
	}
	if _, err := w.do(t, func(tx pgx.Tx) (property.Visit, error) {
		return property.BookVisit(w.e.Ctx, tx, w.org.ID, tk.ID, w.staff.ID, nil, visitStart, visitEnd)
	}); !errors.Is(err, core.ErrInvalidState) {
		t.Fatalf("book on closed ticket: %v", err)
	}
}

func TestCreateTicketWithPreferenceAndAvailability(t *testing.T) {
	w := newTicketWorld(t)
	tk, err := w.raise(t, property.NewTicket{
		LeaseID: &w.lease.ID, ContactID: &w.tenant, UserID: w.tenantUser.ID,
		EntryPreference: strp("permitted"),
		Availability:    []property.AvailabilityWindow{{Date: today.AddDate(0, 0, 2), Slot: "morning"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	pref, _ := property.EntryPreference(w.e.Ctx, w.e.Pool, w.org.ID, tk.ID)
	ws, _ := property.ListAvailability(w.e.Ctx, w.e.Pool, w.org.ID, tk.ID)
	if pref == nil || *pref != "permitted" || len(ws) != 1 {
		t.Fatalf("pref %v windows %+v", pref, ws)
	}
	if _, err := w.raise(t, property.NewTicket{LeaseID: &w.lease.ID, ContactID: &w.tenant, UserID: w.tenantUser.ID,
		Availability: []property.AvailabilityWindow{{Date: today.AddDate(0, 0, -1), Slot: "morning"}}}); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("past window: %v", err)
	}
}
