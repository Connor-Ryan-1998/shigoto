package property

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"shigoto/internal/core"
)

// EntryKind classifies a lease account entry.
type EntryKind string

const (
	// EntryCharge is an issued rent (or opening arrears) invoice: a debit.
	EntryCharge EntryKind = "charge"
	// EntryPayment is a payment against a lease invoice: a credit.
	EntryPayment EntryKind = "payment"
	// EntryWriteOff is the unpaid remainder of a written-off invoice: a
	// credit dated when it was written off.
	EntryWriteOff EntryKind = "write_off"
	// EntryWriteOffReversed is a debit that restores written-off debt when
	// a payment later recovers it, so the payment does not leave the
	// tenant in credit.
	EntryWriteOffReversed EntryKind = "write_off_reversed"
)

// kindOrder orders same-day entries: charges, then reversals (each just
// before the payment that recovers it), then payments, then write-offs.
var kindOrder = map[EntryKind]int{EntryCharge: 0, EntryWriteOffReversed: 1, EntryPayment: 2, EntryWriteOff: 3}

// AccountEntry is one line of a lease account. Balance is the running
// balance after this entry: positive is owed by the tenant, negative is
// in credit.
type AccountEntry struct {
	Date        time.Time
	Kind        EntryKind
	Description string
	Debit       core.Cents
	Credit      core.Cents
	Balance     core.Cents
	InvoiceID   int64
	PaymentID   *int64
	// Method is the payment method (payments only).
	Method string
}

// Account is a lease's running account, oldest entry first.
type Account struct {
	Entries []AccountEntry
	Balance core.Cents
}

// methodLabels are the tenant-facing names of payment methods.
var methodLabels = map[string]string{
	"stripe":        "Card",
	"bank_transfer": "Bank transfer",
	"payid":         "PayID",
	"cash":          "Cash",
	"other":         "Other",
}

// MethodLabel returns the tenant-facing name of payment method m.
func MethodLabel(m string) string {
	if l, ok := methodLabels[m]; ok {
		return l
	}
	return "Other"
}

type accountInvoice struct {
	id               int64
	total            int64
	date             time.Time
	periodStart      *time.Time
	periodEnd        *time.Time
	charges          []string
	writtenOffOn     *time.Time
	writtenOffAmount int64
}

// LeaseAccount builds leaseID's account: every issued (open, paid or
// uncollectible) invoice raised for its rent periods or opening arrears as
// a charge on its due date, every payment against those invoices on its
// paid date, and write-offs (and their reversal by later payments). Draft,
// void and bond invoices are excluded.
func LeaseAccount(ctx context.Context, q core.Querier, orgID, leaseID int64) (Account, error) {
	rows, err := q.Query(ctx, `
		SELECT i.id, i.total, coalesce(i.due_date, i.issue_date), rp.first_start, rp.last_end,
			i.written_off_on, i.written_off_amount,
			coalesce((SELECT array_agg(l.description ORDER BY l.id) FROM invoice_lines l
				WHERE l.org_id = i.org_id AND l.invoice_id = i.id
				AND l.description NOT LIKE 'Rent %' AND l.description <> 'Opening arrears'), '{}')
		FROM invoices i
		LEFT JOIN LATERAL (
			SELECT min(period_start) AS first_start, max(period_end) AS last_end
			FROM rent_periods WHERE org_id = $1 AND lease_id = $2 AND invoice_id = i.id
		) rp ON true
		WHERE i.org_id = $1 AND i.status IN ('open', 'paid', 'uncollectible')
		AND (
			i.id IN (SELECT invoice_id FROM rent_periods WHERE org_id = $1 AND lease_id = $2 AND invoice_id IS NOT NULL)
			OR i.id = (SELECT opening_arrears_invoice_id FROM leases WHERE org_id = $1 AND id = $2)
		)`, orgID, leaseID)
	if err != nil {
		return Account{}, err
	}
	var invoices []accountInvoice
	for rows.Next() {
		var inv accountInvoice
		if err := rows.Scan(&inv.id, &inv.total, &inv.date, &inv.periodStart, &inv.periodEnd,
			&inv.writtenOffOn, &inv.writtenOffAmount, &inv.charges); err != nil {
			rows.Close()
			return Account{}, err
		}
		invoices = append(invoices, inv)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Account{}, err
	}
	if len(invoices) == 0 {
		return Account{}, nil
	}

	ids := make([]int64, len(invoices))
	for i, inv := range invoices {
		ids[i] = inv.id
	}
	type payment struct {
		id, invoiceID, amount int64
		paidOn                time.Time
		method                string
	}
	payRows, err := q.Query(ctx, `
		SELECT id, invoice_id, amount, paid_on, coalesce(method, 'other')
		FROM payments WHERE org_id = $1 AND invoice_id = ANY($2)
		ORDER BY paid_on, id`, orgID, ids)
	if err != nil {
		return Account{}, err
	}
	paymentsByInvoice := map[int64][]payment{}
	for payRows.Next() {
		var p payment
		if err := payRows.Scan(&p.id, &p.invoiceID, &p.amount, &p.paidOn, &p.method); err != nil {
			payRows.Close()
			return Account{}, err
		}
		paymentsByInvoice[p.invoiceID] = append(paymentsByInvoice[p.invoiceID], p)
	}
	payRows.Close()
	if err := payRows.Err(); err != nil {
		return Account{}, err
	}

	var entries []AccountEntry
	for _, inv := range invoices {
		entries = append(entries, AccountEntry{
			Date: inv.date, Kind: EntryCharge, Description: chargeDescription(inv),
			Debit: core.Cents(inv.total), InvoiceID: inv.id,
		})
		recoverable := int64(0)
		if inv.writtenOffOn != nil && inv.writtenOffAmount > 0 {
			recoverable = inv.writtenOffAmount
			entries = append(entries, AccountEntry{
				Date: *inv.writtenOffOn, Kind: EntryWriteOff, Description: "Written off",
				Credit: core.Cents(inv.writtenOffAmount), InvoiceID: inv.id,
			})
		}
		for _, p := range paymentsByInvoice[inv.id] {
			id := p.id
			if recoverable > 0 && !p.paidOn.Before(*inv.writtenOffOn) {
				recovered := min(recoverable, p.amount)
				recoverable -= recovered
				entries = append(entries, AccountEntry{
					Date: p.paidOn, Kind: EntryWriteOffReversed, Description: "Write-off reversed",
					Debit: core.Cents(recovered), InvoiceID: inv.id, PaymentID: &id,
				})
			}
			entries = append(entries, AccountEntry{
				Date: p.paidOn, Kind: EntryPayment, Description: "Payment · " + MethodLabel(p.method),
				Credit: core.Cents(p.amount), InvoiceID: inv.id, PaymentID: &id, Method: p.method,
			})
		}
	}

	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if !a.Date.Equal(b.Date) {
			return a.Date.Before(b.Date)
		}
		if kindOrder[a.Kind] != kindOrder[b.Kind] {
			return kindOrder[a.Kind] < kindOrder[b.Kind]
		}
		if a.InvoiceID != b.InvoiceID {
			return a.InvoiceID < b.InvoiceID
		}
		return paymentID(a) < paymentID(b)
	})

	var balance core.Cents
	for i := range entries {
		balance += entries[i].Debit - entries[i].Credit
		entries[i].Balance = balance
	}
	return Account{Entries: entries, Balance: balance}, nil
}

func paymentID(e AccountEntry) int64 {
	if e.PaymentID == nil {
		return 0
	}
	return *e.PaymentID
}

// chargeDescription is "Rent 5–11 Nov 2026" (plus " + <charge>" for each
// non-rent line) for a rent invoice, or "Opening arrears".
func chargeDescription(inv accountInvoice) string {
	if inv.periodStart == nil || inv.periodEnd == nil {
		return "Opening arrears"
	}
	d := "Rent " + DateRangeLabel(*inv.periodStart, *inv.periodEnd)
	for _, c := range inv.charges {
		d += " + " + c
	}
	return d
}

// DateRangeLabel formats an inclusive date range compactly: "5–11 Nov
// 2026", "28 Nov – 4 Dec 2026" or "29 Dec 2026 – 4 Jan 2027".
func DateRangeLabel(start, end time.Time) string {
	switch {
	case start.Year() != end.Year():
		return start.Format("2 Jan 2006") + " – " + end.Format("2 Jan 2006")
	case start.Month() != end.Month():
		return start.Format("2 Jan") + " – " + end.Format("2 Jan 2006")
	default:
		return fmt.Sprintf("%d–%s", start.Day(), end.Format("2 Jan 2006"))
	}
}

// LeaseHasContact reports whether contactID is one of leaseID's tenants in
// orgID. Portal lease routes use it to scope a tenant to leases they are on.
func LeaseHasContact(ctx context.Context, q core.Querier, orgID, leaseID, contactID int64) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM lease_tenants lt JOIN leases l ON l.id = lt.lease_id
			WHERE l.org_id = $1 AND l.id = $2 AND lt.contact_id = $3
		)`, orgID, leaseID, contactID).Scan(&ok)
	return ok, err
}

// Statement is a lease account for an inclusive date range, with the
// header details a printed or exported statement needs.
type Statement struct {
	Lease           Lease
	PropertyAddress string
	TenantNames     []string
	OrgName         string
	OrgABN          string
	From, To        time.Time
	// Opening is the balance before From; Closing the balance at To.
	Opening, Closing          core.Cents
	TotalDebits, TotalCredits core.Cents
	// Entries are those dated From..To inclusive, oldest first.
	Entries     []AccountEntry
	GeneratedAt time.Time
}

// BuildStatement builds leaseID's statement for from..to (inclusive
// calendar dates), stamped as generated at now.
func BuildStatement(ctx context.Context, q core.Querier, orgID, leaseID int64, from, to, now time.Time) (Statement, error) {
	lease, err := GetLease(ctx, q, orgID, leaseID)
	if err != nil {
		return Statement{}, err
	}
	prop, err := GetProperty(ctx, q, orgID, lease.PropertyID)
	if err != nil {
		return Statement{}, err
	}
	s := Statement{Lease: lease, PropertyAddress: prop.Address, From: from, To: to, GeneratedAt: now}
	if s.PropertyAddress == "" {
		s.PropertyAddress = prop.Name
	}
	var abn *string
	if err := q.QueryRow(ctx, `SELECT name, abn FROM orgs WHERE id = $1`, orgID).Scan(&s.OrgName, &abn); err != nil {
		return Statement{}, err
	}
	if abn != nil {
		s.OrgABN = strings.TrimSpace(*abn)
	}
	names, err := q.Query(ctx, `
		SELECT c.name FROM lease_tenants lt JOIN contacts c ON c.id = lt.contact_id
		WHERE lt.lease_id = $1 AND c.org_id = $2
		ORDER BY lt.is_primary DESC, c.name`, leaseID, orgID)
	if err != nil {
		return Statement{}, err
	}
	for names.Next() {
		var n string
		if err := names.Scan(&n); err != nil {
			names.Close()
			return Statement{}, err
		}
		s.TenantNames = append(s.TenantNames, n)
	}
	names.Close()
	if err := names.Err(); err != nil {
		return Statement{}, err
	}

	account, err := LeaseAccount(ctx, q, orgID, leaseID)
	if err != nil {
		return Statement{}, err
	}
	for _, e := range account.Entries {
		switch {
		case e.Date.Before(from):
			s.Opening = e.Balance
		case !e.Date.After(to):
			s.Entries = append(s.Entries, e)
			s.TotalDebits += e.Debit
			s.TotalCredits += e.Credit
		}
	}
	s.Closing = s.Opening + s.TotalDebits - s.TotalCredits
	return s, nil
}
