package module

import (
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5"

	"shigoto/internal/auth"
	"shigoto/internal/core"
	"shigoto/internal/storage"
)

// SanitizeFilename strips characters that would break or hijack the
// Content-Disposition header (quotes, CR/LF header injection, and path
// separators, in case a client ever reads this value as a path), plus any
// other Unicode control rune (NUL, TAB, DEL/0x7f, etc.) that could confuse
// a header parser or terminal even without matching one of those specific
// cases. Falls back to "receipt" if nothing usable remains.
func SanitizeFilename(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r == '"' || r == '/' || r == '\\':
			continue
		case unicode.IsControl(r):
			continue
		default:
			b.WriteRune(r)
		}
	}
	s := strings.TrimSpace(b.String())
	if s == "" {
		return "receipt"
	}
	return s
}

// FileResponse is the JSON shape of an uploaded file.
type FileResponse struct {
	ID           int64  `json:"id"`
	MIME         string `json:"mime"`
	SizeBytes    int64  `json:"size_bytes"`
	OriginalName string `json:"original_name"`
}

// FileToResponse converts f to its JSON shape.
func FileToResponse(f core.File) FileResponse {
	return FileResponse{ID: f.ID, MIME: f.MIME, SizeBytes: f.SizeBytes, OriginalName: f.OriginalName}
}

// HandleUpload streams a multipart "file" upload into d.Store under pol.
// It refuses the upload with 507 when the store's free space is below
// Cfg.UploadMinFreeBytes, reads the body part by part (other fields before
// "file" are skipped) so nothing is spooled to memory or temp disk, and
// gives 413 when the file exceeds pol.MaxBytes - whether detected by
// SaveFile's own count or by the request-body cap. SaveFile, the audit row
// and onSaved (when non-nil) run in one transaction. On failure
// HandleUpload writes the error response itself and returns false; on
// success it returns the file without writing a response, so the caller
// chooses the status and body.
func HandleUpload(w http.ResponseWriter, r *http.Request, d Deps, pol core.UploadPolicy, onSaved func(tx pgx.Tx, f core.File) error) (core.File, bool) {
	if free, err := d.Store.FreeBytes(r.Context()); err != nil {
		WriteCoreError(w, fmt.Errorf("checking free space: %w", err))
		return core.File{}, false
	} else if free >= 0 && free < d.Cfg.UploadMinFreeBytes {
		WriteError(w, http.StatusInsufficientStorage, "insufficient storage")
		return core.File{}, false
	}
	if pol.MaxBytes > 0 {
		// Headroom for multipart framing and small fields; SaveFile
		// enforces the exact file limit.
		r.Body = http.MaxBytesReader(w, r.Body, pol.MaxBytes+1<<20)
	}
	mr, err := r.MultipartReader()
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid multipart body")
		return core.File{}, false
	}
	var part *multipart.Part
	for {
		part, err = mr.NextPart()
		if errors.Is(err, io.EOF) {
			WriteError(w, http.StatusUnprocessableEntity, "file is required")
			return core.File{}, false
		}
		if err != nil {
			if isTooLarge(err) {
				WriteError(w, http.StatusRequestEntityTooLarge, "file too large")
				return core.File{}, false
			}
			WriteError(w, http.StatusBadRequest, "invalid multipart body")
			return core.File{}, false
		}
		if part.FormName() == "file" {
			break
		}
		_ = part.Close()
	}
	defer part.Close()

	p := auth.From(r.Context())
	var created core.File
	err = RunInTx(r.Context(), d.Pool, func(tx pgx.Tx) error {
		var err error
		created, err = core.SaveFile(r.Context(), tx, d.Store, p.OrgID, part.FileName(), part, pol)
		if err != nil {
			return err
		}
		if err := core.Audit(r.Context(), tx, p.OrgID, &p.UserID, "file", created.ID, "create", map[string]any{
			"mime": created.MIME, "size_bytes": created.SizeBytes,
		}); err != nil {
			return err
		}
		if onSaved != nil {
			return onSaved(tx, created)
		}
		return nil
	})
	if err != nil {
		if isTooLarge(err) {
			WriteError(w, http.StatusRequestEntityTooLarge, "file too large")
			return core.File{}, false
		}
		WriteCoreError(w, err)
		return core.File{}, false
	}
	return created, true
}

func isTooLarge(err error) bool {
	var tooLarge *http.MaxBytesError
	return errors.Is(err, core.ErrTooLarge) || errors.As(err, &tooLarge)
}

// inlineMIME lists the media types ServeFile may show inline (in an
// <img>, <video> or <audio> element) rather than forcing a download.
var inlineMIME = core.MediaPolicy(0).Allowed

// ServeFile serves f's bytes with its stored (sniffed) MIME type, honouring
// HTTP Range requests (iOS Safari requires them for video playback). With
// inline true and a media MIME type the file is served inline; anything
// else is an attachment download under the sanitized original name.
// X-Content-Type-Options: nosniff stops a browser second-guessing the type,
// and Content-Security-Policy: sandbox neuters anything opened directly.
func ServeFile(w http.ResponseWriter, r *http.Request, store storage.Store, f core.File, inline bool) {
	blob, err := core.OpenFile(r.Context(), store, f)
	if err != nil {
		WriteCoreError(w, fmt.Errorf("opening stored file: %w", err))
		return
	}
	defer blob.Close()

	disposition := "attachment"
	if inline && inlineMIME[f.MIME] {
		disposition = "inline"
	}
	w.Header().Set("Content-Type", f.MIME)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox")
	w.Header().Set("Content-Disposition", disposition+`; filename="`+SanitizeFilename(f.OriginalName)+`"`)
	http.ServeContent(w, r, "", f.CreatedAt, blob)
}
