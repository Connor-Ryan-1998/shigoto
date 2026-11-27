package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"

	"shigoto/internal/auth"
	"shigoto/internal/core"
	"shigoto/internal/module"
	"shigoto/internal/property"
)

// registerLogoRoutes mounts the org logo routes: upload/replace and remove
// (org admins), and download (anyone who can read settings).
func registerLogoRoutes(mux *http.ServeMux, d module.Deps) {
	mux.Handle("POST /api/v1/orgs/{org}/settings/logo",
		d.Auth.RequireOrg(auth.PermOrgAdmin)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			uploadLogo(w, r, d)
		})))
	mux.Handle("DELETE /api/v1/orgs/{org}/settings/logo",
		d.Auth.RequireOrg(auth.PermOrgAdmin)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			deleteLogo(w, r, d)
		})))
	mux.Handle("GET /api/v1/orgs/{org}/settings/logo",
		d.Auth.RequireOrg(auth.PermInvoicesRead)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			getLogo(w, r, d)
		})))
}

func uploadLogo(w http.ResponseWriter, r *http.Request, d module.Deps) {
	p := auth.From(r.Context())
	var old *int64
	f, ok := module.HandleUpload(w, r, d, core.LogoPolicy, func(tx pgx.Tx, f core.File) error {
		var err error
		old, err = core.SetOrgLogo(r.Context(), tx, p.OrgID, &f.ID)
		return err
	})
	if !ok {
		return
	}
	dropOldLogo(r, d, old, f.ID)
	s, err := core.GetSettings(r.Context(), d.Pool, p.OrgID)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	module.WriteJSON(w, http.StatusOK, settingsToResponse(s))
}

func deleteLogo(w http.ResponseWriter, r *http.Request, d module.Deps) {
	p := auth.From(r.Context())
	var old *int64
	err := module.RunInTx(r.Context(), d.Pool, func(tx pgx.Tx) error {
		var err error
		if old, err = core.SetOrgLogo(r.Context(), tx, p.OrgID, nil); err != nil {
			return err
		}
		return core.Audit(r.Context(), tx, p.OrgID, &p.UserID, "org_settings", p.OrgID, "remove_logo", nil)
	})
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	dropOldLogo(r, d, old, 0)
	w.WriteHeader(http.StatusNoContent)
}

// dropOldLogo deletes the previous logo file once nothing references it.
// A failure only leaks a file, so it is logged rather than returned.
func dropOldLogo(r *http.Request, d module.Deps, old *int64, current int64) {
	if old == nil || *old == current {
		return
	}
	if err := property.DeleteFileIfUnreferenced(r.Context(), d.Pool, d.Store, *old); err != nil {
		slog.Error("deleting old logo failed", "file_id", *old, "error", err)
	}
}

func getLogo(w http.ResponseWriter, r *http.Request, d module.Deps) {
	p := auth.From(r.Context())
	s, err := core.GetSettings(r.Context(), d.Pool, p.OrgID)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	if s.LogoFileID == nil {
		module.WriteError(w, http.StatusNotFound, "not found")
		return
	}
	f, err := core.GetFile(r.Context(), d.Pool, p.OrgID, *s.LogoFileID)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	module.ServeFile(w, r, d.Store, f, true)
}
