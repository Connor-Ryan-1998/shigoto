package property

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"time"

	"shigoto/internal/auth"
	"shigoto/internal/config"
	"shigoto/internal/core"
	"shigoto/internal/module"
)

// maxStatementYears caps a statement's date range.
const maxStatementYears = 5

// TodayIn returns today's calendar date in loc (UTC when nil) as UTC
// midnight, the form the property module's date maths uses.
func TodayIn(loc *time.Location) time.Time {
	return today(module.Deps{Cfg: config.Config{Location: loc}})
}

// LeaseActive reports whether l is running on today: started, and not
// terminated before today. (end_date alone doesn't stop a lease: it rolls
// on periodically until terminated.)
func LeaseActive(l Lease, today time.Time) bool {
	if l.StartDate.After(today) {
		return false
	}
	return l.TerminationDate == nil || !l.TerminationDate.Before(today)
}

// StatementRange reads the from/to query parameters (YYYY-MM-DD, inclusive)
// of r, defaulting each to the org's current financial year. It returns
// core.ErrInvalidInput for a malformed date, from after to, or a range over
// five years.
func StatementRange(ctx context.Context, q core.Querier, orgID int64, r *http.Request, today time.Time) (from, to time.Time, err error) {
	var fyStart int
	if err := q.QueryRow(ctx, `SELECT fy_start_month FROM orgs WHERE id = $1`, orgID).Scan(&fyStart); err != nil {
		return time.Time{}, time.Time{}, err
	}
	from, to = FYRange(fyStart, today)
	for _, p := range []struct {
		name string
		dst  *time.Time
	}{{"from", &from}, {"to", &to}} {
		v := r.URL.Query().Get(p.name)
		if v == "" {
			continue
		}
		d, perr := time.Parse(dateLayout, v)
		if perr != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("%w: %s must be YYYY-MM-DD", core.ErrInvalidInput, p.name)
		}
		*p.dst = d
	}
	if from.After(to) {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: from must not be after to", core.ErrInvalidInput)
	}
	if to.After(from.AddDate(maxStatementYears, 0, 0)) {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: range must be at most %d years", core.ErrInvalidInput, maxStatementYears)
	}
	return from, to, nil
}

// ServeStatement writes leaseID's statement for the request's range as
// format "csv" or "pdf", as an uncached attachment. The caller has already
// checked the caller may see leaseID.
func ServeStatement(w http.ResponseWriter, r *http.Request, q core.Querier, orgID, leaseID int64, format string, loc *time.Location) {
	if loc == nil {
		loc = time.UTC
	}
	from, to, err := StatementRange(r.Context(), q, orgID, r, TodayIn(loc))
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	s, err := BuildStatement(r.Context(), q, orgID, leaseID, from, to, time.Now())
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	var buf bytes.Buffer
	contentType := "text/csv; charset=utf-8"
	if format == "pdf" {
		contentType = "application/pdf"
		err = WriteStatementPDF(&buf, s, loc)
	} else {
		err = WriteStatementCSV(&buf, s, loc)
	}
	if err != nil {
		module.WriteCoreError(w, fmt.Errorf("rendering statement: %w", err))
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", `attachment; filename="`+module.SanitizeFilename(StatementFilename(s, format))+`"`)
	_, _ = w.Write(buf.Bytes())
}

// registerStatementRoutes adds the staff statement downloads.
func registerStatementRoutes(mux *http.ServeMux, d module.Deps) {
	for _, format := range []string{"csv", "pdf"} {
		mux.Handle("GET /api/v1/orgs/{org}/leases/{id}/statement."+format, d.Auth.RequireOrg(auth.PermPropertyRead)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, ok := module.PathID(r)
			if !ok {
				module.WriteError(w, http.StatusNotFound, "not found")
				return
			}
			ServeStatement(w, r, d.Pool, auth.From(r.Context()).OrgID, id, format, d.Cfg.Location)
		})))
	}
}
