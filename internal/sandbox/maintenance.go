package sandbox

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"shigoto/internal/core"
	"shigoto/internal/property"
)

func seedMaintenance(ctx context.Context, tx pgx.Tx, orgID int64, today time.Time, location *time.Location, propertyID, tenantID, tenantUserID, staffID int64, full bool) error {
	var leaseID int64
	if err := tx.QueryRow(ctx, `SELECT id FROM leases WHERE org_id=$1 AND property_id=$2 AND termination_date IS NULL`, orgID, propertyID).Scan(&leaseID); err != nil {
		return err
	}
	supplier, err := core.CreateContact(ctx, tx, orgID, core.Contact{
		Kind: "supplier", Name: "Sandbox Repairs", Email: "repairs@example.test", Phone: "02 5550 0101",
	})
	if err != nil {
		return err
	}
	definitions := []struct {
		title, description, category, urgency, state string
	}{
		{"Kitchen tap leaking", "Fictional demo: the tap drips into the sink. See the sample photo, video and voice note.", "plumbing", "urgent", "new"},
		{"Window latch sticking", "The bedroom latch is difficult to close.", "doors_windows_locks", "routine", "viewed"},
		{"Oven not heating evenly", "The oven fan runs but food is not heating evenly.", "appliance", "soon", "assigned"},
		{"Laundry tap washer", "The laundry tap needs a replacement washer.", "plumbing", "routine", "booked"},
		{"Front door adjustment", "The front door catches against the frame.", "doors_windows_locks", "routine", "tenant_home"},
		{"Air conditioner service", "The air conditioner needs a routine service.", "heating_cooling", "routine", "permitted"},
		{"Bathroom fan rattling", "The exhaust fan rattles. Entry permission was withdrawn for this visit.", "electrical", "soon", "withdrawn"},
		{"Garden gate repair", "The tenant has requested a different arrival window.", "garden_exterior", "routine", "change_requested"},
		{"Smoke alarm replaced", "The faulty smoke alarm has been replaced and tested.", "electrical", "urgent", "completed"},
		{"Drain inspection cancelled", "The reported blockage cleared before a visit was needed.", "plumbing", "routine", "cancelled"},
	}
	if !full {
		definitions = definitions[:1]
	}
	for i, def := range definitions {
		preference := "tenant_home"
		if def.state == "permitted" || def.state == "withdrawn" || def.state == "booked" {
			preference = "permitted"
		}
		ticket, err := property.CreateTicket(ctx, tx, orgID, property.NewTicket{
			PropertyID: propertyID, LeaseID: &leaseID, ContactID: &tenantID, UserID: tenantUserID,
			Title: def.title, Description: def.description, Category: def.category, Urgency: def.urgency,
			AccessNotes:     "Fictional demo: ring the doorbell; no real keys or access codes.",
			EntryPreference: &preference,
			Availability: []property.AvailabilityWindow{
				{Date: today.AddDate(0, 0, i+2), Slot: "morning"},
				{Date: today.AddDate(0, 0, i+3), Slot: "afternoon"},
			},
		}, today)
		if err != nil {
			return err
		}
		if def.state == "new" {
			continue
		}
		if _, err := property.MarkViewed(ctx, tx, orgID, ticket.ID, staffID); err != nil {
			return err
		}
		for _, ev := range []property.TicketEvent{
			{Kind: "comment", Body: "We have reviewed your request and will keep you updated here.", ActorUserID: &staffID},
			{Kind: "note", Body: "Sandbox internal note: check the fictional supplier quote before approving work.", ActorUserID: &staffID},
			{Kind: "comment", Body: "Thank you. The suggested morning window suits me.", ActorUserID: &tenantUserID},
		} {
			if _, err := property.AddTicketEvent(ctx, tx, orgID, ticket.ID, ev); err != nil {
				return err
			}
		}
		if def.state == "viewed" {
			continue
		}
		if def.state == "cancelled" {
			if _, err := property.CancelTicket(ctx, tx, orgID, ticket.ID, tenantUserID, "The blockage cleared; no visit is needed.", true); err != nil {
				return err
			}
			continue
		}
		if _, err := property.AssignSupplier(ctx, tx, orgID, ticket.ID, staffID, supplier.ID); err != nil {
			return err
		}
		if def.state == "assigned" {
			continue
		}
		day := today.AddDate(0, 0, i+2)
		if def.state == "completed" {
			day = today.AddDate(0, 0, -2)
		}
		start := time.Date(day.Year(), day.Month(), day.Day(), 9, 0, 0, 0, location)
		visit, err := property.BookVisit(ctx, tx, orgID, ticket.ID, staffID, nil, start, start.Add(2*time.Hour))
		if err != nil {
			return err
		}
		switch def.state {
		case "tenant_home", "permitted", "withdrawn":
			mode := "tenant_home"
			if def.state != "tenant_home" {
				mode = "permitted"
			}
			if _, err := property.ConfirmVisit(ctx, tx, orgID, visit.ID, tenantUserID, mode); err != nil {
				return err
			}
			if def.state == "withdrawn" {
				if _, err := property.WithdrawEntry(ctx, tx, orgID, visit.ID, tenantUserID, today); err != nil {
					return err
				}
			}
		case "change_requested":
			if _, err := property.ConfirmVisit(ctx, tx, orgID, visit.ID, tenantUserID, "permitted"); err != nil {
				return err
			}
			if _, err := property.RequestVisitChange(ctx, tx, orgID, visit.ID, tenantUserID, "I will be working; could we use the next afternoon instead?", []property.AvailabilityWindow{
				{Date: day.AddDate(0, 0, 1), Slot: "afternoon"},
			}, today); err != nil {
				return err
			}
		case "completed":
			if _, err := property.MarkVisitDone(ctx, tx, orgID, visit.ID, staffID); err != nil {
				return err
			}
			if _, err := property.CompleteTicket(ctx, tx, orgID, ticket.ID, staffID, "Replaced and tested successfully. No further work required."); err != nil {
				return err
			}
		}
	}
	return nil
}
