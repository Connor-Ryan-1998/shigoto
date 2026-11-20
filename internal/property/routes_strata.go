package property

import (
	"net/http"

	"github.com/jackc/pgx/v5"

	"shigoto/internal/auth"
	"shigoto/internal/core"
	"shigoto/internal/module"
)

func registerStrataRoutes(mux *http.ServeMux, d module.Deps) {
	mux.Handle("GET /api/v1/orgs/{org}/strata-schemes",
		d.Auth.RequireOrg(auth.PermPropertyRead)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			listStrataSchemesHandler(w, r, d)
		})))
	mux.Handle("POST /api/v1/orgs/{org}/strata-schemes",
		d.Auth.RequireOrg(auth.PermPropertyWrite)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			createStrataSchemeHandler(w, r, d)
		})))
	mux.Handle("PUT /api/v1/orgs/{org}/strata-schemes/{id}",
		d.Auth.RequireOrg(auth.PermPropertyWrite)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			updateStrataSchemeHandler(w, r, d)
		})))
	mux.Handle("PUT /api/v1/orgs/{org}/properties/{id}/strata",
		d.Auth.RequireOrg(auth.PermPropertyWrite)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			setPropertyStrataHandler(w, r, d)
		})))
}

// strataSchemeRequest is the full-replacement body of POST and PUT
// /strata-schemes.
type strataSchemeRequest struct {
	Name          string `json:"name"`
	CTSNumber     string `json:"cts_number"`
	IsChairperson bool   `json:"is_chairperson"`
	IsSecretary   bool   `json:"is_secretary"`
	IsTreasurer   bool   `json:"is_treasurer"`
}

func (b strataSchemeRequest) scheme(id int64) StrataScheme {
	return StrataScheme{ID: id, Name: b.Name, CTSNumber: b.CTSNumber,
		IsChairperson: b.IsChairperson, IsSecretary: b.IsSecretary, IsTreasurer: b.IsTreasurer}
}

type strataPropertyResponse struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type strataSchemeResponse struct {
	ID            int64                    `json:"id"`
	Name          string                   `json:"name"`
	CTSNumber     string                   `json:"cts_number"`
	IsChairperson bool                     `json:"is_chairperson"`
	IsSecretary   bool                     `json:"is_secretary"`
	IsTreasurer   bool                     `json:"is_treasurer"`
	Properties    []strataPropertyResponse `json:"properties"`
}

func strataSchemeToResponse(s StrataScheme) strataSchemeResponse {
	resp := strataSchemeResponse{ID: s.ID, Name: s.Name, CTSNumber: s.CTSNumber,
		IsChairperson: s.IsChairperson, IsSecretary: s.IsSecretary, IsTreasurer: s.IsTreasurer,
		Properties: make([]strataPropertyResponse, len(s.Properties))}
	for i, p := range s.Properties {
		resp.Properties[i] = strataPropertyResponse{ID: p.ID, Name: p.Name}
	}
	return resp
}

func listStrataSchemesHandler(w http.ResponseWriter, r *http.Request, d module.Deps) {
	p := auth.From(r.Context())
	schemes, err := ListStrataSchemes(r.Context(), d.Pool, p.OrgID)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	resp := make([]strataSchemeResponse, len(schemes))
	for i, s := range schemes {
		resp[i] = strataSchemeToResponse(s)
	}
	module.WriteJSON(w, http.StatusOK, resp)
}

func createStrataSchemeHandler(w http.ResponseWriter, r *http.Request, d module.Deps) {
	var body strataSchemeRequest
	if err := module.ReadJSON(r, &body); err != nil {
		module.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	p := auth.From(r.Context())
	var created StrataScheme
	err := module.RunInTx(r.Context(), d.Pool, func(tx pgx.Tx) error {
		var err error
		if created, err = CreateStrataScheme(r.Context(), tx, p.OrgID, body.scheme(0)); err != nil {
			return err
		}
		return core.Audit(r.Context(), tx, p.OrgID, &p.UserID, "strata_scheme", created.ID, "create", body)
	})
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	module.WriteJSON(w, http.StatusCreated, strataSchemeToResponse(created))
}

func updateStrataSchemeHandler(w http.ResponseWriter, r *http.Request, d module.Deps) {
	id, ok := module.PathID(r)
	if !ok {
		module.WriteError(w, http.StatusNotFound, "not found")
		return
	}
	var body strataSchemeRequest
	if err := module.ReadJSON(r, &body); err != nil {
		module.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	p := auth.From(r.Context())
	var updated StrataScheme
	err := module.RunInTx(r.Context(), d.Pool, func(tx pgx.Tx) error {
		var err error
		if updated, err = UpdateStrataScheme(r.Context(), tx, p.OrgID, body.scheme(id)); err != nil {
			return err
		}
		return core.Audit(r.Context(), tx, p.OrgID, &p.UserID, "strata_scheme", id, "update", body)
	})
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	module.WriteJSON(w, http.StatusOK, strataSchemeToResponse(updated))
}

// propertyStrataResponse is PUT /properties/{id}/strata's body: the
// property's scheme after the change, or null when unlinked.
type propertyStrataResponse struct {
	StrataScheme *strataSchemeResponse `json:"strata_scheme"`
}

func setPropertyStrataHandler(w http.ResponseWriter, r *http.Request, d module.Deps) {
	id, ok := module.PathID(r)
	if !ok {
		module.WriteError(w, http.StatusNotFound, "not found")
		return
	}
	var body struct {
		StrataSchemeID *int64 `json:"strata_scheme_id"`
	}
	if err := module.ReadJSON(r, &body); err != nil {
		module.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	p := auth.From(r.Context())
	var resp propertyStrataResponse
	err := module.RunInTx(r.Context(), d.Pool, func(tx pgx.Tx) error {
		if err := SetPropertyStrataScheme(r.Context(), tx, p.OrgID, id, body.StrataSchemeID); err != nil {
			return err
		}
		if body.StrataSchemeID != nil {
			s, err := GetStrataScheme(r.Context(), tx, p.OrgID, *body.StrataSchemeID)
			if err != nil {
				return err
			}
			sr := strataSchemeToResponse(s)
			resp.StrataScheme = &sr
		}
		return core.Audit(r.Context(), tx, p.OrgID, &p.UserID, "property", id, "update_strata", body)
	})
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	module.WriteJSON(w, http.StatusOK, resp)
}
