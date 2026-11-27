package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"shigoto/internal/core"
	"shigoto/internal/quotes"
	"shigoto/internal/storage"
)

// seedQuotes gives the main sandbox client quotes in every state: a draft,
// a sent quote, an expired one, one accepted into a fixed-price project,
// and a declined quote whose revision was sent (superseding it). The sent
// quote has an internal scope-of-work attachment. Every returned key
// (frozen PDFs and the attachment) is already stored and is the caller's to
// delete if the transaction fails.
func seedQuotes(ctx context.Context, tx pgx.Tx, store storage.Store, orgs []core.Org, today time.Time) ([]storage.Key, error) {
	var keys []storage.Key
	org := orgs[0]
	var contactID, ownerID int64
	if err := tx.QueryRow(ctx, `SELECT m.contact_id FROM memberships m JOIN users u ON u.id = m.user_id
		WHERE m.org_id = $1 AND u.email = 'client@example.test'`, org.ID).Scan(&contactID); err != nil {
		return keys, fmt.Errorf("quote client: %w", err)
	}
	if err := tx.QueryRow(ctx, `SELECT user_id FROM memberships WHERE org_id = $1 AND role = 'owner' ORDER BY user_id LIMIT 1`,
		org.ID).Scan(&ownerID); err != nil {
		return keys, err
	}
	settings, err := core.GetSettings(ctx, tx, org.ID)
	if err != nil {
		return keys, err
	}
	gst := "FRE"
	if settings.GSTRegistered {
		gst = "GST"
	}
	lines := []quotes.NewLine{
		{Kind: quotes.KindLabour, Description: "Site assessment", QuantityMilli: 2000, UnitAmount: 9500, GSTCode: gst},
		{Kind: quotes.KindLabour, Description: "Installation", QuantityMilli: 6500, UnitAmount: 9500, GSTCode: gst},
		{Kind: quotes.KindMaterial, Description: "Cable and fittings (fictional)", QuantityMilli: 1000, UnitAmount: 42000, GSTCode: gst},
	}
	create := func(title string, issued time.Time) (quotes.Quote, error) {
		return quotes.CreateDraft(ctx, tx, org.ID, quotes.Draft{
			ContactID: contactID, Title: title, Terms: "Fictional terms: 50% deposit on acceptance, balance on completion.",
			IssueDate: issued, ValidUntil: issued.AddDate(0, 0, 30), Lines: lines,
		})
	}
	send := func(q quotes.Quote, at time.Time) (quotes.Quote, error) {
		sent, err := quotes.MarkSent(ctx, tx, store, org.ID, q.ID, at)
		if err == nil {
			key, kerr := fileKey(ctx, tx, org.ID, *sent.PDFFileID)
			if kerr != nil {
				return sent, kerr
			}
			keys = append(keys, key)
		}
		return sent, err
	}
	at := func(days int) time.Time { return today.AddDate(0, 0, days).Add(9 * time.Hour) }

	if _, err := create("Switchboard upgrade (draft)", today); err != nil {
		return keys, err
	}
	open, err := create("Kitchen power points", today.AddDate(0, 0, -3))
	if err != nil {
		return keys, err
	}
	if open, err = send(open, at(-3)); err != nil {
		return keys, err
	}
	scope, err := demoPDF(org.Name, "Scope of work: Kitchen power points", today)
	if err != nil {
		return keys, err
	}
	staged, err := core.StageFile(ctx, store, org.ID, "scope-of-work.pdf", bytes.NewReader(scope), core.DocumentPolicy)
	if err != nil {
		return keys, err
	}
	scopeFile, err := staged.Save(ctx, tx)
	if err != nil {
		_ = staged.Abort()
		return keys, err
	}
	if err := staged.Commit(ctx); err != nil {
		return keys, err
	}
	keys = append(keys, scopeFile.Key())
	if _, err := quotes.AddAttachment(ctx, tx, org.ID, open.ID, ownerID, scopeFile); err != nil {
		return keys, err
	}

	expired, err := create("Garage lighting", today.AddDate(0, 0, -45))
	if err != nil {
		return keys, err
	}
	if _, err := send(expired, at(-45)); err != nil {
		return keys, err
	}

	accepted, err := create("Bathroom exhaust fan", today.AddDate(0, 0, -20))
	if err != nil {
		return keys, err
	}
	if _, err := send(accepted, at(-20)); err != nil {
		return keys, err
	}
	if _, _, err := quotes.Accept(ctx, tx, org.ID, ownerID, accepted.ID, at(-15)); err != nil {
		return keys, err
	}

	declined, err := create("Solar inverter", today.AddDate(0, 0, -10))
	if err != nil {
		return keys, err
	}
	if _, err := send(declined, at(-10)); err != nil {
		return keys, err
	}
	if _, err := quotes.Decline(ctx, tx, org.ID, ownerID, declined.ID, at(-8)); err != nil {
		return keys, err
	}
	revision, err := quotes.Revise(ctx, tx, org.ID, declined.ID, today.AddDate(0, 0, -7))
	if err != nil {
		return keys, err
	}
	if _, err := send(revision, at(-7)); err != nil {
		return keys, err
	}
	return keys, nil
}

func fileKey(ctx context.Context, tx pgx.Tx, orgID, fileID int64) (storage.Key, error) {
	f, err := core.GetFile(ctx, tx, orgID, fileID)
	return f.Key(), err
}
