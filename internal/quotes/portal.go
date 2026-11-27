package quotes

import (
	"context"

	"shigoto/internal/core"
)

// portalStatuses are the statuses a customer can see: anything that has
// been sent. Drafts never reach the portal.
const portalStatuses = `('sent', 'accepted', 'declined', 'superseded')`

// ListPortalQuotes lists contactID's sent quotes in orgID (optionally only
// projectID's), newest first, without lines.
func ListPortalQuotes(ctx context.Context, q core.Querier, orgID, contactID int64, projectID *int64) ([]Quote, error) {
	sql := `SELECT ` + quoteColumns + ` FROM quotes WHERE org_id = $1 AND contact_id = $2 AND status IN ` + portalStatuses
	args := []any{orgID, contactID}
	if projectID != nil {
		sql += ` AND project_id = $3`
		args = append(args, *projectID)
	}
	rows, err := q.Query(ctx, sql+` ORDER BY created_at DESC, id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Quote{}
	for rows.Next() {
		qt, err := scanQuote(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, qt)
	}
	return out, rows.Err()
}

// GetPortalQuote returns quote id with its lines when it belongs to
// contactID in orgID and has been sent; otherwise core.ErrNotFound.
func GetPortalQuote(ctx context.Context, q core.Querier, orgID, contactID, id int64) (Quote, error) {
	qt, err := scanQuote(q.QueryRow(ctx, `SELECT `+quoteColumns+` FROM quotes
		WHERE org_id = $1 AND contact_id = $2 AND id = $3 AND status IN `+portalStatuses, orgID, contactID, id))
	if err != nil {
		return Quote{}, err
	}
	qt.Lines, err = quoteLines(ctx, q, orgID, id)
	return qt, err
}
