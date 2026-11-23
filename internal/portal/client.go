package portal

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"shigoto/internal/auth"
	"shigoto/internal/billing"
	"shigoto/internal/clients"
	"shigoto/internal/core"
	"shigoto/internal/module"
	"shigoto/internal/timesheets"
)

// registerClientRoutes mounts the client-only portal API: the hub, project
// pages (only with the timesheets module, which owns projects), documents
// and signing. Every route requires a client membership with a contact and
// scopes every query to it; anything else is a 404.
func registerClientRoutes(mux *http.ServeMux, d module.Deps) {
	handle := func(pattern string, fn func(http.ResponseWriter, *http.Request, module.Deps, auth.Principal)) {
		mux.Handle(pattern, d.Auth.RequirePortal(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := auth.From(r.Context())
			if p.Role != auth.Client || p.ContactID == nil {
				module.WriteError(w, http.StatusNotFound, "not found")
				return
			}
			fn(w, r, d, p)
		})))
	}
	handle("GET /api/v1/portal/{org}/hub", getHub)
	if d.Cfg.Modules["timesheets"] {
		handle("GET /api/v1/portal/{org}/projects/{id}", getClientProject)
	}
	handle("GET /api/v1/portal/{org}/documents", listClientDocuments)
	handle("GET /api/v1/portal/{org}/documents/{id}", getClientDocument)
	handle("GET /api/v1/portal/{org}/documents/{id}/file", func(w http.ResponseWriter, r *http.Request, d module.Deps, p auth.Principal) {
		serveClientDocument(w, r, d, p, false)
	})
	handle("GET /api/v1/portal/{org}/documents/{id}/certificate", func(w http.ResponseWriter, r *http.Request, d module.Deps, p auth.Principal) {
		serveClientDocument(w, r, d, p, true)
	})
	handle("POST /api/v1/portal/{org}/documents/{id}/sign", signClientDocument)
}

type portalDocumentResponse struct {
	ID           int64                      `json:"id"`
	ProjectID    *int64                     `json:"project_id"`
	ProjectName  string                     `json:"project_name"`
	Title        string                     `json:"title"`
	Kind         string                     `json:"kind"`
	Category     string                     `json:"category"`
	Status       string                     `json:"status"`
	SentAt       *string                    `json:"sent_at"`
	AppliesToAll bool                       `json:"applies_to_all"`
	File         module.FileResponse        `json:"file"`
	Signature    *clients.SignatureResponse `json:"signature"`
	ConsentText  string                     `json:"consent_text,omitempty"`
}

func toPortalDocument(ctx context.Context, q core.Querier, orgID int64, pd clients.PortalDocument) (portalDocumentResponse, error) {
	f, err := core.GetFile(ctx, q, orgID, pd.FileID)
	if err != nil {
		return portalDocumentResponse{}, err
	}
	var sentAt *string
	if pd.SentAt != nil {
		s := pd.SentAt.UTC().Format(time.RFC3339)
		sentAt = &s
	}
	return portalDocumentResponse{
		ID: pd.ID, ProjectID: pd.ProjectID, ProjectName: pd.ProjectName, Title: pd.Title, Kind: pd.Kind,
		Category: pd.Category, Status: pd.Status, SentAt: sentAt, AppliesToAll: pd.AppliesToAll,
		File: fileResponse(f, pd.Document), Signature: clients.SignatureToResponse(pd.Signature),
	}, nil
}

func toPortalDocuments(ctx context.Context, q core.Querier, orgID int64, docs []clients.PortalDocument) ([]portalDocumentResponse, error) {
	out := make([]portalDocumentResponse, 0, len(docs))
	for _, pd := range docs {
		resp, err := toPortalDocument(ctx, q, orgID, pd)
		if err != nil {
			return nil, err
		}
		out = append(out, resp)
	}
	return out, nil
}

// fileResponse is f's JSON shape under doc's own name: the stored name may
// belong to another client's identical upload.
func fileResponse(f core.File, doc clients.Document) module.FileResponse {
	resp := module.FileToResponse(f)
	resp.OriginalName = clients.DocumentFileName(doc)
	return resp
}

func isToSign(d clients.Document) bool {
	return d.Kind == clients.KindSignature && d.Status == clients.StatusSent
}

type hubProject struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	Status    string  `json:"status"`
	StartDate *string `json:"start_date"`
	EndDate   *string `json:"end_date"`
	ToSign    int     `json:"to_sign"`
}

func dateOrNil(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format("2006-01-02")
	return &s
}

func getHub(w http.ResponseWriter, r *http.Request, d module.Deps, p auth.Principal) {
	ctx, contactID := r.Context(), *p.ContactID
	welcome, err := clients.GetWelcome(ctx, d.Pool, p.OrgID, contactID)
	if err != nil {
		// A client membership on a non-client contact sees an empty hub
		// rather than an error.
		if !errors.Is(err, clients.ErrNotClient) {
			slog.Error("loading client welcome failed", "error", err)
			module.WriteError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	checklist := []clients.ChecklistItem{}
	if err == nil {
		if checklist, err = clients.ListChecklist(ctx, d.Pool, p.OrgID, contactID); err != nil {
			slog.Error("loading client checklist failed", "error", err)
			module.WriteError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	docs, err := clients.ListPortalDocuments(ctx, d.Pool, p.OrgID, contactID, nil)
	if err != nil {
		slog.Error("loading client documents failed", "error", err)
		module.WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	counts := map[string]int{"to_sign": 0, "signed": 0, "shared": 0}
	toSignByProject := map[int64]int{}
	for _, doc := range docs {
		switch {
		case isToSign(doc.Document):
			counts["to_sign"]++
			if doc.ProjectID != nil {
				toSignByProject[*doc.ProjectID]++
			}
		case doc.Status == clients.StatusSigned:
			counts["signed"]++
		case doc.Kind == clients.KindShared && doc.Status == clients.StatusSent:
			counts["shared"]++
		}
	}
	projects := []hubProject{}
	if d.Cfg.Modules["timesheets"] {
		list, err := timesheets.ListProjects(ctx, d.Pool, p.OrgID, &contactID, false)
		if err != nil {
			slog.Error("loading client projects failed", "error", err)
			module.WriteError(w, http.StatusInternalServerError, "internal error")
			return
		}
		for _, pr := range list {
			projects = append(projects, hubProject{ID: pr.ID, Name: pr.Name, Status: pr.Status,
				StartDate: dateOrNil(pr.StartDate), EndDate: dateOrNil(pr.EndDate), ToSign: toSignByProject[pr.ID]})
		}
	}
	module.WriteJSON(w, http.StatusOK, map[string]any{
		"welcome":   map[string]string{"notes": welcome.Notes},
		"checklist": clients.ChecklistToResponse(checklist),
		"projects":  projects,
		"documents": counts,
	})
}

// splitLines returns s's non-blank lines, trimmed.
func splitLines(s string) []string {
	out := []string{}
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func getClientProject(w http.ResponseWriter, r *http.Request, d module.Deps, p auth.Principal) {
	id, ok := module.PathID(r)
	if !ok {
		module.WriteError(w, http.StatusNotFound, "not found")
		return
	}
	ctx, contactID := r.Context(), *p.ContactID
	proj, err := timesheets.GetProject(ctx, d.Pool, p.OrgID, id)
	if err != nil || proj.ClientContactID != contactID {
		if err != nil && !errors.Is(err, core.ErrNotFound) {
			slog.Error("loading client project failed", "error", err)
			module.WriteError(w, http.StatusInternalServerError, "internal error")
			return
		}
		module.WriteError(w, http.StatusNotFound, "not found")
		return
	}
	docs, err := clients.ListPortalDocuments(ctx, d.Pool, p.OrgID, contactID, &proj.ID)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	groups := map[string][]clients.PortalDocument{"to_sign": {}, "signed": {}, "discovery": {}, "shared": {}, "inactive": {}}
	for _, doc := range docs {
		key := "shared"
		switch {
		case doc.Status == clients.StatusWithdrawn || doc.Status == clients.StatusSuperseded:
			key = "inactive"
		case isToSign(doc.Document):
			key = "to_sign"
		case doc.Status == clients.StatusSigned:
			key = "signed"
		case doc.Category == clients.CategoryDiscovery:
			key = "discovery"
		}
		groups[key] = append(groups[key], doc)
	}
	grouped := map[string][]portalDocumentResponse{}
	for k, list := range groups {
		if grouped[k], err = toPortalDocuments(ctx, d.Pool, p.OrgID, list); err != nil {
			module.WriteCoreError(w, err)
			return
		}
	}
	invoices, err := ListProjectInvoices(ctx, d.Pool, d.Billing, p.OrgID, contactID, proj.ID)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	module.WriteJSON(w, http.StatusOK, map[string]any{
		"id": proj.ID, "name": proj.Name, "status": proj.Status, "summary": proj.Summary,
		"goals": splitLines(proj.Goals), "stakeholders": splitLines(proj.Stakeholders),
		"start_date": dateOrNil(proj.StartDate), "end_date": dateOrNil(proj.EndDate),
		"documents": grouped, "invoices": invoices,
	})
}

// ListProjectInvoices returns contactID's portal-visible invoices (as
// ListInvoices) that have at least one line on projectID.
func ListProjectInvoices(ctx context.Context, q core.Querier, bill *billing.Service, orgID, contactID, projectID int64) ([]billing.InvoiceResponse, error) {
	rows, err := q.Query(ctx, `SELECT DISTINCT invoice_id FROM invoice_lines WHERE org_id = $1 AND project_id = $2`, orgID, projectID)
	if err != nil {
		return nil, err
	}
	onProject := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		onProject[id] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	all, err := ListInvoices(ctx, q, bill, orgID, contactID)
	if err != nil {
		return nil, err
	}
	out := []billing.InvoiceResponse{}
	for _, inv := range all {
		if onProject[inv.ID] {
			out = append(out, inv)
		}
	}
	return out, nil
}

func listClientDocuments(w http.ResponseWriter, r *http.Request, d module.Deps, p auth.Principal) {
	docs, err := clients.ListPortalDocuments(r.Context(), d.Pool, p.OrgID, *p.ContactID, nil)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	out, err := toPortalDocuments(r.Context(), d.Pool, p.OrgID, docs)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	module.WriteJSON(w, http.StatusOK, out)
}

// portalDocument loads the principal's non-draft document {id} with its
// project name, writing a 404 itself when it isn't theirs.
func portalDocument(w http.ResponseWriter, r *http.Request, d module.Deps, p auth.Principal) (clients.PortalDocument, bool) {
	id, ok := module.PathID(r)
	if !ok {
		module.WriteError(w, http.StatusNotFound, "not found")
		return clients.PortalDocument{}, false
	}
	doc, err := clients.GetPortalDocument(r.Context(), d.Pool, p.OrgID, *p.ContactID, id)
	if err != nil {
		module.WriteCoreError(w, err)
		return clients.PortalDocument{}, false
	}
	pd := clients.PortalDocument{Document: doc}
	if doc.ProjectID != nil {
		if proj, err := timesheets.GetProject(r.Context(), d.Pool, p.OrgID, *doc.ProjectID); err == nil {
			pd.ProjectName = proj.Name
		}
	}
	return pd, true
}

func writeClientDocument(w http.ResponseWriter, r *http.Request, d module.Deps, p auth.Principal, pd clients.PortalDocument) {
	resp, err := toPortalDocument(r.Context(), d.Pool, p.OrgID, pd)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	if isToSign(pd.Document) {
		contact, err := core.GetContact(r.Context(), d.Pool, p.OrgID, *p.ContactID)
		if err != nil {
			module.WriteCoreError(w, err)
			return
		}
		resp.ConsentText = clients.ConsentText(contact.Name)
	}
	module.WriteJSON(w, http.StatusOK, resp)
}

func getClientDocument(w http.ResponseWriter, r *http.Request, d module.Deps, p auth.Principal) {
	if pd, ok := portalDocument(w, r, d, p); ok {
		writeClientDocument(w, r, d, p, pd)
	}
}

func serveClientDocument(w http.ResponseWriter, r *http.Request, d module.Deps, p auth.Principal, certificate bool) {
	pd, ok := portalDocument(w, r, d, p)
	if !ok {
		return
	}
	clients.ServeDocument(w, r, d, p.OrgID, pd.Document, certificate)
}

// maxUserAgent caps the stored user agent.
const maxUserAgent = 500

func signClientDocument(w http.ResponseWriter, r *http.Request, d module.Deps, p auth.Principal) {
	id, ok := module.PathID(r)
	if !ok {
		module.WriteError(w, http.StatusNotFound, "not found")
		return
	}
	var body struct {
		TypedName string `json:"typed_name"`
		Consent   bool   `json:"consent"`
	}
	if err := module.ReadJSON(r, &body); err != nil {
		module.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ctx := r.Context()
	var email string
	if err := d.Pool.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, p.UserID).Scan(&email); err != nil {
		module.WriteCoreError(w, err)
		return
	}
	ua := r.UserAgent()
	if len(ua) > maxUserAgent {
		ua = ua[:maxUserAgent]
	}
	loc := d.Cfg.Location
	if loc == nil {
		loc = time.UTC
	}
	doc, err := clients.Sign(ctx, d.Pool, d.Store, loc, p.OrgID, *p.ContactID, id, clients.SignInput{
		UserID: p.UserID, Email: email, TypedName: body.TypedName,
		IP: auth.ClientIP(r, d.Cfg.TrustedProxy), UserAgent: ua, Consent: body.Consent,
	})
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	notifySigned(ctx, d, p, r.PathValue("org"), doc)
	pd, ok := portalDocument(w, r, d, p)
	if ok {
		writeClientDocument(w, r, d, p, pd)
	}
}

// notifySigned emails staff that the client signed doc. Failures are
// logged; the signature stands.
func notifySigned(ctx context.Context, d module.Deps, p auth.Principal, orgSlug string, doc clients.Document) {
	if d.Mailer == nil {
		return
	}
	to, err := clients.SignedRecipients(ctx, d.Pool, p.OrgID, doc.CreatedBy)
	if err != nil {
		slog.Error("finding signed-document recipients failed", "document_id", doc.ID, "error", err)
		return
	}
	contact, err := core.GetContact(ctx, d.Pool, p.OrgID, *p.ContactID)
	if err != nil {
		slog.Error("loading signing contact failed", "document_id", doc.ID, "error", err)
		return
	}
	if err := clients.NotifySigned(ctx, d.Mailer, to, d.Cfg.BaseURL, orgSlug, contact.Name, doc.Title, contact.ID); err != nil {
		slog.Error("sending signed-document email failed", "document_id", doc.ID, "error", err)
	}
}
