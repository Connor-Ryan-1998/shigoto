//go:build integration

package quotes_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"shigoto/internal/quotes"
	"shigoto/internal/storage"
	"shigoto/internal/testkit"
)

func TestAttachmentRoutes(t *testing.T) {
	e := testkit.New(t)
	store := storage.NewDisk(t.TempDir())
	handler := e.Handler(testkit.Store(store))
	org, owner := e.OwnerSession("acme", handler)
	q, err := quotes.CreateDraft(e.Ctx, e.Pool, org.ID, draft(e.Contact(org).ID,
		quotes.NewLine{Kind: quotes.KindMaterial, Description: "x", QuantityMilli: 1000, UnitAmount: 100, GSTCode: "FRE"}))
	if err != nil {
		t.Fatal(err)
	}
	base := fmt.Sprintf("/api/v1/orgs/acme/quotes/%d/attachments", q.ID)

	if rr := owner.Upload(base, "notes.txt", []byte("plain text notes")); rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("txt upload: %d %s, want 422", rr.Code, rr.Body)
	}
	rr := owner.Upload(base, "sow.pdf", testkit.PDF("scope"))
	if rr.Code != http.StatusCreated {
		t.Fatalf("pdf upload: %d %s", rr.Code, rr.Body)
	}
	var att struct {
		ID   int64 `json:"id"`
		File struct {
			ID int64 `json:"id"`
		} `json:"file"`
	}
	if err := json.Unmarshal(rr.Body, &att); err != nil {
		t.Fatal(err)
	}
	if rr := owner.Do(http.MethodGet, fmt.Sprintf("%s/%d", base, att.ID), nil); rr.Code != http.StatusOK || !strings.HasPrefix(string(rr.Body), "%PDF") {
		t.Fatalf("download: %d", rr.Code)
	}

	// The same file is also an expense receipt: removing the attachment
	// keeps it.
	var expense int64
	if err := e.Pool.QueryRow(e.Ctx, `INSERT INTO expenses (org_id, spent_on, description, receipt_file_id) VALUES ($1, '2026-12-01', 'Parts', $2) RETURNING id`,
		org.ID, att.File.ID).Scan(&expense); err != nil {
		t.Fatal(err)
	}
	if rr := owner.Do(http.MethodDelete, fmt.Sprintf("%s/%d", base, att.ID), nil); rr.Code != http.StatusNoContent {
		t.Fatalf("remove: %d %s", rr.Code, rr.Body)
	}
	if n := e.Count("files", "id = $1", att.File.ID); n != 1 {
		t.Fatal("shared file was deleted")
	}

	// An unshared file goes with its last attachment.
	rr = owner.Upload(base, "photo.png", testkit.PNG)
	if err := json.Unmarshal(rr.Body, &att); err != nil || rr.Code != http.StatusCreated {
		t.Fatalf("png upload: %d %s", rr.Code, rr.Body)
	}
	if rr := owner.Do(http.MethodDelete, fmt.Sprintf("%s/%d", base, att.ID), nil); rr.Code != http.StatusNoContent {
		t.Fatalf("remove png: %d", rr.Code)
	}
	if n := e.Count("files", "id = $1", att.File.ID); n != 0 {
		t.Fatal("unreferenced file was kept")
	}

	// Deleting a draft garbage-collects its attachments' files.
	rr = owner.Upload(base, "site.png", append(append([]byte{}, testkit.PNG...), 1))
	if err := json.Unmarshal(rr.Body, &att); err != nil || rr.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", rr.Code, rr.Body)
	}
	if rr := owner.Do(http.MethodDelete, fmt.Sprintf("/api/v1/orgs/acme/quotes/%d", q.ID), nil); rr.Code != http.StatusNoContent {
		t.Fatalf("delete draft: %d %s", rr.Code, rr.Body)
	}
	if n := e.Count("files", "id = $1", att.File.ID); n != 0 {
		t.Fatal("draft attachment file kept after delete")
	}
}

func TestQuotePDFRoute(t *testing.T) {
	e := testkit.New(t)
	handler := e.Handler(testkit.Store(storage.NewDisk(t.TempDir())))
	org, owner := e.OwnerSession("acme", handler)
	q, err := quotes.CreateDraft(e.Ctx, e.Pool, org.ID, draft(e.Contact(org).ID,
		quotes.NewLine{Kind: quotes.KindMaterial, Description: "x", QuantityMilli: 1000, UnitAmount: 100, GSTCode: "FRE"}))
	if err != nil {
		t.Fatal(err)
	}
	rr := owner.Do(http.MethodGet, fmt.Sprintf("/api/v1/orgs/acme/quotes/%d/pdf", q.ID), nil)
	if rr.Code != http.StatusOK || rr.Header.Get("Content-Type") != "application/pdf" ||
		rr.Header.Get("Content-Disposition") != `attachment; filename="Q-0001 (draft).pdf"` {
		t.Fatalf("pdf: %d %v", rr.Code, rr.Header)
	}
}

func TestPortalQuotePDF(t *testing.T) {
	e := testkit.New(t)
	store := storage.NewDisk(t.TempDir())
	handler := e.Handler(testkit.Store(store))
	org := e.Org("acme")
	contact := e.Contact(org).ID
	client := e.Login(e.PortalUser("client@example.com", org, "client", contact), handler)
	q, err := quotes.CreateDraft(e.Ctx, e.Pool, org.ID, draft(contact,
		quotes.NewLine{Kind: quotes.KindMaterial, Description: "x", QuantityMilli: 1000, UnitAmount: 100, GSTCode: "FRE"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := markSent(t, e, store, org.ID, q.ID); err != nil {
		t.Fatal(err)
	}
	rr := client.Do(http.MethodGet, fmt.Sprintf("/api/v1/portal/acme/quotes/%d/pdf", q.ID), nil)
	if rr.Code != http.StatusOK || rr.Header.Get("Content-Type") != "application/pdf" || !strings.HasPrefix(string(rr.Body), "%PDF") ||
		rr.Header.Get("Content-Disposition") != `inline; filename=Q-0001.pdf` {
		t.Fatalf("portal pdf: %d %v", rr.Code, rr.Header)
	}
}
