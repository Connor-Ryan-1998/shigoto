package portal

import (
	"net/http"
	"strconv"
	"time"

	"shigoto/internal/auth"
	"shigoto/internal/core"
	"shigoto/internal/module"
	"shigoto/internal/quotes"
)

// portalQuoteLine is a quote line as the customer sees it.
type portalQuoteLine struct {
	Kind          string `json:"kind"`
	Description   string `json:"description"`
	QuantityMilli int64  `json:"quantity_milli"`
	UnitAmount    int64  `json:"unit_amount"`
	GSTCode       string `json:"gst_code"`
	Amount        int64  `json:"amount"`
}

// portalQuote is a quote as the customer sees it: never its attachments,
// which are staff records. Lines, totals and terms are on the detail only.
type portalQuote struct {
	ID            int64             `json:"id"`
	NumberLabel   string            `json:"number_label"`
	Title         string            `json:"title"`
	ProjectID     *int64            `json:"project_id"`
	ProjectName   string            `json:"project_name"`
	Status        string            `json:"status"`
	DisplayStatus string            `json:"display_status"`
	IssueDate     string            `json:"issue_date"`
	ValidUntil    string            `json:"valid_until"`
	Subtotal      *int64            `json:"subtotal,omitempty"`
	GSTTotal      *int64            `json:"gst_total,omitempty"`
	Total         int64             `json:"total"`
	Terms         *string           `json:"terms,omitempty"`
	Lines         []portalQuoteLine `json:"lines,omitempty"`
}

func registerQuoteRoutes(handle func(string, func(http.ResponseWriter, *http.Request, module.Deps, auth.Principal))) {
	handle("GET /api/v1/portal/{org}/quotes", listClientQuotes)
	handle("GET /api/v1/portal/{org}/quotes/{id}", getClientQuote)
	handle("GET /api/v1/portal/{org}/quotes/{id}/pdf", clientQuotePDF)
}

func toPortalQuote(qt quotes.Quote, today time.Time, projects map[int64]string, detail bool) portalQuote {
	out := portalQuote{
		ID: qt.ID, NumberLabel: qt.Label(), Title: qt.Title, ProjectID: qt.ProjectID,
		Status: qt.Status, DisplayStatus: qt.DisplayStatus(today),
		IssueDate: qt.IssueDate.Format(time.DateOnly), ValidUntil: qt.ValidUntil.Format(time.DateOnly),
		Total: int64(qt.Total),
	}
	if qt.ProjectID != nil {
		out.ProjectName = projects[*qt.ProjectID]
	}
	if detail {
		subtotal, gst, terms := int64(qt.Subtotal), int64(qt.GSTTotal), qt.Terms
		out.Subtotal, out.GSTTotal, out.Terms = &subtotal, &gst, &terms
		for _, l := range qt.Lines {
			out.Lines = append(out.Lines, portalQuoteLine{Kind: l.Kind, Description: l.Description,
				QuantityMilli: l.QuantityMilli, UnitAmount: int64(l.UnitAmount), GSTCode: string(l.GSTCode), Amount: int64(l.Amount)})
		}
	}
	return out
}

// projectNames maps the projects of qs to their names, scoped to orgID.
func projectNames(r *http.Request, d module.Deps, orgID int64, qs []quotes.Quote) (map[int64]string, error) {
	var ids []int64
	for _, qt := range qs {
		if qt.ProjectID != nil {
			ids = append(ids, *qt.ProjectID)
		}
	}
	out := map[int64]string{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := d.Pool.Query(r.Context(), `SELECT id, name FROM projects WHERE org_id = $1 AND id = ANY($2)`, orgID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}

func listClientQuotes(w http.ResponseWriter, r *http.Request, d module.Deps, p auth.Principal) {
	var projectID *int64
	if s := r.URL.Query().Get("project_id"); s != "" {
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			module.WriteError(w, http.StatusUnprocessableEntity, "project_id must be an integer")
			return
		}
		projectID = &id
	}
	qs, err := quotes.ListPortalQuotes(r.Context(), d.Pool, p.OrgID, *p.ContactID, projectID)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	names, err := projectNames(r, d, p.OrgID, qs)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	today := quotes.Today(d)
	out := make([]portalQuote, len(qs))
	for i, qt := range qs {
		out[i] = toPortalQuote(qt, today, names, false)
	}
	module.WriteJSON(w, http.StatusOK, map[string]any{"quotes": out})
}

func portalQuoteByPath(w http.ResponseWriter, r *http.Request, d module.Deps, p auth.Principal) (quotes.Quote, bool) {
	id, ok := module.PathID(r)
	if !ok {
		module.WriteError(w, http.StatusNotFound, "not found")
		return quotes.Quote{}, false
	}
	qt, err := quotes.GetPortalQuote(r.Context(), d.Pool, p.OrgID, *p.ContactID, id)
	if err != nil {
		module.WriteCoreError(w, err)
		return quotes.Quote{}, false
	}
	return qt, true
}

func getClientQuote(w http.ResponseWriter, r *http.Request, d module.Deps, p auth.Principal) {
	qt, ok := portalQuoteByPath(w, r, d, p)
	if !ok {
		return
	}
	names, err := projectNames(r, d, p.OrgID, []quotes.Quote{qt})
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	module.WriteJSON(w, http.StatusOK, toPortalQuote(qt, quotes.Today(d), names, true))
}

func clientQuotePDF(w http.ResponseWriter, r *http.Request, d module.Deps, p auth.Principal) {
	qt, ok := portalQuoteByPath(w, r, d, p)
	if !ok {
		return
	}
	if qt.PDFFileID == nil {
		// Every sent quote has a frozen PDF; never render one for a customer.
		module.WriteCoreError(w, core.ErrNotFound)
		return
	}
	quotes.WritePDF(w, r, d, p.OrgID, qt, true)
}
