package quotes

import (
	"bytes"
	"context"
	"io"
	"time"

	"github.com/jackc/pgx/v5"

	"shigoto/internal/core"
	"shigoto/internal/storage"
	"shigoto/internal/timesheets"
)

// pdfPolicy stores frozen quote PDFs.
var pdfPolicy = core.UploadPolicy{MaxBytes: 20 << 20, Allowed: map[string]bool{"application/pdf": true}}

// render builds qt's PDF from the current org profile, logo, customer and
// project. qt must carry its lines.
func render(ctx context.Context, q core.Querier, store storage.Store, orgID int64, qt Quote) ([]byte, error) {
	settings, err := core.GetSettings(ctx, q, orgID)
	if err != nil {
		return nil, err
	}
	customer, err := core.GetContact(ctx, q, orgID, qt.ContactID)
	if err != nil {
		return nil, err
	}
	in := PDFInput{Quote: qt, Org: settings, Customer: customer}
	if qt.ProjectID != nil {
		p, err := timesheets.GetProject(ctx, q, orgID, *qt.ProjectID)
		if err != nil {
			return nil, err
		}
		in.ProjectName = p.Name
	}
	if settings.LogoFileID != nil {
		if in.Logo, err = readFile(ctx, q, store, orgID, *settings.LogoFileID); err != nil {
			return nil, err
		}
	}
	var buf bytes.Buffer
	if err := WriteQuotePDF(&buf, in); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func readFile(ctx context.Context, q core.Querier, store storage.Store, orgID, fileID int64) ([]byte, error) {
	f, err := core.GetFile(ctx, q, orgID, fileID)
	if err != nil {
		return nil, err
	}
	blob, err := core.OpenFile(ctx, store, f)
	if err != nil {
		return nil, err
	}
	defer blob.Close()
	return io.ReadAll(blob)
}

// PDF returns qt's PDF and download name: the PDF frozen when it was sent,
// or for a draft one rendered now (never stored).
func PDF(ctx context.Context, q core.Querier, store storage.Store, orgID int64, qt Quote) ([]byte, string, error) {
	if qt.PDFFileID != nil {
		b, err := readFile(ctx, q, store, orgID, *qt.PDFFileID)
		return b, qt.Label() + ".pdf", err
	}
	if qt.Lines == nil {
		full, err := GetQuote(ctx, q, orgID, qt.ID)
		if err != nil {
			return nil, "", err
		}
		qt = full
	}
	b, err := render(ctx, q, store, orgID, qt)
	return b, qt.Label() + " (draft).pdf", err
}

// MarkSent marks draft id sent at now: it renders the PDF from the current
// profile and stores it, so the customer's copy never changes afterwards,
// and supersedes the revision it was made from. Sending is refused once
// another revision of the same quote has been accepted.
func MarkSent(ctx context.Context, tx pgx.Tx, store storage.Store, orgID, id int64, now time.Time) (Quote, error) {
	current, err := lockQuote(ctx, tx, orgID, id)
	if err != nil {
		return Quote{}, err
	}
	if current.Status != StatusDraft {
		return Quote{}, invalidState("quote is not a draft")
	}
	var accepted bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM quotes WHERE org_id = $1 AND number = $2 AND status = 'accepted')`,
		orgID, current.Number).Scan(&accepted); err != nil {
		return Quote{}, err
	}
	if accepted {
		return Quote{}, invalidState("another revision of this quote was accepted")
	}
	full, err := GetQuote(ctx, tx, orgID, id)
	if err != nil {
		return Quote{}, err
	}
	full.SentAt = &now
	pdf, err := render(ctx, tx, store, orgID, full)
	if err != nil {
		return Quote{}, err
	}
	staged, err := core.StageFile(ctx, store, orgID, full.Label()+".pdf", bytes.NewReader(pdf), pdfPolicy)
	if err != nil {
		return Quote{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = staged.Abort()
		}
	}()
	f, err := staged.Save(ctx, tx)
	if err != nil {
		return Quote{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE quotes SET status = 'sent', sent_at = $3, pdf_file_id = $4 WHERE org_id = $1 AND id = $2`,
		orgID, id, now, f.ID); err != nil {
		return Quote{}, err
	}
	if current.ParentID != nil {
		if _, err := tx.Exec(ctx, `UPDATE quotes SET status = 'superseded' WHERE org_id = $1 AND id = $2 AND status IN ('sent', 'declined')`,
			orgID, *current.ParentID); err != nil {
			return Quote{}, err
		}
	}
	// Store the PDF before the transaction commits, like signing
	// certificates: a storage failure rolls the send back; a failed commit
	// afterwards only leaves an unreferenced blob.
	if err := staged.Commit(ctx); err != nil {
		return Quote{}, err
	}
	committed = true
	return GetQuote(ctx, tx, orgID, id)
}
