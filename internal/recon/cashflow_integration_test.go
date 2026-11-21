//go:build integration

package recon_test

import (
	"encoding/json"
	"testing"
	"time"

	"shigoto/internal/auth"
	"shigoto/internal/config"
	"shigoto/internal/httpapi"
	"shigoto/internal/recon"
	"shigoto/internal/testkit"
)

func TestBuildCashFlow(t *testing.T) {
	defer recon.SetCashFlowClockForTest(func() time.Time { return testkit.Date("2026-11-21") })()
	e := testkit.New(t)
	org, owner := e.OwnerSession("acme", e.Handler())
	foreign := e.Org("foreign")
	for _, c := range []struct {
		date   string
		amount int64
		status string
	}{
		{"2026-07-01", 12000, "unmatched"},
		{"2026-07-20", 3000, "matched"},
		{"2026-07-31", -4000, "matched"},
		{"2027-06-30", 200, "unmatched"},
		{"2026-06-30", 99999, "unmatched"},
		{"2027-07-01", 99999, "unmatched"},
		{"2026-07-01", 99999, "ignored"},
	} {
		id := testBankLineID(t, e.Pool, org.ID, c.amount, testkit.Date(c.date), "Activity")
		if _, err := e.Pool.Exec(e.Ctx, "UPDATE bank_lines SET status=$1 WHERE id=$2", c.status, id); err != nil {
			t.Fatal(err)
		}
		if c.amount == 3000 {
			inv := e.Invoice(org, e.Contact(org).ID, testkit.Amount(3000), testkit.InvoiceGST("N-T"), testkit.Issued(testkit.Date(c.date)))
			paymentID := e.Payment(org, inv.ID, 3000, testkit.Date(c.date))
			if _, err := e.Pool.Exec(e.Ctx, "UPDATE payments SET bank_line_id=$1 WHERE id=$2", id, paymentID); err != nil {
				t.Fatal(err)
			}
		}
		if c.amount == -4000 {
			expense, _, err := recon.CreateExpense(e.Ctx, e.Pool, org.ID, recon.NewExpense{
				SpentOn: testkit.Date(c.date), Description: "Paid expense",
				Lines: []recon.NewExpenseLine{{CategoryID: categoryByName(t, e.Pool, org.ID, "Other expenses").ID, Amount: 4000}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := e.Pool.Exec(e.Ctx, "UPDATE expenses SET bank_line_id=$1 WHERE id=$2", id, expense.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	testBankLineID(t, e.Pool, foreign.ID, 99999, testkit.Date("2026-07-01"), "Foreign")
	type month struct {
		Month              string
		Incoming, Outgoing int64
	}
	type response struct {
		From, To string
		Months   []month
	}
	read := func(period string) response {
		t.Helper()
		rr := owner.Do("GET", "/api/v1/orgs/acme/cash-flow?period="+period, nil)
		if rr.Code != 200 {
			t.Fatalf("status=%d body=%s", rr.Code, rr.Body)
		}
		var body response
		if err := json.Unmarshal(rr.Body, &body); err != nil {
			t.Fatal(err)
		}
		return body
	}
	body := read("current")
	if body.From != "2026-07-01" || body.To != "2027-06-30" || len(body.Months) != 12 {
		t.Fatalf("range/months: %+v", body)
	}
	if body.Months[0] != (month{"2026-07-01", 15000, 4000}) || body.Months[11] != (month{"2027-06-01", 200, 0}) {
		t.Fatalf("wrong amounts: %+v", body.Months)
	}
	for i := 1; i < 11; i++ {
		if body.Months[i].Incoming != 0 || body.Months[i].Outgoing != 0 || body.Months[i].Month <= body.Months[i-1].Month {
			t.Fatalf("zero/ordered month: %+v", body.Months)
		}
	}
	_, emptyOwner := e.OwnerSession("empty-two", e.Handler())
	rr := emptyOwner.Do("GET", "/api/v1/orgs/empty-two/cash-flow", nil)
	var zero response
	rr.JSON(&zero)
	if len(zero.Months) != 12 {
		t.Fatalf("empty year: %s", rr.Body)
	}
	for _, m := range zero.Months {
		if m.Incoming != 0 || m.Outgoing != 0 {
			t.Fatalf("empty amounts: %+v", m)
		}
	}
}

func TestCashFlowUsesConfiguredTimezone(t *testing.T) {
	defer recon.SetCashFlowClockForTest(func() time.Time { return time.Date(2026, 6, 30, 16, 0, 0, 0, time.UTC) })()
	e := testkit.New(t)
	loc, err := time.LoadLocation("Australia/Brisbane")
	if err != nil {
		t.Fatal(err)
	}
	handler := httpapi.New(httpapi.Deps{
		Pool: e.Pool, Auth: auth.New(e.Pool, false),
		Cfg: config.Config{Modules: map[string]bool{"reconciliation": true}, Location: loc},
	})
	_, owner := e.OwnerSession("acme", handler)
	rr := owner.Do("GET", "/api/v1/orgs/acme/cash-flow", nil)
	var body struct{ From string }
	rr.JSON(&body)
	if rr.Code != 200 || body.From != "2026-07-01" {
		t.Fatalf("timezone rollover: %s", rr.Body)
	}
}
