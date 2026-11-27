//go:build integration

package sandbox

import (
	"testing"

	"shigoto/internal/storage"
	"shigoto/internal/testkit"
)

// TestPopulateQuotes seeds quotes in every state for the main sandbox
// client, and repopulating (which resets the sandbox, quotes included)
// still works.
func TestPopulateQuotes(t *testing.T) {
	e := testkit.New(t)
	store := storage.NewDisk(t.TempDir())
	if _, err := Populate(e.Ctx, e.Pool, store, testDay); err != nil {
		t.Fatal(err)
	}
	org := `(SELECT id FROM orgs WHERE slug = 'sbx-property')`
	for status, want := range map[string]int{"draft": 1, "sent": 3, "accepted": 1, "superseded": 1} {
		if n := e.Count("quotes", "org_id = "+org+" AND status = $1", status); n != want {
			t.Errorf("%s quotes = %d, want %d", status, n, want)
		}
	}
	if n := e.Count("quotes", "org_id = "+org+" AND status <> 'draft' AND pdf_file_id IS NULL"); n != 0 {
		t.Errorf("%d sent quotes without a frozen PDF", n)
	}
	if n := e.Count("quote_attachments", "org_id = "+org); n == 0 {
		t.Error("no quote attachments seeded")
	}
	if n := e.Count("projects", "org_id = "+org+" AND billing_method = 'fixed' AND hourly_rate IS NULL"); n != 1 {
		t.Errorf("fixed-price projects from accepted quotes = %d, want 1", n)
	}

	if _, err := Populate(e.Ctx, e.Pool, store, testDay); err != nil {
		t.Fatalf("repopulate with quotes present: %v", err)
	}
	if n := e.Count("quotes", "org_id = "+org); n != 6 {
		t.Errorf("quotes after repopulate = %d, want 6", n)
	}
}
