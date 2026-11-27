//go:build integration

package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"shigoto/internal/httpapi"
	"shigoto/internal/testkit"
)

// TestSettingsPutThenIssueRent checks that a rent invoice can't be issued
// until the org has at least 2 payment options, and that PUTting bank and
// PayID details (2 options, since GetEnv is stubbed to never produce a
// Stripe option) unblocks issuing the same invoice.
func TestSettingsPutThenIssueRent(t *testing.T) {
	e := testkit.New(t)
	handler := e.Handler(testkit.OnlyModules())
	org, owner := e.OwnerSession("acme", handler)

	contact := e.Contact(org)
	invoiceID := e.Invoice(org, contact.ID, testkit.Amount(50000), testkit.InvoiceGST("N-T"), testkit.Source("rent")).ID

	rr := owner.Do(http.MethodPost, "/api/v1/orgs/acme/invoices/"+strconv.FormatInt(invoiceID, 10)+"/issue", nil)
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("issue before settings PUT: status = %d, want 422, body=%s", rr.Code, rr.Body)
	}

	putBody := httpapi.SettingsRequest{
		Name:            "Acme Org",
		BankBSB:         "123-456",
		BankAccount:     "12345678",
		PayID:           "owner@example.com",
		InvoiceLeadDays: 7,
	}
	rr = owner.Do(http.MethodPut, "/api/v1/orgs/acme/settings", putBody)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT settings: status = %d, want 200, body=%s", rr.Code, rr.Body)
	}

	rr = owner.Do(http.MethodPost, "/api/v1/orgs/acme/invoices/"+strconv.FormatInt(invoiceID, 10)+"/issue", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("issue after settings PUT: status = %d, want 200, body=%s", rr.Code, rr.Body)
	}
}

func TestSettingsAuditRedactsPaymentDetails(t *testing.T) {
	e := testkit.New(t)
	handler := e.Handler(testkit.OnlyModules())
	org, owner := e.OwnerSession("acme", handler)
	for _, body := range []httpapi.SettingsRequest{
		{Name: "Acme Org", BankBSB: "062-000", BankAccount: "12345678", PayID: "audit@example.com", InvoiceLeadDays: 7},
		{Name: "Acme Org", InvoiceLeadDays: 7},
	} {
		rr := owner.Do(http.MethodPut, "/api/v1/orgs/acme/settings", body)
		if rr.Code != http.StatusOK {
			t.Fatalf("PUT: %d %s", rr.Code, rr.Body)
		}
		var diff httpapi.SettingsRequest
		var raw []byte
		if err := e.Pool.QueryRow(e.Ctx,
			"SELECT diff FROM audit_log WHERE org_id=$1 AND entity='org_settings' ORDER BY id DESC LIMIT 1", org.ID).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &diff); err != nil {
			t.Fatal(err)
		}
		if diff.BankBSB != "[REDACTED]" || diff.BankAccount != "[REDACTED]" || diff.PayID != "[REDACTED]" {
			t.Errorf("audit payment fields not redacted: %s", raw)
		}
		if diff.Name != body.Name || diff.InvoiceLeadDays != body.InvoiceLeadDays {
			t.Errorf("non-sensitive audit fields lost: %s", raw)
		}
		rr = owner.Do(http.MethodGet, "/api/v1/orgs/acme/settings", nil)
		var got httpapi.SettingsRequest
		if rr.Code != http.StatusOK {
			t.Fatalf("GET: %d %s", rr.Code, rr.Body)
		}
		if err := json.Unmarshal(rr.Body, &got); err != nil {
			t.Fatal(err)
		}
		if got.BankBSB != body.BankBSB || got.BankAccount != body.BankAccount || got.PayID != body.PayID {
			t.Errorf("stored payment instructions changed: %+v", got)
		}
	}
}

// TestSettingsBusinessProfile round-trips the profile fields and licences.
func TestSettingsBusinessProfile(t *testing.T) {
	e := testkit.New(t)
	handler := e.Handler(testkit.OnlyModules())
	_, owner := e.OwnerSession("acme", handler)

	put := map[string]any{
		"name": "Acme Electrical", "invoice_lead_days": 7,
		"address": "1 Main St\nBrisbane QLD 4000", "phone": "07 3000 1234", "email": "hi@acme.test",
		"quote_terms": "50% deposit on acceptance.",
		"licences":    []map[string]string{{"type": "QBCC", "number": "1234567"}, {"type": "Electrical", "number": "98765"}},
	}
	rr := owner.Do(http.MethodPut, "/api/v1/orgs/acme/settings", put)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT settings: %d %s", rr.Code, rr.Body)
	}
	var got map[string]any
	rr = owner.Do(http.MethodGet, "/api/v1/orgs/acme/settings", nil)
	if err := json.Unmarshal(rr.Body, &got); err != nil {
		t.Fatal(err)
	}
	if got["address"] != "1 Main St\nBrisbane QLD 4000" || got["email"] != "hi@acme.test" || got["quote_terms"] != "50% deposit on acceptance." || got["has_logo"] != false {
		t.Fatalf("settings = %v", got)
	}
	lic, _ := json.Marshal(got["licences"])
	if string(lic) != `[{"number":"1234567","type":"QBCC"},{"number":"98765","type":"Electrical"}]` {
		t.Fatalf("licences = %s", lic)
	}
}

// TestSettingsLogo covers uploading, replacing, serving and removing the
// org logo.
func TestSettingsLogo(t *testing.T) {
	e := testkit.New(t)
	handler := e.Handler(testkit.OnlyModules())
	org, owner := e.OwnerSession("acme", handler)

	if rr := owner.Upload("/api/v1/orgs/acme/settings/logo", "doc.pdf", testkit.PDF("x")); rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("PDF logo: %d %s, want 422", rr.Code, rr.Body)
	}
	if rr := owner.Upload("/api/v1/orgs/acme/settings/logo", "logo.png", testkit.PNG); rr.Code != http.StatusOK {
		t.Fatalf("upload logo: %d %s", rr.Code, rr.Body)
	}
	rr := owner.Do(http.MethodGet, "/api/v1/orgs/acme/settings/logo", nil)
	if rr.Code != http.StatusOK || rr.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("GET logo: %d %q", rr.Code, rr.Header.Get("Content-Type"))
	}
	first := e.Count("files", "org_id = $1", org.ID)

	// Replacing deletes the old, now unreferenced, file.
	other := append(append([]byte{}, testkit.PNG...), 0)
	if rr := owner.Upload("/api/v1/orgs/acme/settings/logo", "logo2.png", other); rr.Code != http.StatusOK {
		t.Fatalf("replace logo: %d %s", rr.Code, rr.Body)
	}
	if n := e.Count("files", "org_id = $1", org.ID); n != first {
		t.Fatalf("files after replace = %d, want %d", n, first)
	}

	if rr := owner.Do(http.MethodDelete, "/api/v1/orgs/acme/settings/logo", nil); rr.Code != http.StatusNoContent {
		t.Fatalf("DELETE logo: %d %s", rr.Code, rr.Body)
	}
	if n := e.Count("files", "org_id = $1", org.ID); n != first-1 {
		t.Fatalf("files after delete = %d, want %d", n, first-1)
	}
	if rr := owner.Do(http.MethodGet, "/api/v1/orgs/acme/settings/logo", nil); rr.Code != http.StatusNotFound {
		t.Fatalf("GET removed logo: %d", rr.Code)
	}
}
