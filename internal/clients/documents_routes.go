package clients

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"

	"shigoto/internal/auth"
	"shigoto/internal/core"
	"shigoto/internal/module"
)

func registerDocumentRoutes(handle routeAdder, read, write func(http.Handler) http.Handler) {
	handle("GET /api/v1/orgs/{org}/contacts/{id}/documents", read, listDocumentsHandler)
	handle("POST /api/v1/orgs/{org}/contacts/{id}/documents", write, uploadDocumentHandler)
	handle("POST /api/v1/orgs/{org}/documents/{docId}/send", write, sendDocumentHandler)
	handle("POST /api/v1/orgs/{org}/documents/{docId}/withdraw", write, withdrawDocumentHandler)
	handle("POST /api/v1/orgs/{org}/documents/{docId}/replace", write, replaceDocumentHandler)
	handle("DELETE /api/v1/orgs/{org}/documents/{docId}", write, deleteDocumentHandler)
	handle("GET /api/v1/orgs/{org}/documents/{docId}/file", read, documentFileHandler)
	handle("GET /api/v1/orgs/{org}/documents/{docId}/certificate", read, documentCertificateHandler)
}

// DocumentResponse is the staff JSON shape of a document.
type DocumentResponse struct {
	ID           int64               `json:"id"`
	ContactID    int64               `json:"contact_id"`
	ProjectID    *int64              `json:"project_id"`
	Title        string              `json:"title"`
	Kind         string              `json:"kind"`
	Category     string              `json:"category"`
	Status       string              `json:"status"`
	SentAt       *string             `json:"sent_at"`
	CreatedAt    string              `json:"created_at"`
	SupersedesID *int64              `json:"supersedes_id"`
	File         module.FileResponse `json:"file"`
	Signature    *SignatureResponse  `json:"signature"`
}

// SignatureResponse is the JSON shape of a signature.
type SignatureResponse struct {
	TypedName string `json:"typed_name"`
	Email     string `json:"email"`
	SignedAt  string `json:"signed_at"`
}

// SignatureToResponse converts s (nil when unsigned) to its JSON shape.
func SignatureToResponse(s *Signature) *SignatureResponse {
	if s == nil {
		return nil
	}
	return &SignatureResponse{TypedName: s.TypedName, Email: s.Email, SignedAt: *formatTime(&s.SignedAt)}
}

func documentToResponse(d Document, f core.File) DocumentResponse {
	return DocumentResponse{
		ID: d.ID, ContactID: d.ContactID, ProjectID: d.ProjectID, Title: d.Title, Kind: d.Kind,
		Category: d.Category, Status: d.Status, SentAt: formatTime(d.SentAt),
		CreatedAt: *formatTime(&d.CreatedAt), SupersedesID: d.SupersedesID,
		File: module.FileToResponse(f), Signature: SignatureToResponse(d.Signature),
	}
}

func writeDocument(w http.ResponseWriter, r *http.Request, d module.Deps, status int, doc Document) {
	f, err := core.GetFile(r.Context(), d.Pool, auth.From(r.Context()).OrgID, doc.FileID)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	module.WriteJSON(w, status, documentToResponse(doc, f))
}

func listDocumentsHandler(w http.ResponseWriter, r *http.Request, d module.Deps) {
	contactID, ok := pathInt(r, "id")
	if !ok {
		module.WriteError(w, http.StatusNotFound, "not found")
		return
	}
	var projectID *int64
	if v := r.URL.Query().Get("project"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			module.WriteError(w, http.StatusUnprocessableEntity, "project must be an id")
			return
		}
		projectID = &id
	}
	p := auth.From(r.Context())
	docs, err := ListDocuments(r.Context(), d.Pool, p.OrgID, contactID, projectID)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	out := make([]DocumentResponse, 0, len(docs))
	for _, doc := range docs {
		f, err := core.GetFile(r.Context(), d.Pool, p.OrgID, doc.FileID)
		if err != nil {
			module.WriteCoreError(w, err)
			return
		}
		out = append(out, documentToResponse(doc, f))
	}
	module.WriteJSON(w, http.StatusOK, out)
}

// uploadDocumentHandler takes the document's metadata from the query string
// (title, kind, category, project_id) because module.HandleUpload streams
// the multipart body and skips every part except "file".
func uploadDocumentHandler(w http.ResponseWriter, r *http.Request, d module.Deps) {
	contactID, ok := pathInt(r, "id")
	if !ok {
		module.WriteError(w, http.StatusNotFound, "not found")
		return
	}
	q := r.URL.Query()
	nd := NewDocument{ContactID: contactID, Title: q.Get("title"), Kind: q.Get("kind"), Category: q.Get("category")}
	if v := q.Get("project_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			module.WriteError(w, http.StatusUnprocessableEntity, "project_id must be an id")
			return
		}
		nd.ProjectID = &id
	}
	// Validate before streaming so a bad request doesn't upload anything.
	if err := validateNewDocument(&nd); err != nil {
		module.WriteCoreError(w, err)
		return
	}
	p := auth.From(r.Context())
	if err := checkClientAndProject(r, d, p.OrgID, nd); err != nil {
		module.WriteCoreError(w, err)
		return
	}
	var created Document
	if _, ok := module.HandleUpload(w, r, d, PDFPolicy, func(tx pgx.Tx, f core.File) error {
		var err error
		created, err = CreateDocument(r.Context(), tx, p.OrgID, p.UserID, nd, f)
		return err
	}); !ok {
		return
	}
	writeDocument(w, r, d, http.StatusCreated, created)
}

func checkClientAndProject(r *http.Request, d module.Deps, orgID int64, nd NewDocument) error {
	if _, err := RequireClient(r.Context(), d.Pool, orgID, nd.ContactID); err != nil {
		return err
	}
	return checkProject(r.Context(), d.Pool, orgID, nd.ContactID, nd.ProjectID)
}

func docAction(w http.ResponseWriter, r *http.Request, d module.Deps, fn func(tx pgx.Tx, orgID, userID, id int64) (Document, error)) (Document, bool) {
	id, ok := pathInt(r, "docId")
	if !ok {
		module.WriteError(w, http.StatusNotFound, "not found")
		return Document{}, false
	}
	p := auth.From(r.Context())
	var doc Document
	err := module.RunInTx(r.Context(), d.Pool, func(tx pgx.Tx) error {
		var err error
		doc, err = fn(tx, p.OrgID, p.UserID, id)
		return err
	})
	if err != nil {
		module.WriteCoreError(w, err)
		return Document{}, false
	}
	return doc, true
}

func sendDocumentHandler(w http.ResponseWriter, r *http.Request, d module.Deps) {
	doc, ok := docAction(w, r, d, func(tx pgx.Tx, orgID, userID, id int64) (Document, error) {
		return SendDocument(r.Context(), tx, orgID, userID, id)
	})
	if !ok {
		return
	}
	if doc.Kind == KindSignature && d.Mailer != nil {
		p := auth.From(r.Context())
		if to, found, err := PortalLoginEmail(r.Context(), d.Pool, p.OrgID, doc.ContactID); err != nil {
			slog.Error("finding client portal email failed", "document_id", doc.ID, "error", err)
		} else if found {
			if err := NotifySent(r.Context(), d.Mailer, d.Cfg.BaseURL, r.PathValue("org"), to, doc.Title, doc.ID); err != nil {
				slog.Error("sending document email failed", "document_id", doc.ID, "error", err)
			}
		}
	}
	writeDocument(w, r, d, http.StatusOK, doc)
}

func withdrawDocumentHandler(w http.ResponseWriter, r *http.Request, d module.Deps) {
	doc, ok := docAction(w, r, d, func(tx pgx.Tx, orgID, userID, id int64) (Document, error) {
		return WithdrawDocument(r.Context(), tx, orgID, userID, id)
	})
	if ok {
		writeDocument(w, r, d, http.StatusOK, doc)
	}
}

func replaceDocumentHandler(w http.ResponseWriter, r *http.Request, d module.Deps) {
	id, ok := pathInt(r, "docId")
	if !ok {
		module.WriteError(w, http.StatusNotFound, "not found")
		return
	}
	p := auth.From(r.Context())
	existing, err := GetDocument(r.Context(), d.Pool, p.OrgID, id)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	switch existing.Status {
	case StatusSent:
	case StatusSigned:
		module.WriteCoreError(w, ErrSigned)
		return
	default:
		module.WriteCoreError(w, ErrBadState)
		return
	}
	var created Document
	if _, ok := module.HandleUpload(w, r, d, PDFPolicy, func(tx pgx.Tx, f core.File) error {
		var err error
		created, err = ReplaceDocument(r.Context(), tx, p.OrgID, p.UserID, id, f)
		return err
	}); !ok {
		return
	}
	writeDocument(w, r, d, http.StatusCreated, created)
}

func deleteDocumentHandler(w http.ResponseWriter, r *http.Request, d module.Deps) {
	id, ok := pathInt(r, "docId")
	if !ok {
		module.WriteError(w, http.StatusNotFound, "not found")
		return
	}
	p := auth.From(r.Context())
	if err := DeleteDraft(r.Context(), d.Pool, d.Store, p.OrgID, p.UserID, id); err != nil {
		module.WriteCoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func documentFileHandler(w http.ResponseWriter, r *http.Request, d module.Deps) {
	serveDocumentFile(w, r, d, false)
}

func documentCertificateHandler(w http.ResponseWriter, r *http.Request, d module.Deps) {
	serveDocumentFile(w, r, d, true)
}

func serveDocumentFile(w http.ResponseWriter, r *http.Request, d module.Deps, certificate bool) {
	id, ok := pathInt(r, "docId")
	if !ok {
		module.WriteError(w, http.StatusNotFound, "not found")
		return
	}
	p := auth.From(r.Context())
	doc, err := GetDocument(r.Context(), d.Pool, p.OrgID, id)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	ServeDocument(w, r, d, p.OrgID, doc, certificate)
}

// ServeDocument streams doc's file, or its signing certificate when
// certificate is true (404 when unsigned), as a download.
func ServeDocument(w http.ResponseWriter, r *http.Request, d module.Deps, orgID int64, doc Document, certificate bool) {
	fileID := doc.FileID
	if certificate {
		if doc.Signature == nil {
			module.WriteError(w, http.StatusNotFound, "not found")
			return
		}
		fileID = doc.Signature.CertificateFileID
	}
	f, err := core.GetFile(r.Context(), d.Pool, orgID, fileID)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	// Files are deduplicated by content, so the stored name may be the one
	// another client's copy was uploaded under: name the download after
	// this document instead.
	f.OriginalName = DocumentFileName(doc)
	if certificate {
		f.OriginalName = doc.Title + " - signing certificate.pdf"
	}
	module.ServeFile(w, r, d.Store, f, false)
}

// DocumentFileName is the file name a document is shown and downloaded
// under: its title, never the deduplicated file's stored name.
func DocumentFileName(doc Document) string { return doc.Title + ".pdf" }
