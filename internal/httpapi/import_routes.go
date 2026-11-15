package httpapi

import (
	"context"
	"net/http"

	"shigoto/internal/auth"
	"shigoto/internal/billing"
	"shigoto/internal/core"
	"shigoto/internal/importers"
	"shigoto/internal/module"
)

// registerImportRoutes mounts the owner-only invoice CSV import: a preview
// that writes nothing and an all-or-nothing apply.
func registerImportRoutes(mux *http.ServeMux, d Deps) {
	mux.Handle("POST /api/v1/orgs/{org}/invoices/import/preview",
		d.Auth.RequireOrg(auth.PermImport)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			importInvoices(w, r, d, false)
		})))
	mux.Handle("POST /api/v1/orgs/{org}/invoices/import",
		d.Auth.RequireOrg(auth.PermImport)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			importInvoices(w, r, d, true)
		})))
}

func importInvoices(w http.ResponseWriter, r *http.Request, d Deps, apply bool) {
	p := auth.From(r.Context())
	module.ServeImport(w, r, d.Pool, p.OrgID, billing.InvoiceImportFields, apply,
		func(ctx context.Context, q core.Querier, rows []importers.Row, dateFormat string, apply bool) (any, []importers.Problem, error) {
			parsed, parseProblems := billing.ParseInvoiceRows(rows, dateFormat)
			res, err := billing.ImportInvoices(ctx, q, p.OrgID, &p.UserID, parsed, apply && len(parseProblems) == 0)
			if err != nil {
				return nil, nil, err
			}
			res.Problems = module.MergeProblems(parseProblems, res.Problems)
			return res, res.Problems, nil
		})
}
