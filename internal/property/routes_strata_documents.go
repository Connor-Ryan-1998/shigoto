package property

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"shigoto/internal/auth"
	"shigoto/internal/core"
	"shigoto/internal/module"
)

// strataPolicy accepts StrataMax PDFs up to 25 MiB.
var strataPolicy = core.UploadPolicy{MaxBytes: 25 << 20, Allowed: map[string]bool{"application/pdf": true}}

const errNoOfficerPosition = "the organisation holds no chairperson or treasurer position on this scheme"

func registerStrataDocumentRoutes(mux *http.ServeMux, d module.Deps) {
	handle := func(pattern string, perm auth.Perm, fn func(http.ResponseWriter, *http.Request, module.Deps, StrataScheme)) {
		mux.Handle(pattern, d.Auth.RequireOrg(perm)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scheme, ok := requireStrataOfficer(w, r, d)
			if !ok {
				return
			}
			fn(w, r, d, scheme)
		})))
	}
	const base = "/api/v1/orgs/{org}/strata-schemes/{id}/documents"
	handle("GET "+base, auth.PermPropertyRead, listStrataDocumentsHandler)
	handle("POST "+base, auth.PermPropertyWrite, uploadStrataDocumentHandler)
	handle("GET "+base+"/{doc}/file", auth.PermPropertyRead, downloadStrataDocumentHandler)
	handle("POST "+base+"/{doc}/reindex", auth.PermPropertyWrite, reindexStrataDocumentHandler)
	handle("DELETE "+base+"/{doc}", auth.PermPropertyWrite, deleteStrataDocumentHandler)
}

// requireStrataOfficer loads path scheme {id} of the request's org (404
// when absent) and requires the org to hold chairperson or treasurer on it
// (403 otherwise). On failure it writes the response and returns false.
func requireStrataOfficer(w http.ResponseWriter, r *http.Request, d module.Deps) (StrataScheme, bool) {
	id, ok := module.PathID(r)
	if !ok {
		module.WriteError(w, http.StatusNotFound, "not found")
		return StrataScheme{}, false
	}
	scheme, err := GetStrataScheme(r.Context(), d.Pool, auth.From(r.Context()).OrgID, id)
	if err != nil {
		module.WriteCoreError(w, err)
		return StrataScheme{}, false
	}
	if !scheme.IsChairperson && !scheme.IsTreasurer {
		module.WriteError(w, http.StatusForbidden, errNoOfficerPosition)
		return StrataScheme{}, false
	}
	return scheme, true
}

type strataDocumentResponse struct {
	ID             int64  `json:"id"`
	DocType        string `json:"doc_type"`
	Title          string `json:"title"`
	DocumentDate   string `json:"document_date"`
	Status         string `json:"status"`
	Error          string `json:"error"`
	PageCount      int    `json:"page_count"`
	ExtractModel   string `json:"extract_model"`
	ChunksTotal    int    `json:"chunks_total"`
	ChunksEmbedded int    `json:"chunks_embedded"`
}

func strataDocumentToResponse(doc StrataDocument) strataDocumentResponse {
	return strataDocumentResponse{
		ID: doc.ID, DocType: string(doc.DocType), Title: doc.Title,
		DocumentDate: doc.DocumentDate.Format(time.DateOnly), Status: string(doc.Status), Error: doc.Error,
		PageCount: doc.PageCount, ExtractModel: doc.ExtractModel,
		ChunksTotal: doc.ChunksTotal, ChunksEmbedded: doc.ChunksEmbedded,
	}
}

func embedModel(d module.Deps) string {
	if d.LLM == nil {
		return ""
	}
	return d.LLM.EmbedModel()
}

func listStrataDocumentsHandler(w http.ResponseWriter, r *http.Request, d module.Deps, scheme StrataScheme) {
	docs, err := ListStrataDocuments(r.Context(), d.Pool, scheme.OrgID, scheme.ID, embedModel(d))
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	out := make([]strataDocumentResponse, len(docs))
	for i, doc := range docs {
		out[i] = strataDocumentToResponse(doc)
	}
	module.WriteJSON(w, http.StatusOK, out)
}

// strataUploadMeta reads the document's metadata from the query string
// (the multipart body is streamed straight to storage, so its other fields
// are not available before the file).
func strataUploadMeta(r *http.Request) (StrataDocument, error) {
	q := r.URL.Query()
	doc := StrataDocument{DocType: StrataDocType(q.Get("doc_type")), Title: q.Get("title")}
	if !doc.DocType.Valid() {
		return doc, fmt.Errorf("%w: unknown doc_type %q", core.ErrInvalidInput, q.Get("doc_type"))
	}
	raw := q.Get("document_date")
	if raw == "" {
		return doc, fmt.Errorf("%w: document_date is required", core.ErrInvalidInput)
	}
	date, err := time.Parse(time.DateOnly, raw)
	if err != nil {
		return doc, fmt.Errorf("%w: document_date must be YYYY-MM-DD", core.ErrInvalidInput)
	}
	doc.DocumentDate = date
	// Validate title, type and date now, before streaming the upload; the
	// page count is checked once the file is stored.
	doc.PageCount = 1
	if _, err := validateStrataDocument(doc); err != nil {
		return doc, err
	}
	return doc, nil
}

func uploadStrataDocumentHandler(w http.ResponseWriter, r *http.Request, d module.Deps, scheme StrataScheme) {
	meta, err := strataUploadMeta(r)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	f, ok := module.HandleUpload(w, r, d, strataPolicy, nil)
	if !ok {
		return
	}
	// From here a failure must not leave an unreferenced file behind.
	discard := func() {
		if err := deleteFileIfUnreferenced(context.WithoutCancel(r.Context()), d.Pool, d.Store, f.ID); err != nil {
			slog.Error("discarding rejected strata upload", "file_id", f.ID, "error", err)
		}
	}
	pages, err := storedPDFPageCount(r.Context(), d, f)
	if err != nil {
		discard()
		module.WriteCoreError(w, err)
		return
	}
	meta.FileID, meta.PageCount = f.ID, pages
	p := auth.From(r.Context())
	var created StrataDocument
	err = module.RunInTx(r.Context(), d.Pool, func(tx pgx.Tx) error {
		var err error
		if created, err = CreateStrataDocument(r.Context(), tx, scheme.OrgID, scheme.ID, meta); err != nil {
			return err
		}
		return core.Audit(r.Context(), tx, p.OrgID, &p.UserID, "strata_document", created.ID, "create", map[string]any{
			"scheme_id": scheme.ID, "file_id": f.ID, "doc_type": created.DocType, "title": created.Title,
		})
	})
	if err != nil {
		discard()
		module.WriteCoreError(w, err)
		return
	}
	if d.StrataKick != nil {
		d.StrataKick()
	}
	module.WriteJSON(w, http.StatusCreated, strataDocumentToResponse(created))
}

func storedPDFPageCount(ctx context.Context, d module.Deps, f core.File) (int, error) {
	blob, err := core.OpenFile(ctx, d.Store, f)
	if err != nil {
		return 0, fmt.Errorf("opening stored upload: %w", err)
	}
	defer blob.Close()
	pdf, err := io.ReadAll(io.LimitReader(blob, strataPolicy.MaxBytes+1))
	if err != nil {
		return 0, fmt.Errorf("reading stored upload: %w", err)
	}
	return PDFPageCount(pdf)
}

func pathDocument(w http.ResponseWriter, r *http.Request, d module.Deps, scheme StrataScheme) (StrataDocument, bool) {
	docID, ok := pathInt64(r, "doc")
	if !ok {
		module.WriteError(w, http.StatusNotFound, "not found")
		return StrataDocument{}, false
	}
	doc, err := GetStrataDocument(r.Context(), d.Pool, scheme.OrgID, scheme.ID, docID)
	if err != nil {
		module.WriteCoreError(w, err)
		return StrataDocument{}, false
	}
	return doc, true
}

func downloadStrataDocumentHandler(w http.ResponseWriter, r *http.Request, d module.Deps, scheme StrataScheme) {
	doc, ok := pathDocument(w, r, d, scheme)
	if !ok {
		return
	}
	f, err := core.GetFile(r.Context(), d.Pool, scheme.OrgID, doc.FileID)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	module.ServeFile(w, r, d.Store, f, false)
}

func reindexStrataDocumentHandler(w http.ResponseWriter, r *http.Request, d module.Deps, scheme StrataScheme) {
	doc, ok := pathDocument(w, r, d, scheme)
	if !ok {
		return
	}
	p := auth.From(r.Context())
	err := module.RunInTx(r.Context(), d.Pool, func(tx pgx.Tx) error {
		if err := ReindexStrataDocument(r.Context(), tx, scheme.OrgID, scheme.ID, doc.ID); err != nil {
			return err
		}
		return core.Audit(r.Context(), tx, p.OrgID, &p.UserID, "strata_document", doc.ID, "reindex", nil)
	})
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	if d.StrataKick != nil {
		d.StrataKick()
	}
	w.WriteHeader(http.StatusNoContent)
}

func deleteStrataDocumentHandler(w http.ResponseWriter, r *http.Request, d module.Deps, scheme StrataScheme) {
	doc, ok := pathDocument(w, r, d, scheme)
	if !ok {
		return
	}
	p := auth.From(r.Context())
	if err := core.Audit(r.Context(), d.Pool, p.OrgID, &p.UserID, "strata_document", doc.ID, "delete", map[string]any{
		"scheme_id": scheme.ID, "title": doc.Title,
	}); err != nil {
		module.WriteCoreError(w, err)
		return
	}
	if err := DeleteStrataDocument(r.Context(), d.Pool, d.Store, scheme.OrgID, scheme.ID, doc.ID); err != nil {
		if !errors.Is(err, core.ErrNotFound) {
			module.WriteCoreError(w, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
