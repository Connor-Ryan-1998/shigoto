package clients

import (
	"context"

	"shigoto/internal/core"
)

// PortalDocument is a document as its client sees it: never a draft, with
// the project's name and whether a general signed document applies to
// every project.
type PortalDocument struct {
	Document
	ProjectName  string
	AppliesToAll bool
}

// ListPortalDocuments returns contactID's documents visible in the portal
// (every status except draft), newest first. With projectID it returns
// that project's documents plus general (no project) signed documents,
// which are flagged AppliesToAll.
func ListPortalDocuments(ctx context.Context, q core.Querier, orgID, contactID int64, projectID *int64) ([]PortalDocument, error) {
	rows, err := q.Query(ctx, `SELECT `+documentColumns+`, coalesce(p.name, '')`+documentFrom+`
		LEFT JOIN projects p ON p.id = d.project_id
		WHERE d.org_id = $1 AND d.contact_id = $2 AND d.status <> 'draft'
		  AND ($3::bigint IS NULL OR d.project_id = $3 OR (d.project_id IS NULL AND d.status = 'signed'))
		ORDER BY d.created_at DESC, d.id DESC`, orgID, contactID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PortalDocument{}
	for rows.Next() {
		var pd PortalDocument
		doc, err := scanDocument(scanExtra{rows, &pd.ProjectName})
		if err != nil {
			return nil, err
		}
		pd.Document = doc
		pd.AppliesToAll = projectID != nil && doc.ProjectID == nil
		out = append(out, pd)
	}
	return out, rows.Err()
}

// GetPortalDocument returns contactID's non-draft document id, else
// core.ErrNotFound.
func GetPortalDocument(ctx context.Context, q core.Querier, orgID, contactID, id int64) (Document, error) {
	d, err := GetDocument(ctx, q, orgID, id)
	if err != nil {
		return Document{}, err
	}
	if d.ContactID != contactID || d.Status == StatusDraft {
		return Document{}, core.ErrNotFound
	}
	return d, nil
}

// scanExtra lets scanDocument scan a row carrying one trailing column.
type scanExtra struct {
	row   interface{ Scan(...any) error }
	extra *string
}

func (s scanExtra) Scan(dest ...any) error { return s.row.Scan(append(dest, s.extra)...) }
