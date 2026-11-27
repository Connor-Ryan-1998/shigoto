package httpapi

import (
	"net/http"

	"github.com/jackc/pgx/v5"

	"shigoto/internal/auth"
	"shigoto/internal/core"
)

// registerSettingsRoutes mounts the org settings routes. Unlike the
// modules in the modules registry, these are always on: they aren't gated
// by config.Config.Modules.
func registerSettingsRoutes(mux *http.ServeMux, d Deps) {
	mux.Handle("GET /api/v1/orgs/{org}/settings",
		d.Auth.RequireOrg(auth.PermInvoicesRead)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			getSettings(w, r, d)
		})))
	mux.Handle("PUT /api/v1/orgs/{org}/settings",
		d.Auth.RequireOrg(auth.PermOrgAdmin)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			putSettings(w, r, d)
		})))
}

// settingsRequest is also used as the response shape: a PUT replaces the
// whole settings object, so request and response mirror each other
// exactly.
type settingsRequest struct {
	Name            string `json:"name"`
	ABN             string `json:"abn"`
	GSTRegistered   bool   `json:"gst_registered"`
	BASFrequency    string `json:"bas_frequency"`
	BankBSB         string `json:"bank_bsb"`
	BankAccount     string `json:"bank_account"`
	PayID           string `json:"payid"`
	InvoiceLeadDays int    `json:"invoice_lead_days"`
	AutoSendRent    bool   `json:"auto_send_rent"`
	EmergencyPhone  string `json:"emergency_phone"`
	// Business profile.
	Address    string         `json:"address"`
	Phone      string         `json:"phone"`
	Email      string         `json:"email"`
	QuoteTerms string         `json:"quote_terms"`
	Licences   []core.Licence `json:"licences"`
	// HasLogo is response-only; a PUT ignores it (the logo has its own
	// routes).
	HasLogo bool `json:"has_logo"`
}

func settingsToResponse(s core.OrgSettings) settingsRequest {
	return settingsRequest{
		Name: s.Name, ABN: s.ABN, GSTRegistered: s.GSTRegistered, BASFrequency: s.BASFrequency,
		BankBSB: s.BankBSB, BankAccount: s.BankAccount, PayID: s.PayID,
		InvoiceLeadDays: s.InvoiceLeadDays, AutoSendRent: s.AutoSendRent, EmergencyPhone: s.EmergencyPhone,
		Address: s.Address, Phone: s.Phone, Email: s.Email, QuoteTerms: s.QuoteTerms,
		Licences: s.Licences, HasLogo: s.LogoFileID != nil,
	}
}

func settingsFromRequest(body settingsRequest) core.OrgSettings {
	return core.OrgSettings{
		Name: body.Name, ABN: body.ABN, GSTRegistered: body.GSTRegistered, BASFrequency: body.BASFrequency,
		BankBSB: body.BankBSB, BankAccount: body.BankAccount, PayID: body.PayID,
		InvoiceLeadDays: body.InvoiceLeadDays, AutoSendRent: body.AutoSendRent, EmergencyPhone: body.EmergencyPhone,
		Address: body.Address, Phone: body.Phone, Email: body.Email, QuoteTerms: body.QuoteTerms,
		Licences: body.Licences,
	}
}

func getSettings(w http.ResponseWriter, r *http.Request, d Deps) {
	p := auth.From(r.Context())
	s, err := core.GetSettings(r.Context(), d.Pool, p.OrgID)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settingsToResponse(s))
}

// putSettings handles PUT .../settings: it replaces the org's settings
// wholesale (empty strings clear optional fields) and audits the write.
func putSettings(w http.ResponseWriter, r *http.Request, d Deps) {
	var body settingsRequest
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	p := auth.From(r.Context())
	in := settingsFromRequest(body)

	var updated core.OrgSettings
	err := runInTx(r.Context(), d.Pool, func(tx pgx.Tx) error {
		var err error
		updated, err = core.UpdateSettings(r.Context(), tx, p.OrgID, in)
		if err != nil {
			return err
		}
		audit := body
		audit.BankBSB, audit.BankAccount, audit.PayID = "[REDACTED]", "[REDACTED]", "[REDACTED]"
		return core.Audit(r.Context(), tx, p.OrgID, &p.UserID, "org_settings", p.OrgID, "update", audit)
	})
	if err != nil {
		writeCoreError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, settingsToResponse(updated))
}
