package recon

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"shigoto/internal/auth"
	"shigoto/internal/core"
	"shigoto/internal/module"
	"shigoto/internal/reports"
)

var cashFlowNow = time.Now

type cashFlowMonth struct {
	Month    string `json:"month"`
	Incoming int64  `json:"incoming"`
	Outgoing int64  `json:"outgoing"`
}

type cashFlowResponse struct {
	From   string          `json:"from"`
	To     string          `json:"to"`
	Months []cashFlowMonth `json:"months"`
}

func cashFlowPeriod(today time.Time, fyStartMonth int, period string) (from, to time.Time, err error) {
	if period != "current" && period != "previous" {
		return from, to, fmt.Errorf("period must be current or previous")
	}
	endYear := today.Year()
	if fyStartMonth != 1 && int(today.Month()) >= fyStartMonth {
		endYear++
	}
	if period == "previous" {
		endYear--
	}
	return reports.FinancialYear(endYear, fyStartMonth)
}

func buildCashFlow(ctx context.Context, q core.Querier, orgID int64, from, to time.Time) (cashFlowResponse, error) {
	result := cashFlowResponse{From: from.Format(dateLayout), To: to.Format(dateLayout), Months: make([]cashFlowMonth, 12)}
	for i := range result.Months {
		result.Months[i].Month = from.AddDate(0, i, 0).Format(dateLayout)
	}
	rows, err := q.Query(ctx, `
		SELECT extract(month FROM posted_on)::int,
			coalesce(sum(amount) FILTER (WHERE amount > 0), 0),
			coalesce(sum(-amount) FILTER (WHERE amount < 0), 0)
		FROM bank_lines
		WHERE org_id = $1 AND posted_on BETWEEN $2 AND $3 AND status <> 'ignored'
		GROUP BY 1`, orgID, from, to)
	if err != nil {
		return cashFlowResponse{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var month int
		var incoming, outgoing int64
		if err := rows.Scan(&month, &incoming, &outgoing); err != nil {
			return cashFlowResponse{}, err
		}
		idx := (month - int(from.Month()) + 12) % 12
		result.Months[idx].Incoming = incoming
		result.Months[idx].Outgoing = outgoing
	}
	if err := rows.Err(); err != nil {
		return cashFlowResponse{}, err
	}
	return result, nil
}

func cashFlowHandler(w http.ResponseWriter, r *http.Request, d module.Deps) {
	period := "current"
	if values, exists := r.URL.Query()["period"]; exists {
		if len(values) != 1 || (values[0] != "current" && values[0] != "previous") {
			module.WriteError(w, http.StatusUnprocessableEntity, "period must be current or previous")
			return
		}
		period = values[0]
	}
	orgID := auth.From(r.Context()).OrgID
	fyMonth, err := reports.OrgFYStartMonth(r.Context(), d.Pool, orgID)
	if err == nil {
		loc := d.Cfg.Location
		if loc == nil {
			loc = time.UTC
		}
		var from, to time.Time
		from, to, err = cashFlowPeriod(cashFlowNow().In(loc), fyMonth, period)
		if err == nil {
			var body cashFlowResponse
			body, err = buildCashFlow(r.Context(), d.Pool, orgID, from, to)
			if err == nil {
				module.WriteJSON(w, http.StatusOK, body)
				return
			}
		}
	}
	slog.Error("cash flow failed", "error", err)
	module.WriteError(w, http.StatusInternalServerError, "internal error")
}
