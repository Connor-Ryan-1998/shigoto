package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/signintech/gopdf"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"

	"shigoto/internal/clients"
	"shigoto/internal/core"
	"shigoto/internal/storage"
)

// sandboxDoc is one fictional client document. Project names a seeded
// project ("" = general); Status is sent, signed, withdrawn or draft.
type sandboxDoc struct {
	title, kind, category, project, status string
}

// clientPortalSeeds are the documents each sandbox client receives. The
// main client covers every state; the beta client has one to sign, for
// cross-organisation isolation checks. client-empty gets nothing.
var clientPortalSeeds = []struct {
	email string
	org   int
	full  bool
	docs  []sandboxDoc
}{
	{"client@example.test", 0, true, []sandboxDoc{
		{"Master Services Agreement", clients.KindSignature, clients.CategoryGeneral, "", clients.StatusSigned},
		{"Statement of Work", clients.KindSignature, clients.CategoryGeneral, "Reporting setup", clients.StatusSent},
		{"Discovery workshop notes", clients.KindShared, clients.CategoryDiscovery, "Reporting setup", clients.StatusSent},
		{"Change request 1", clients.KindSignature, clients.CategoryGeneral, "Reporting setup", clients.StatusDraft},
		{"Website refresh brief", clients.KindShared, clients.CategoryGeneral, "Website refresh", clients.StatusSent},
		{"Mutual NDA (old)", clients.KindSignature, clients.CategoryGeneral, "", clients.StatusWithdrawn},
	}},
	{"client-beta@example.test", 1, false, []sandboxDoc{
		{"Statement of Work", clients.KindSignature, clients.CategoryGeneral, "", clients.StatusSent},
	}},
}

// seedClientPortal gives the sandbox clients welcome notes, a kickoff
// checklist, client-facing project details and documents in every state,
// including one already signed (with a real signing certificate). Document
// files are committed to store immediately, because signing re-reads them.
// Every returned key (documents and signing certificates) is already
// stored and is the caller's to delete if the transaction fails.
func seedClientPortal(ctx context.Context, tx pgx.Tx, store storage.Store, orgs []core.Org, today time.Time, location *time.Location) ([]storage.Key, error) {
	var stages []storage.Key
	for _, seed := range clientPortalSeeds {
		org := orgs[seed.org]
		var contactID, userID, ownerID int64
		if err := tx.QueryRow(ctx, `SELECT m.contact_id, u.id FROM memberships m JOIN users u ON u.id = m.user_id
			WHERE m.org_id = $1 AND u.email = $2`, org.ID, seed.email).Scan(&contactID, &userID); err != nil {
			return stages, fmt.Errorf("%s: %w", seed.email, err)
		}
		if err := tx.QueryRow(ctx, `SELECT user_id FROM memberships WHERE org_id = $1 AND role = 'owner' ORDER BY user_id LIMIT 1`,
			org.ID).Scan(&ownerID); err != nil {
			return stages, err
		}
		if seed.full {
			if err := seedClientHub(ctx, tx, org.ID, contactID, ownerID, today); err != nil {
				return stages, err
			}
		}
		for _, d := range seed.docs {
			keys, err := seedClientDocument(ctx, tx, store, org, contactID, ownerID, userID, seed.email, d, today, location)
			stages = append(stages, keys...)
			if err != nil {
				return stages, fmt.Errorf("%s %q: %w", seed.email, d.title, err)
			}
		}
	}
	return stages, nil
}

func seedClientHub(ctx context.Context, tx pgx.Tx, orgID, contactID, ownerID int64, today time.Time) error {
	if _, err := clients.PutWelcome(ctx, tx, orgID, contactID, ownerID,
		"Slack #sandbox-client for day-to-day questions: replies within 4 business hours.\n"+
			"Email for contracts and approvals: replies within 1 business day.\n"+
			"Phone only for outages (fictional number, do not call)."); err != nil {
		return err
	}
	for i, it := range []struct {
		action, owner string
		days          int
		done          bool
	}{
		{"Grant admin access to the analytics account", "Sam (client IT)", -3, true},
		{"Send brand guidelines PDF", "Priya (client marketing)", -1, true},
		{"Add the studio to the client Slack workspace", "Sam (client IT)", 2, false},
		{"Share DNS registrar login or delegate access", "Client IT", 4, false},
		{"Confirm the stakeholder list", "Priya (client marketing)", 5, false},
	} {
		due := today.AddDate(0, 0, it.days)
		created, err := clients.CreateChecklistItem(ctx, tx, orgID, contactID, ownerID, clients.ChecklistItem{
			Action: it.action, Owner: it.owner, DueDate: &due})
		if err != nil {
			return fmt.Errorf("checklist item %d: %w", i, err)
		}
		if it.done {
			if _, err := clients.SetChecklistDone(ctx, tx, orgID, contactID, ownerID, created.ID, true); err != nil {
				return err
			}
		}
	}
	_, err := tx.Exec(ctx, `UPDATE projects SET
		summary = 'Set up monthly reporting from the client''s sales and support systems, with a shared dashboard.',
		goals = E'Dashboard live before the next monthly executive review\nData refreshed nightly without manual steps\nClient staff can add charts themselves',
		stakeholders = E'Priya Nair — Sponsor (client)\nSam Lee — IT lead (client)\nSandbox Staff — Project lead (studio)'
		WHERE org_id = $1 AND client_contact_id = $2 AND name = 'Reporting setup'`, orgID, contactID)
	return err
}

func seedClientDocument(ctx context.Context, tx pgx.Tx, store storage.Store, org core.Org, contactID, ownerID, clientUserID int64,
	clientEmail string, d sandboxDoc, today time.Time, location *time.Location) ([]storage.Key, error) {
	var projectID *int64
	if d.project != "" {
		var id int64
		if err := tx.QueryRow(ctx, `SELECT id FROM projects WHERE org_id = $1 AND client_contact_id = $2 AND name = $3`,
			org.ID, contactID, d.project).Scan(&id); err != nil {
			return nil, fmt.Errorf("project %q: %w", d.project, err)
		}
		projectID = &id
	}
	data, err := demoPDF(org.Name, d.title, today)
	if err != nil {
		return nil, err
	}
	staged, err := core.StageFile(ctx, store, org.ID, d.title+".pdf", bytes.NewReader(data), clients.PDFPolicy)
	if err != nil {
		return nil, err
	}
	file, err := staged.Save(ctx, tx)
	if err != nil {
		_ = staged.Abort()
		return nil, err
	}
	// Committed now: signing re-reads and hashes the stored bytes.
	if err := staged.Commit(ctx); err != nil {
		return nil, err
	}
	keys := []storage.Key{file.Key()}
	doc, err := clients.CreateDocument(ctx, tx, org.ID, ownerID, clients.NewDocument{
		ContactID: contactID, ProjectID: projectID, Title: d.title, Kind: d.kind, Category: d.category}, file)
	if err != nil || d.status == clients.StatusDraft {
		return keys, err
	}
	if _, err := clients.SendDocument(ctx, tx, org.ID, ownerID, doc.ID); err != nil {
		return keys, err
	}
	switch d.status {
	case clients.StatusWithdrawn:
		_, err = clients.WithdrawDocument(ctx, tx, org.ID, ownerID, doc.ID)
	case clients.StatusSigned:
		var signed clients.Document
		signed, err = clients.SignTx(ctx, tx, store, location, org.ID, contactID, doc.ID, clients.SignInput{
			UserID: clientUserID, Email: clientEmail, TypedName: "Priya Nair",
			IP: "192.0.2.10", UserAgent: "Sandbox seed", Consent: true})
		if err == nil {
			var cert core.File
			if cert, err = core.GetFile(ctx, tx, org.ID, signed.Signature.CertificateFileID); err == nil {
				keys = append(keys, cert.Key())
			}
		}
	}
	return keys, err
}

// demoPDF renders a one-page fictional document so the sandbox needs no
// bundled or downloaded files.
func demoPDF(orgName, title string, today time.Time) ([]byte, error) {
	pdf := &gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4})
	if err := pdf.AddTTFFontData("go", goregular.TTF); err != nil {
		return nil, err
	}
	if err := pdf.AddTTFFontData("go-bold", gobold.TTF); err != nil {
		return nil, err
	}
	pdf.AddPage()
	lines := []struct {
		font string
		size float64
		text string
	}{
		{"go-bold", 18, title},
		{"go", 11, orgName + " · sandbox document · " + today.Format("2 Jan 2006")},
		{"go", 11, ""},
		{"go", 11, "This is a fictional document generated by populate-sbx for local testing."},
		{"go", 11, "It is not a real agreement and has no legal effect."},
		{"go", 11, "The studio has signed it on its side; the client signs it in the portal."},
	}
	y := 60.0
	for _, l := range lines {
		if err := pdf.SetFont(l.font, "", l.size); err != nil {
			return nil, err
		}
		pdf.SetXY(50, y)
		if err := pdf.Cell(nil, l.text); err != nil {
			return nil, err
		}
		y += l.size + 10
	}
	var buf bytes.Buffer
	if err := pdf.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
