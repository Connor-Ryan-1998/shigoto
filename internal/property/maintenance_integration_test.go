//go:build integration

package property_test

import (
	"bytes"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"shigoto/internal/auth"
	"shigoto/internal/core"
	"shigoto/internal/property"
	"shigoto/internal/storage"
	"shigoto/internal/testkit"
)

// ticketWorld is an org with a property, an active lease (primary tenant
// plus co-tenant, each a portal user), a staff user and a supplier.
type ticketWorld struct {
	e                *testkit.Env
	org              core.Org
	propertyID       int64
	lease            property.Lease
	tenant, co       int64 // contact ids
	tenantUser       testkit.User
	staff            testkit.User
	supplier, client int64
}

func newTicketWorld(t *testing.T) ticketWorld {
	t.Helper()
	e := testkit.New(t)
	org := e.Org("acme")
	p := e.Property(org).ID
	tenant := e.Contact(org, testkit.Kind("tenant"), testkit.Name("Tina")).ID
	co := e.Contact(org, testkit.Kind("tenant"), testkit.Name("Cody")).ID
	lease := e.Lease(org, p, tenant, testkit.CoTenant(co), testkit.Start(testkit.Date("2026-01-05")))
	staff := e.User("staff@example.com")
	e.Member(staff, org, auth.Staff)
	return ticketWorld{
		e: e, org: org, propertyID: p, lease: lease, tenant: tenant, co: co,
		tenantUser: e.PortalUser("tina@example.com", org, auth.Tenant, tenant),
		staff:      staff,
		supplier:   e.Contact(org, testkit.Kind("supplier"), testkit.Name("Fitzroy Plumbing")).ID,
		client:     e.Contact(org, testkit.Kind("client")).ID,
	}
}

var today = testkit.Date("2026-11-19")

func (w ticketWorld) tx(t *testing.T, f func(tx pgx.Tx) error) error {
	t.Helper()
	tx, err := w.e.Pool.Begin(w.e.Ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(w.e.Ctx) }()
	if err := f(tx); err != nil {
		return err
	}
	return tx.Commit(w.e.Ctx)
}

func (w ticketWorld) newTicket(in property.NewTicket) property.NewTicket {
	if in.PropertyID == 0 {
		in.PropertyID = w.propertyID
	}
	if in.Title == "" {
		in.Title = "Kitchen tap leaking"
	}
	if in.Description == "" {
		in.Description = "Water pooling under the sink."
	}
	if in.Category == "" {
		in.Category = "plumbing"
	}
	if in.Urgency == "" {
		in.Urgency = "urgent"
	}
	return in
}

func (w ticketWorld) raise(t *testing.T, in property.NewTicket) (property.Ticket, error) {
	t.Helper()
	var tk property.Ticket
	err := w.tx(t, func(tx pgx.Tx) error {
		var err error
		tk, err = property.CreateTicket(w.e.Ctx, tx, w.org.ID, w.newTicket(in), today)
		return err
	})
	return tk, err
}

func (w ticketWorld) tenantTicket(t *testing.T) property.Ticket {
	t.Helper()
	tk, err := w.raise(t, property.NewTicket{LeaseID: &w.lease.ID, ContactID: &w.tenant, UserID: w.tenantUser.ID})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	return tk
}

func TestCreateTicketByTenant(t *testing.T) {
	w := newTicketWorld(t)
	tk := w.tenantTicket(t)
	if tk.Ref != "R-0001" || tk.Status != "new" || tk.LeaseID == nil || *tk.LeaseID != w.lease.ID {
		t.Fatalf("ticket = %+v", tk)
	}
	if second := w.tenantTicket(t); second.Ref != "R-0002" {
		t.Fatalf("second ref = %s", second.Ref)
	}
	events, err := property.ListTicketEvents(w.e.Ctx, w.e.Pool, w.org.ID, tk.ID, true)
	if err != nil || len(events) != 1 || events[0].Kind != "created" || !events[0].TenantVisible {
		t.Fatalf("events = %+v err %v", events, err)
	}
	if n := w.e.Count("audit_log", "entity = 'maintenance_ticket' AND entity_id = $1", tk.ID); n != 1 {
		t.Fatalf("audit rows = %d", n)
	}
}

func TestTicketRefsArePerOrg(t *testing.T) {
	w := newTicketWorld(t)
	w.tenantTicket(t)
	beta := w.e.Org("beta")
	p := w.e.Property(beta).ID
	var tk property.Ticket
	if err := w.tx(t, func(tx pgx.Tx) error {
		var err error
		tk, err = property.CreateTicket(w.e.Ctx, tx, beta.ID, w.newTicket(property.NewTicket{PropertyID: p, UserID: w.staff.ID}), today)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if tk.Ref != "R-0001" {
		t.Fatalf("beta ref = %s", tk.Ref)
	}
}

func TestCreateTicketRules(t *testing.T) {
	w := newTicketWorld(t)
	ended := w.e.Lease(w.org, w.e.Property(w.org).ID, w.tenant, testkit.Start(testkit.Date("2025-01-06")))
	if _, err := w.e.Pool.Exec(w.e.Ctx, `UPDATE leases SET termination_date = '2026-11-18' WHERE id = $1`, ended.ID); err != nil {
		t.Fatal(err)
	}
	stranger := w.e.Contact(w.org, testkit.Kind("tenant")).ID
	cases := map[string]struct {
		in   property.NewTicket
		want error
	}{
		"ended lease":             {property.NewTicket{PropertyID: ended.PropertyID, LeaseID: &ended.ID, ContactID: &w.tenant, UserID: w.tenantUser.ID}, core.ErrInvalidInput},
		"not on lease":            {property.NewTicket{LeaseID: &w.lease.ID, ContactID: &stranger, UserID: w.tenantUser.ID}, core.ErrNotFound},
		"tenant without lease":    {property.NewTicket{ContactID: &w.tenant, UserID: w.tenantUser.ID}, core.ErrInvalidInput},
		"lease on other property": {property.NewTicket{PropertyID: ended.PropertyID, LeaseID: &w.lease.ID, UserID: w.staff.ID}, core.ErrInvalidInput},
		"bad category":            {property.NewTicket{Category: "roof", UserID: w.staff.ID}, core.ErrInvalidInput},
		"bad urgency":             {property.NewTicket{Urgency: "asap", UserID: w.staff.ID}, core.ErrInvalidInput},
		"blank title":             {property.NewTicket{Title: "   ", UserID: w.staff.ID}, core.ErrInvalidInput},
	}
	for name, c := range cases {
		if _, err := w.raise(t, c.in); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", name, err, c.want)
		}
	}
	if _, err := w.raise(t, property.NewTicket{UserID: w.staff.ID}); err != nil {
		t.Errorf("staff ticket on a property without a lease: %v", err)
	}
}

func TestMarkViewedConcurrent(t *testing.T) {
	w := newTicketWorld(t)
	tk := w.tenantTicket(t)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = w.tx(t, func(tx pgx.Tx) error {
				_, err := property.MarkViewed(w.e.Ctx, tx, w.org.ID, tk.ID, w.staff.ID)
				return err
			})
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n := w.e.Count("maintenance_events", "ticket_id = $1 AND kind = 'viewed'", tk.ID); n != 1 {
		t.Fatalf("viewed events = %d", n)
	}
}

func TestTicketLifecycle(t *testing.T) {
	w := newTicketWorld(t)
	tk := w.tenantTicket(t)
	ctx, org, staff := w.e.Ctx, w.org.ID, w.staff.ID

	if err := w.tx(t, func(tx pgx.Tx) error {
		_, err := property.AssignSupplier(ctx, tx, org, tk.ID, staff, w.client)
		return err
	}); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("assign client: %v", err)
	}
	var got property.Ticket
	if err := w.tx(t, func(tx pgx.Tx) error {
		var err error
		got, err = property.AssignSupplier(ctx, tx, org, tk.ID, staff, w.supplier)
		return err
	}); err != nil || got.Status != "assigned" || got.SupplierName != "Fitzroy Plumbing" {
		t.Fatalf("assign: %+v %v", got, err)
	}
	// Viewed after assignment is a no-op.
	if err := w.tx(t, func(tx pgx.Tx) error {
		var err error
		got, err = property.MarkViewed(ctx, tx, org, tk.ID, staff)
		return err
	}); err != nil || got.Status != "assigned" {
		t.Fatalf("viewed after assign: %+v", got)
	}
	if err := w.tx(t, func(tx pgx.Tx) error {
		_, err := property.CancelTicket(ctx, tx, org, tk.ID, w.tenantUser.ID, "fixed it", true)
		return err
	}); !errors.Is(err, core.ErrInvalidState) {
		t.Fatalf("tenant cancel after assign: %v", err)
	}
	if err := w.tx(t, func(tx pgx.Tx) error {
		_, err := property.AddTicketEvent(ctx, tx, org, tk.ID, property.TicketEvent{Kind: "note", Body: "SENTINEL-INTERNAL", ActorUserID: &staff})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := w.tx(t, func(tx pgx.Tx) error {
		var err error
		got, err = property.CompleteTicket(ctx, tx, org, tk.ID, staff, "Replaced washer")
		return err
	}); err != nil || got.Status != "completed" || got.CompletedAt == nil {
		t.Fatalf("complete: %+v", got)
	}
	if err := w.tx(t, func(tx pgx.Tx) error {
		_, err := property.AddTicketEvent(ctx, tx, org, tk.ID, property.TicketEvent{Kind: "comment", Body: "thanks", ActorUserID: &w.tenantUser.ID})
		return err
	}); !errors.Is(err, core.ErrInvalidState) {
		t.Fatalf("comment after complete: %v", err)
	}

	tenantView, _ := property.ListTicketEvents(ctx, w.e.Pool, org, tk.ID, true)
	staffView, _ := property.ListTicketEvents(ctx, w.e.Pool, org, tk.ID, false)
	for _, ev := range tenantView {
		if ev.Kind == "note" || ev.Body == "SENTINEL-INTERNAL" {
			t.Fatalf("tenant view leaks note: %+v", ev)
		}
	}
	if len(staffView) != len(tenantView)+1 {
		t.Fatalf("staff %d events, tenant %d", len(staffView), len(tenantView))
	}
}

func TestTenantCancelNewTicket(t *testing.T) {
	w := newTicketWorld(t)
	tk := w.tenantTicket(t)
	var got property.Ticket
	if err := w.tx(t, func(tx pgx.Tx) error {
		var err error
		got, err = property.CancelTicket(w.e.Ctx, tx, w.org.ID, tk.ID, w.tenantUser.ID, "sorted", true)
		return err
	}); err != nil || got.Status != "cancelled" || got.CancelReason != "sorted" {
		t.Fatalf("cancel: %+v %v", got, err)
	}
}

func (w ticketWorld) file(t *testing.T, i int) core.File {
	t.Helper()
	data := append(append([]byte{}, testkit.PNG...), []byte(fmt.Sprint(i))...)
	f, err := core.SaveFile(w.e.Ctx, w.e.Pool, storage.NewDisk(t.TempDir()), w.org.ID, "p.png", bytes.NewReader(data), core.MediaPolicy(0))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestAttachTicketFileCap(t *testing.T) {
	w := newTicketWorld(t)
	tk := w.tenantTicket(t)
	for i := range 20 {
		f := w.file(t, i)
		if err := w.tx(t, func(tx pgx.Tx) error {
			return property.AttachTicketFile(w.e.Ctx, tx, w.org.ID, tk.ID, f.ID, w.tenantUser.ID)
		}); err != nil {
			t.Fatalf("attach %d: %v", i, err)
		}
	}
	f := w.file(t, 99)
	if err := w.tx(t, func(tx pgx.Tx) error {
		return property.AttachTicketFile(w.e.Ctx, tx, w.org.ID, tk.ID, f.ID, w.tenantUser.ID)
	}); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("21st file: %v", err)
	}
	files, err := property.ListTicketFiles(w.e.Ctx, w.e.Pool, w.org.ID, tk.ID)
	if err != nil || len(files) != 20 {
		t.Fatalf("files = %d err %v", len(files), err)
	}
}

func TestCreateTicketAttachesOwnPendingFilesOnly(t *testing.T) {
	w := newTicketWorld(t)
	mine, theirs := w.file(t, 1), w.file(t, 2)
	other := w.e.User("other@example.com")
	var minePending, theirsPending int64
	if err := w.tx(t, func(tx pgx.Tx) error {
		var err error
		if minePending, err = property.SavePendingFile(w.e.Ctx, tx, w.org.ID, w.tenantUser.ID, mine); err != nil {
			return err
		}
		theirsPending, err = property.SavePendingFile(w.e.Ctx, tx, w.org.ID, other.ID, theirs)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.raise(t, property.NewTicket{LeaseID: &w.lease.ID, ContactID: &w.tenant, UserID: w.tenantUser.ID, PendingFileIDs: []int64{minePending, theirsPending}}); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("someone else's pending file: %v", err)
	}
	tk, err := w.raise(t, property.NewTicket{LeaseID: &w.lease.ID, ContactID: &w.tenant, UserID: w.tenantUser.ID, PendingFileIDs: []int64{minePending}})
	if err != nil {
		t.Fatal(err)
	}
	if tk.FileCount != 1 || w.e.Count("maintenance_pending_files", "id = $1", minePending) != 0 {
		t.Fatalf("file_count %d", tk.FileCount)
	}
}

func TestListTicketsFilters(t *testing.T) {
	w := newTicketWorld(t)
	open := w.tenantTicket(t)
	done := w.tenantTicket(t)
	if err := w.tx(t, func(tx pgx.Tx) error {
		_, err := property.CompleteTicket(w.e.Ctx, tx, w.org.ID, done.ID, w.staff.ID, "")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	openOnly, _ := property.ListTickets(w.e.Ctx, w.e.Pool, w.org.ID, property.TicketFilter{PropertyID: &w.propertyID, OpenOnly: true})
	all, _ := property.ListTickets(w.e.Ctx, w.e.Pool, w.org.ID, property.TicketFilter{PropertyID: &w.propertyID})
	byLease, _ := property.ListTickets(w.e.Ctx, w.e.Pool, w.org.ID, property.TicketFilter{LeaseIDs: []int64{w.lease.ID}})
	none, _ := property.ListTickets(w.e.Ctx, w.e.Pool, w.org.ID, property.TicketFilter{LeaseIDs: []int64{}})
	if len(openOnly) != 1 || openOnly[0].ID != open.ID || len(all) != 2 || len(byLease) != 2 || len(none) != 0 {
		t.Fatalf("open %d all %d lease %d none %d", len(openOnly), len(all), len(byLease), len(none))
	}
}

func TestOpenRepairsSummaryOrdering(t *testing.T) {
	w := newTicketWorld(t)
	raise := func(title, urgency, status string) {
		w.e.Ticket(w.org, property.NewTicket{LeaseID: &w.lease.ID, ContactID: &w.tenant, Title: title, Urgency: urgency}, status, "")
	}
	raise("routine new", "routine", "new")
	raise("urgent assigned", "urgent", "assigned")
	raise("soon new", "soon", "new")
	raise("done", "urgent", "completed")
	raise("gone", "urgent", "cancelled")

	s, err := property.OpenRepairs(w.e.Ctx, w.e.Pool, w.org.ID)
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, it := range s.Items {
		titles = append(titles, it.Title)
	}
	if s.OpenCount != 3 || fmt.Sprint(titles) != "[urgent assigned soon new routine new]" {
		t.Fatalf("open %d items %v", s.OpenCount, titles)
	}
	if s.Items[0].PropertyID != w.propertyID || s.Items[0].Ref == "" {
		t.Fatalf("item = %+v", s.Items[0])
	}
}
