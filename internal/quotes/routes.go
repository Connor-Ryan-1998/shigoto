package quotes

import (
	"context"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"shigoto/internal/auth"
	"shigoto/internal/core"
	"shigoto/internal/module"
	"shigoto/internal/property"
	"shigoto/internal/timesheets"
)

// Module returns the quotes module, gated by config.Config.Modules["quotes"]
// (which requires timesheets).
func Module() module.Module {
	return module.Module{Name: "quotes", Routes: registerRoutes}
}

// defaultValidDays is how long a new quote is valid when no date is given.
const defaultValidDays = 30

// Today is the org-local calendar date as UTC midnight, the same basis
// quote dates are stored on.
func Today(d module.Deps) time.Time {
	loc := d.Cfg.Location
	if loc == nil {
		loc = time.UTC
	}
	y, m, dd := time.Now().In(loc).Date()
	return time.Date(y, m, dd, 0, 0, 0, 0, time.UTC)
}

func registerRoutes(mux *http.ServeMux, d module.Deps) {
	handle := func(pattern string, perm auth.Perm, fn func(http.ResponseWriter, *http.Request, module.Deps)) {
		mux.Handle(pattern, d.Auth.RequireOrg(perm)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fn(w, r, d)
		})))
	}
	handle("GET /api/v1/orgs/{org}/quotes", auth.PermQuotesRead, listHandler)
	handle("POST /api/v1/orgs/{org}/quotes", auth.PermQuotesWrite, createHandler)
	handle("GET /api/v1/orgs/{org}/quotes/{id}", auth.PermQuotesRead, getHandler)
	handle("PUT /api/v1/orgs/{org}/quotes/{id}", auth.PermQuotesWrite, updateHandler)
	handle("DELETE /api/v1/orgs/{org}/quotes/{id}", auth.PermQuotesWrite, deleteHandler)
	handle("POST /api/v1/orgs/{org}/quotes/{id}/send", auth.PermQuotesWrite, sendHandler)
	handle("POST /api/v1/orgs/{org}/quotes/{id}/decline", auth.PermQuotesWrite, declineHandler)
	handle("POST /api/v1/orgs/{org}/quotes/{id}/revise", auth.PermQuotesWrite, reviseHandler)
	handle("POST /api/v1/orgs/{org}/quotes/{id}/accept", auth.PermQuotesWrite, acceptHandler)
	handle("GET /api/v1/orgs/{org}/quotes/{id}/pdf", auth.PermQuotesRead, pdfHandler)
	handle("POST /api/v1/orgs/{org}/quotes/{id}/attachments", auth.PermQuotesWrite, uploadAttachmentHandler)
	handle("GET /api/v1/orgs/{org}/quotes/{id}/attachments/{attachmentId}", auth.PermQuotesRead, downloadAttachmentHandler)
	handle("DELETE /api/v1/orgs/{org}/quotes/{id}/attachments/{attachmentId}", auth.PermQuotesWrite, removeAttachmentHandler)
	handle("GET /api/v1/orgs/{org}/projects/{id}/costs", auth.PermReportsRead, costsHandler)
}

// --- request/response shapes ---

type lineJSON struct {
	ID            int64  `json:"id,omitempty"`
	Kind          string `json:"kind"`
	Description   string `json:"description"`
	QuantityMilli int64  `json:"quantity_milli"`
	UnitAmount    int64  `json:"unit_amount"`
	GSTCode       string `json:"gst_code"`
	GSTAmount     int64  `json:"gst_amount"`
	Amount        int64  `json:"amount"`
}

type quoteRequest struct {
	ContactID  int64      `json:"contact_id"`
	ProjectID  *int64     `json:"project_id"`
	Title      string     `json:"title"`
	Terms      *string    `json:"terms"`
	IssueDate  string     `json:"issue_date"`
	ValidUntil string     `json:"valid_until"`
	Lines      []lineJSON `json:"lines"`
}

type attachmentJSON struct {
	ID        int64               `json:"id"`
	CreatedAt time.Time           `json:"created_at"`
	File      module.FileResponse `json:"file"`
}

type revisionJSON struct {
	ID          int64  `json:"id"`
	NumberLabel string `json:"number_label"`
	Status      string `json:"status"`
}

// QuoteJSON is a quote as the APIs return it. Lines, Attachments and
// Revisions are only filled on detail responses.
type QuoteJSON struct {
	ID            int64             `json:"id"`
	ContactID     int64             `json:"contact_id"`
	ContactName   string            `json:"contact_name"`
	ProjectID     *int64            `json:"project_id"`
	ProjectName   string            `json:"project_name"`
	Number        int               `json:"number"`
	Revision      int               `json:"revision"`
	NumberLabel   string            `json:"number_label"`
	ParentID      *int64            `json:"parent_id"`
	Title         string            `json:"title"`
	Terms         string            `json:"terms"`
	Status        string            `json:"status"`
	DisplayStatus string            `json:"display_status"`
	IssueDate     string            `json:"issue_date"`
	ValidUntil    string            `json:"valid_until"`
	Subtotal      int64             `json:"subtotal"`
	GSTTotal      int64             `json:"gst_total"`
	Total         int64             `json:"total"`
	SentAt        *time.Time        `json:"sent_at"`
	DecidedAt     *time.Time        `json:"decided_at"`
	Lines         []lineJSON        `json:"lines,omitempty"`
	Attachments   *[]attachmentJSON `json:"attachments,omitempty"`
	Revisions     []revisionJSON    `json:"revisions,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
}

// names resolves contact and project names for qs in one query each.
func names(ctx context.Context, q core.Querier, orgID int64, qs []Quote) (map[int64]string, map[int64]string, error) {
	var contactIDs, projectIDs []int64
	for _, qt := range qs {
		contactIDs = append(contactIDs, qt.ContactID)
		if qt.ProjectID != nil {
			projectIDs = append(projectIDs, *qt.ProjectID)
		}
	}
	load := func(sql string, ids []int64) (map[int64]string, error) {
		out := map[int64]string{}
		if len(ids) == 0 {
			return out, nil
		}
		rows, err := q.Query(ctx, sql, orgID, ids)
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
	contacts, err := load(`SELECT id, name FROM contacts WHERE org_id = $1 AND id = ANY($2)`, contactIDs)
	if err != nil {
		return nil, nil, err
	}
	projects, err := load(`SELECT id, name FROM projects WHERE org_id = $1 AND id = ANY($2)`, projectIDs)
	return contacts, projects, err
}

// ToJSON is qt as the API returns it, with names from contacts/projects.
func ToJSON(qt Quote, today time.Time, contacts, projects map[int64]string) QuoteJSON {
	out := QuoteJSON{
		ID: qt.ID, ContactID: qt.ContactID, ContactName: contacts[qt.ContactID], ProjectID: qt.ProjectID,
		Number: qt.Number, Revision: qt.Revision, NumberLabel: qt.Label(), ParentID: qt.ParentID,
		Title: qt.Title, Terms: qt.Terms, Status: qt.Status, DisplayStatus: qt.DisplayStatus(today),
		IssueDate: qt.IssueDate.Format(time.DateOnly), ValidUntil: qt.ValidUntil.Format(time.DateOnly),
		Subtotal: int64(qt.Subtotal), GSTTotal: int64(qt.GSTTotal), Total: int64(qt.Total),
		SentAt: qt.SentAt, DecidedAt: qt.DecidedAt, CreatedAt: qt.CreatedAt,
	}
	if qt.ProjectID != nil {
		out.ProjectName = projects[*qt.ProjectID]
	}
	for _, l := range qt.Lines {
		out.Lines = append(out.Lines, lineJSON{
			ID: l.ID, Kind: l.Kind, Description: l.Description, QuantityMilli: l.QuantityMilli,
			UnitAmount: int64(l.UnitAmount), GSTCode: string(l.GSTCode), GSTAmount: int64(l.GSTAmount), Amount: int64(l.Amount),
		})
	}
	return out
}

func parseDate(s string, fallback time.Time) (time.Time, error) {
	if s == "" {
		return fallback, nil
	}
	return time.Parse(time.DateOnly, s)
}

// draftFromRequest builds a Draft; terms default to the org's quote terms
// only when defaultTerms is set (on create) and the key was omitted.
func draftFromRequest(ctx context.Context, d module.Deps, orgID int64, body quoteRequest, defaultTerms bool) (Draft, error) {
	today := Today(d)
	issue, err := parseDate(body.IssueDate, today)
	if err != nil {
		return Draft{}, invalidInput("issue_date must be YYYY-MM-DD")
	}
	until, err := parseDate(body.ValidUntil, issue.AddDate(0, 0, defaultValidDays))
	if err != nil {
		return Draft{}, invalidInput("valid_until must be YYYY-MM-DD")
	}
	draft := Draft{ContactID: body.ContactID, ProjectID: body.ProjectID, Title: body.Title, IssueDate: issue, ValidUntil: until}
	switch {
	case body.Terms != nil:
		draft.Terms = *body.Terms
	case defaultTerms:
		settings, err := core.GetSettings(ctx, d.Pool, orgID)
		if err != nil {
			return Draft{}, err
		}
		draft.Terms = settings.QuoteTerms
	}
	for _, l := range body.Lines {
		draft.Lines = append(draft.Lines, NewLine{Kind: l.Kind, Description: l.Description,
			QuantityMilli: l.QuantityMilli, UnitAmount: core.Cents(l.UnitAmount), GSTCode: l.GSTCode})
	}
	return draft, nil
}

// writeQuote writes quote id's full detail response.
func writeQuote(w http.ResponseWriter, r *http.Request, d module.Deps, status int, orgID, id int64) {
	ctx := r.Context()
	qt, err := GetQuote(ctx, d.Pool, orgID, id)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	body, err := detailJSON(ctx, d, orgID, qt)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	module.WriteJSON(w, status, body)
}

func detailJSON(ctx context.Context, d module.Deps, orgID int64, qt Quote) (QuoteJSON, error) {
	contacts, projects, err := names(ctx, d.Pool, orgID, []Quote{qt})
	if err != nil {
		return QuoteJSON{}, err
	}
	out := ToJSON(qt, Today(d), contacts, projects)
	atts, err := ListAttachments(ctx, d.Pool, orgID, qt.ID)
	if err != nil {
		return QuoteJSON{}, err
	}
	list := make([]attachmentJSON, len(atts))
	for i, a := range atts {
		list[i] = attachmentJSON{ID: a.ID, CreatedAt: a.CreatedAt, File: module.FileToResponse(a.File)}
	}
	out.Attachments = &list
	rows, err := d.Pool.Query(ctx, `SELECT id, number, revision, status FROM quotes WHERE org_id = $1 AND number = $2 ORDER BY revision`, orgID, qt.Number)
	if err != nil {
		return QuoteJSON{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var rv revisionJSON
		var number, revision int
		if err := rows.Scan(&rv.ID, &number, &revision, &rv.Status); err != nil {
			return QuoteJSON{}, err
		}
		rv.NumberLabel = FormatNumber(number, revision)
		out.Revisions = append(out.Revisions, rv)
	}
	return out, rows.Err()
}

// --- handlers ---

func optionalID(r *http.Request, key string) (*int64, bool) {
	s := r.URL.Query().Get(key)
	if s == "" {
		return nil, true
	}
	id, err := strconv.ParseInt(s, 10, 64)
	return &id, err == nil
}

func listHandler(w http.ResponseWriter, r *http.Request, d module.Deps) {
	f := ListFilter{Status: r.URL.Query().Get("status")}
	var ok bool
	if f.ContactID, ok = optionalID(r, "contact_id"); !ok {
		module.WriteError(w, http.StatusUnprocessableEntity, "contact_id must be an integer")
		return
	}
	if f.ProjectID, ok = optionalID(r, "project_id"); !ok {
		module.WriteError(w, http.StatusUnprocessableEntity, "project_id must be an integer")
		return
	}
	switch f.Status {
	case "", StatusDraft, StatusSent, StatusAccepted, StatusDeclined, StatusSuperseded:
	default:
		module.WriteError(w, http.StatusUnprocessableEntity, "status must be draft, sent, accepted, declined or superseded")
		return
	}
	f.Limit, _ = strconv.Atoi(r.URL.Query().Get("limit"))
	f.Offset, _ = strconv.Atoi(r.URL.Query().Get("offset"))
	p := auth.From(r.Context())
	qs, total, err := ListQuotes(r.Context(), d.Pool, p.OrgID, f)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	contacts, projects, err := names(r.Context(), d.Pool, p.OrgID, qs)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	today := Today(d)
	out := make([]QuoteJSON, len(qs))
	for i, qt := range qs {
		out[i] = ToJSON(qt, today, contacts, projects)
	}
	module.WriteJSON(w, http.StatusOK, map[string]any{"quotes": out, "total": total})
}

func readQuoteRequest(w http.ResponseWriter, r *http.Request) (quoteRequest, bool) {
	var body quoteRequest
	if err := module.ReadJSON(r, &body); err != nil {
		module.WriteError(w, http.StatusBadRequest, "invalid request body")
		return body, false
	}
	return body, true
}

func createHandler(w http.ResponseWriter, r *http.Request, d module.Deps) {
	body, ok := readQuoteRequest(w, r)
	if !ok {
		return
	}
	p := auth.From(r.Context())
	draft, err := draftFromRequest(r.Context(), d, p.OrgID, body, true)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	var created Quote
	err = module.RunInTx(r.Context(), d.Pool, func(tx pgx.Tx) error {
		var err error
		if created, err = CreateDraft(r.Context(), tx, p.OrgID, draft); err != nil {
			return err
		}
		return core.Audit(r.Context(), tx, p.OrgID, &p.UserID, "quote", created.ID, "create", body)
	})
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	writeQuote(w, r, d, http.StatusCreated, p.OrgID, created.ID)
}

func getHandler(w http.ResponseWriter, r *http.Request, d module.Deps) {
	id, ok := module.PathID(r)
	if !ok {
		module.WriteError(w, http.StatusNotFound, "not found")
		return
	}
	writeQuote(w, r, d, http.StatusOK, auth.From(r.Context()).OrgID, id)
}

func updateHandler(w http.ResponseWriter, r *http.Request, d module.Deps) {
	id, ok := module.PathID(r)
	if !ok {
		module.WriteError(w, http.StatusNotFound, "not found")
		return
	}
	body, ok := readQuoteRequest(w, r)
	if !ok {
		return
	}
	p := auth.From(r.Context())
	draft, err := draftFromRequest(r.Context(), d, p.OrgID, body, false)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	err = module.RunInTx(r.Context(), d.Pool, func(tx pgx.Tx) error {
		if _, err := UpdateDraft(r.Context(), tx, p.OrgID, id, draft); err != nil {
			return err
		}
		return core.Audit(r.Context(), tx, p.OrgID, &p.UserID, "quote", id, "update", body)
	})
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	writeQuote(w, r, d, http.StatusOK, p.OrgID, id)
}

func deleteHandler(w http.ResponseWriter, r *http.Request, d module.Deps) {
	id, ok := module.PathID(r)
	if !ok {
		module.WriteError(w, http.StatusNotFound, "not found")
		return
	}
	p := auth.From(r.Context())
	var fileIDs []int64
	err := module.RunInTx(r.Context(), d.Pool, func(tx pgx.Tx) error {
		var err error
		if fileIDs, err = DeleteDraftFiles(r.Context(), tx, p.OrgID, id); err != nil {
			return err
		}
		return core.Audit(r.Context(), tx, p.OrgID, &p.UserID, "quote", id, "delete", nil)
	})
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	for _, fid := range fileIDs {
		dropFile(r, d, fid)
	}
	w.WriteHeader(http.StatusNoContent)
}

// dropFile deletes a file once nothing references it; a failure only
// leaks the file, so it is logged.
func dropFile(r *http.Request, d module.Deps, fileID int64) {
	if err := property.DeleteFileIfUnreferenced(r.Context(), d.Pool, d.Store, fileID); err != nil {
		slog.Error("deleting quote file failed", "file_id", fileID, "error", err)
	}
}

// action runs fn on quote {id} in a transaction, audits it and writes the
// quote's detail response.
func action(w http.ResponseWriter, r *http.Request, d module.Deps, name string, status int, fn func(ctx context.Context, tx pgx.Tx, p auth.Principal, id int64) (int64, error)) {
	id, ok := module.PathID(r)
	if !ok {
		module.WriteError(w, http.StatusNotFound, "not found")
		return
	}
	p := auth.From(r.Context())
	var resultID int64
	err := module.RunInTx(r.Context(), d.Pool, func(tx pgx.Tx) error {
		var err error
		if resultID, err = fn(r.Context(), tx, p, id); err != nil {
			return err
		}
		return core.Audit(r.Context(), tx, p.OrgID, &p.UserID, "quote", resultID, name, nil)
	})
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	writeQuote(w, r, d, status, p.OrgID, resultID)
}

func sendHandler(w http.ResponseWriter, r *http.Request, d module.Deps) {
	action(w, r, d, "send", http.StatusOK, func(ctx context.Context, tx pgx.Tx, p auth.Principal, id int64) (int64, error) {
		qt, err := MarkSent(ctx, tx, d.Store, p.OrgID, id, time.Now())
		return qt.ID, err
	})
}

func declineHandler(w http.ResponseWriter, r *http.Request, d module.Deps) {
	action(w, r, d, "decline", http.StatusOK, func(ctx context.Context, tx pgx.Tx, p auth.Principal, id int64) (int64, error) {
		qt, err := Decline(ctx, tx, p.OrgID, p.UserID, id, time.Now())
		return qt.ID, err
	})
}

func reviseHandler(w http.ResponseWriter, r *http.Request, d module.Deps) {
	action(w, r, d, "revise", http.StatusCreated, func(ctx context.Context, tx pgx.Tx, p auth.Principal, id int64) (int64, error) {
		qt, err := Revise(ctx, tx, p.OrgID, id, Today(d))
		return qt.ID, err
	})
}

func acceptHandler(w http.ResponseWriter, r *http.Request, d module.Deps) {
	p := auth.From(r.Context())
	if !auth.Can(p.Role, auth.PermTimesheetsAdmin) {
		module.WriteError(w, http.StatusForbidden, "accepting a quote needs permission to create projects")
		return
	}
	id, ok := module.PathID(r)
	if !ok {
		module.WriteError(w, http.StatusNotFound, "not found")
		return
	}
	var project timesheets.Project
	err := module.RunInTx(r.Context(), d.Pool, func(tx pgx.Tx) error {
		var err error
		if _, project, err = Accept(r.Context(), tx, p.OrgID, p.UserID, id, time.Now()); err != nil {
			return err
		}
		return core.Audit(r.Context(), tx, p.OrgID, &p.UserID, "quote", id, "accept", map[string]int64{"project_id": project.ID})
	})
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	qt, err := GetQuote(r.Context(), d.Pool, p.OrgID, id)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	body, err := detailJSON(r.Context(), d, p.OrgID, qt)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	module.WriteJSON(w, http.StatusOK, map[string]any{"quote": body, "project": timesheets.ProjectJSON(project)})
}

func pdfHandler(w http.ResponseWriter, r *http.Request, d module.Deps) {
	id, ok := module.PathID(r)
	if !ok {
		module.WriteError(w, http.StatusNotFound, "not found")
		return
	}
	p := auth.From(r.Context())
	qt, err := GetQuote(r.Context(), d.Pool, p.OrgID, id)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	WritePDF(w, r, d, p.OrgID, qt, false)
}

// WritePDF writes qt's PDF (frozen once sent), inline or as a download.
func WritePDF(w http.ResponseWriter, r *http.Request, d module.Deps, orgID int64, qt Quote, inline bool) {
	pdf, name, err := PDF(r.Context(), d.Pool, d.Store, orgID, qt)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	disposition := "attachment"
	if inline {
		disposition = "inline"
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": name}))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pdf)
}

func attachmentPath(r *http.Request) (quoteID, attachmentID int64, ok bool) {
	quoteID, ok = module.PathID(r)
	if !ok {
		return 0, 0, false
	}
	attachmentID, err := strconv.ParseInt(r.PathValue("attachmentId"), 10, 64)
	return quoteID, attachmentID, err == nil
}

func uploadAttachmentHandler(w http.ResponseWriter, r *http.Request, d module.Deps) {
	id, ok := module.PathID(r)
	if !ok {
		module.WriteError(w, http.StatusNotFound, "not found")
		return
	}
	p := auth.From(r.Context())
	if err := requireQuote(r.Context(), d.Pool, p.OrgID, id); err != nil {
		module.WriteCoreError(w, err)
		return
	}
	var created Attachment
	if _, ok := module.HandleUpload(w, r, d, core.DocumentPolicy, func(tx pgx.Tx, f core.File) error {
		var err error
		created, err = AddAttachment(r.Context(), tx, p.OrgID, id, p.UserID, f)
		return err
	}); !ok {
		return
	}
	module.WriteJSON(w, http.StatusCreated, attachmentJSON{ID: created.ID, CreatedAt: created.CreatedAt, File: module.FileToResponse(created.File)})
}

func downloadAttachmentHandler(w http.ResponseWriter, r *http.Request, d module.Deps) {
	quoteID, attachmentID, ok := attachmentPath(r)
	if !ok {
		module.WriteError(w, http.StatusNotFound, "not found")
		return
	}
	a, err := GetAttachment(r.Context(), d.Pool, auth.From(r.Context()).OrgID, quoteID, attachmentID)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	module.ServeFile(w, r, d.Store, a.File, true)
}

func removeAttachmentHandler(w http.ResponseWriter, r *http.Request, d module.Deps) {
	quoteID, attachmentID, ok := attachmentPath(r)
	if !ok {
		module.WriteError(w, http.StatusNotFound, "not found")
		return
	}
	p := auth.From(r.Context())
	var fileID int64
	err := module.RunInTx(r.Context(), d.Pool, func(tx pgx.Tx) error {
		var err error
		if fileID, err = RemoveAttachment(r.Context(), tx, p.OrgID, quoteID, attachmentID); err != nil {
			return err
		}
		return core.Audit(r.Context(), tx, p.OrgID, &p.UserID, "quote", quoteID, "remove_attachment", map[string]int64{"file_id": fileID})
	})
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	dropFile(r, d, fileID)
	w.WriteHeader(http.StatusNoContent)
}

type costLineJSON struct {
	ExpenseID   int64  `json:"expense_id"`
	SpentOn     string `json:"spent_on"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Supplier    string `json:"supplier"`
	Amount      int64  `json:"amount"`
}

func costsHandler(w http.ResponseWriter, r *http.Request, d module.Deps) {
	id, ok := module.PathID(r)
	if !ok {
		module.WriteError(w, http.StatusNotFound, "not found")
		return
	}
	c, err := ProjectCosts(r.Context(), d.Pool, auth.From(r.Context()).OrgID, id)
	if err != nil {
		if !errors.Is(err, core.ErrNotFound) {
			slog.Error("project costs failed", "error", err)
		}
		module.WriteCoreError(w, err)
		return
	}
	lines := make([]costLineJSON, len(c.Expenses))
	for i, l := range c.Expenses {
		lines[i] = costLineJSON{ExpenseID: l.ExpenseID, SpentOn: l.SpentOn.Format(time.DateOnly), Description: l.Description,
			Category: l.Category, Supplier: l.Supplier, Amount: int64(l.Amount)}
	}
	module.WriteJSON(w, http.StatusOK, map[string]any{
		"quoted_labour": c.QuotedLabour, "quoted_materials": c.QuotedMaterials, "quoted_total": c.QuotedTotal,
		"invoiced": c.Invoiced, "expenses_total": c.ExpensesTotal, "expenses": lines,
	})
}
