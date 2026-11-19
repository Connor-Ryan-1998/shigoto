package portal

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"shigoto/internal/auth"
	"shigoto/internal/core"
	"shigoto/internal/module"
	"shigoto/internal/property"
)

// registerRentRoutes mounts the tenant rent ledger: the lease account,
// payment receipts and statement downloads. Each is tenant-only and scoped
// to leases the caller's contact is listed on.
func registerRentRoutes(mux *http.ServeMux, d module.Deps) {
	tenant := func(h func(http.ResponseWriter, *http.Request, module.Deps, auth.Principal, int64)) http.Handler {
		return d.Auth.RequirePortal(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, leaseID, ok := requireLeaseTenant(w, r, d)
			if ok {
				h(w, r, d, p, leaseID)
			}
		}))
	}
	mux.Handle("GET /api/v1/portal/{org}/leases/{id}/account", tenant(getAccount))
	mux.Handle("GET /api/v1/portal/{org}/leases/{id}/payments/{paymentId}/receipt", tenant(getReceipt))
	for _, format := range []string{"csv", "pdf"} {
		mux.Handle("GET /api/v1/portal/{org}/leases/{id}/statement."+format, tenant(
			func(w http.ResponseWriter, r *http.Request, d module.Deps, p auth.Principal, leaseID int64) {
				property.ServeStatement(w, r, d.Pool, p.OrgID, leaseID, format, d.Cfg.Location)
			}))
	}
}

// requireLeaseTenant resolves the {id} lease for a tenant listed on it. It
// writes a bare 404 - for a client, a missing lease, a lease the caller is
// not on, or the property module being off - and returns ok false.
func requireLeaseTenant(w http.ResponseWriter, r *http.Request, d module.Deps) (auth.Principal, int64, bool) {
	p := auth.From(r.Context())
	leaseID, idOK := module.PathID(r)
	if !d.Cfg.Modules["property"] || p.Role != auth.Tenant || p.ContactID == nil || !idOK {
		module.WriteError(w, http.StatusNotFound, "not found")
		return p, 0, false
	}
	on, err := property.LeaseHasContact(r.Context(), d.Pool, p.OrgID, leaseID, *p.ContactID)
	if err != nil {
		module.WriteCoreError(w, err)
		return p, 0, false
	}
	if !on {
		module.WriteError(w, http.StatusNotFound, "not found")
		return p, 0, false
	}
	return p, leaseID, true
}

type accountLease struct {
	ID              int64   `json:"id"`
	PropertyAddress string  `json:"property_address"`
	StartDate       string  `json:"start_date"`
	EndDate         *string `json:"end_date"`
	Frequency       string  `json:"frequency"`
	RentAmount      int64   `json:"rent_amount"`
}

type nextDue struct {
	Date   string `json:"date"`
	Amount int64  `json:"amount"`
}

type accountSummary struct {
	PaidTo   *string  `json:"paid_to"`
	OwingNow int64    `json:"owing_now"`
	NextDue  *nextDue `json:"next_due"`
	Balance  int64    `json:"balance"`
}

type accountEntry struct {
	Date        string `json:"date"`
	Kind        string `json:"kind"`
	Description string `json:"description"`
	Debit       int64  `json:"debit"`
	Credit      int64  `json:"credit"`
	Balance     int64  `json:"balance"`
	PaymentID   *int64 `json:"payment_id,omitempty"`
}

type accountResponse struct {
	Lease   accountLease   `json:"lease"`
	Summary accountSummary `json:"summary"`
	Entries []accountEntry `json:"entries"`
}

func dateStr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format("2006-01-02")
	return &s
}

// paidTo is the end of the last rent period in an unbroken run of fully
// paid periods from the lease start, or the lease's recorded paid-to date
// when that is later.
func paidTo(lease property.Lease, ledger property.Ledger) *time.Time {
	var to *time.Time
	if lease.PaidToDate != nil {
		d := *lease.PaidToDate
		to = &d
	}
	for _, p := range ledger.Periods {
		if p.Paid < p.AmountDue {
			break
		}
		if to == nil || p.End.After(*to) {
			end := p.End
			to = &end
		}
	}
	return to
}

func getAccount(w http.ResponseWriter, r *http.Request, d module.Deps, p auth.Principal, leaseID int64) {
	ctx := r.Context()
	today := property.TodayIn(d.Cfg.Location)
	resp, err := buildAccount(ctx, d.Pool, p.OrgID, leaseID, today)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	module.WriteJSON(w, http.StatusOK, resp)
}

func buildAccount(ctx context.Context, q core.Querier, orgID, leaseID int64, today time.Time) (accountResponse, error) {
	lease, err := property.GetLease(ctx, q, orgID, leaseID)
	if err != nil {
		return accountResponse{}, err
	}
	prop, err := property.GetProperty(ctx, q, orgID, lease.PropertyID)
	if err != nil {
		return accountResponse{}, err
	}
	ledger, err := property.GetLedger(ctx, q, orgID, leaseID, today)
	if err != nil {
		return accountResponse{}, err
	}
	account, err := property.LeaseAccount(ctx, q, orgID, leaseID)
	if err != nil {
		return accountResponse{}, err
	}

	resp := accountResponse{
		Lease: accountLease{
			ID: lease.ID, PropertyAddress: prop.Address,
			StartDate: lease.StartDate.Format("2006-01-02"), EndDate: dateStr(lease.EndDate),
			Frequency: string(lease.Frequency), RentAmount: currentRent(lease, today),
		},
		Summary: accountSummary{
			PaidTo:   dateStr(paidTo(lease, ledger)),
			OwingNow: int64(ledger.Arrears),
			Balance:  int64(account.Balance),
		},
		Entries: make([]accountEntry, 0, len(account.Entries)),
	}
	if resp.Lease.PropertyAddress == "" {
		resp.Lease.PropertyAddress = prop.Name
	}
	if n := nextDuePeriod(ledger); n != nil {
		resp.Summary.NextDue = &nextDue{Date: n.Start, Amount: n.AmountDue}
	}
	// Newest first for the screen.
	for i := len(account.Entries) - 1; i >= 0; i-- {
		e := account.Entries[i]
		resp.Entries = append(resp.Entries, accountEntry{
			Date: e.Date.Format("2006-01-02"), Kind: string(e.Kind), Description: e.Description,
			Debit: int64(e.Debit), Credit: int64(e.Credit), Balance: int64(e.Balance),
			PaymentID: e.PaymentID,
		})
	}
	return resp, nil
}

type receiptPayment struct {
	ID        int64  `json:"id"`
	Amount    int64  `json:"amount"`
	PaidOn    string `json:"paid_on"`
	Method    string `json:"method"`
	Reference string `json:"reference"`
}

type receiptInvoice struct {
	ID     int64  `json:"id"`
	Number *int   `json:"number"`
	Total  int64  `json:"total"`
	Period string `json:"period"`
}

type receiptResponse struct {
	OrgName         string         `json:"org_name"`
	OrgABN          string         `json:"org_abn"`
	TenantNames     []string       `json:"tenant_names"`
	PropertyAddress string         `json:"property_address"`
	Payment         receiptPayment `json:"payment"`
	Invoice         receiptInvoice `json:"invoice"`
	BalanceAfter    int64          `json:"balance_after"`
	GeneratedAt     time.Time      `json:"generated_at"`
}

func getReceipt(w http.ResponseWriter, r *http.Request, d module.Deps, p auth.Principal, leaseID int64) {
	ctx := r.Context()
	paymentID, ok := pathID(r, "paymentId")
	if !ok {
		module.WriteError(w, http.StatusNotFound, "not found")
		return
	}
	// An all-time statement gives the header (org, tenants, address) and
	// every account entry; the payment must be one of this lease's.
	s, err := property.BuildStatement(ctx, d.Pool, p.OrgID, leaseID, time.Time{}, time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC), time.Now())
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	var entry *property.AccountEntry
	for i := range s.Entries {
		e := s.Entries[i]
		if e.Kind == property.EntryPayment && e.PaymentID != nil && *e.PaymentID == paymentID {
			entry = &e
		}
	}
	if entry == nil {
		module.WriteError(w, http.StatusNotFound, "not found")
		return
	}
	inv, err := core.GetInvoice(ctx, d.Pool, p.OrgID, entry.InvoiceID)
	if err != nil {
		module.WriteCoreError(w, err)
		return
	}
	period := ""
	for _, e := range s.Entries {
		if e.Kind == property.EntryCharge && e.InvoiceID == inv.ID {
			period = e.Description
		}
	}
	module.WriteJSON(w, http.StatusOK, receiptResponse{
		OrgName: s.OrgName, OrgABN: s.OrgABN, TenantNames: s.TenantNames, PropertyAddress: s.PropertyAddress,
		Payment: receiptPayment{
			ID: paymentID, Amount: int64(entry.Credit), PaidOn: entry.Date.Format("2006-01-02"),
			Method: entry.Method, Reference: inv.PaymentReference,
		},
		Invoice:      receiptInvoice{ID: inv.ID, Number: inv.Number, Total: int64(inv.Total), Period: period},
		BalanceAfter: int64(entry.Balance),
		GeneratedAt:  s.GeneratedAt,
	})
}

// pathID parses the named path value as a positive int64.
func pathID(r *http.Request, name string) (int64, bool) {
	v, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return v, err == nil && v > 0
}
