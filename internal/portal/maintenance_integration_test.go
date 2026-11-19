//go:build integration

package portal_test

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"shigoto/internal/auth"
	"shigoto/internal/property"
	"shigoto/internal/storage"
	"shigoto/internal/testkit"
)

// webmAudio is an EBML header, as browsers' MediaRecorder writes for voice notes.
var webmAudio = append([]byte{0x1A, 0x45, 0xDF, 0xA3}, bytes.Repeat([]byte{0}, 64)...)

// m4aAudio is an ISO-BMFF M4A header, as iOS Safari records.
var m4aAudio = append([]byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p', 'M', '4', 'A', ' '}, bytes.Repeat([]byte{0}, 64)...)

func TestTenantRaisesTicketWithMedia(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	h := e.Handler()
	p := e.Property(org).ID
	tina := e.Contact(org, testkit.Kind("tenant")).ID
	bob := e.Contact(org, testkit.Kind("tenant")).ID
	lease := e.Lease(org, p, tina)
	e.Lease(org, e.Property(org).ID, bob)
	tenant := e.Login(e.PortalUser("tina@example.com", org, auth.Tenant, tina), h)
	other := e.Login(e.PortalUser("bob@example.com", org, auth.Tenant, bob), h)

	var pendingIDs []int64
	for name, data := range map[string][]byte{"leak.png": testkit.PNG, "voice.webm": webmAudio, "voice.m4a": m4aAudio} {
		rr := tenant.Upload("/api/v1/portal/acme/maintenance/uploads", name, data)
		if rr.Code != http.StatusCreated {
			t.Fatalf("upload %s: %d %s", name, rr.Code, rr.Body)
		}
		var out struct {
			PendingID int64 `json:"pending_id"`
		}
		rr.JSON(&out)
		pendingIDs = append(pendingIDs, out.PendingID)
	}
	if rr := tenant.Upload("/api/v1/portal/acme/maintenance/uploads", "x.exe", []byte("MZ\x90\x00 not media")); rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("exe upload: %d", rr.Code)
	}
	// Another tenant cannot attach Tina's pending upload.
	if rr := other.Do("POST", "/api/v1/portal/acme/maintenance", map[string]any{
		"lease_id": lease.ID, "title": "x", "description": "y", "category": "other", "urgency": "routine", "pending_file_ids": pendingIDs[:1],
	}); rr.Code != http.StatusNotFound {
		t.Fatalf("other tenant: %d %s", rr.Code, rr.Body)
	}

	rr := tenant.Do("POST", "/api/v1/portal/acme/maintenance", map[string]any{
		"lease_id": lease.ID, "title": "Tap", "description": "Leaking", "category": "plumbing", "urgency": "urgent",
		"pending_file_ids": pendingIDs,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rr.Code, rr.Body)
	}
	var ticket struct {
		ID        int64 `json:"id"`
		FileCount int   `json:"file_count"`
	}
	rr.JSON(&ticket)
	if ticket.FileCount != 3 {
		t.Fatalf("file_count = %d", ticket.FileCount)
	}

	var detail struct {
		Files []struct {
			ID   int64  `json:"id"`
			MIME string `json:"mime"`
		} `json:"files"`
	}
	tenant.Do("GET", fmt.Sprintf("/api/v1/portal/acme/maintenance/%d", ticket.ID), nil).JSON(&detail)
	if len(detail.Files) != 3 {
		t.Fatalf("files = %+v", detail.Files)
	}
	for _, f := range detail.Files {
		path := fmt.Sprintf("/api/v1/portal/acme/maintenance/%d/files/%d?inline=1", ticket.ID, f.ID)
		got := tenant.DoWith("GET", path, nil, map[string]string{"Range": "bytes=0-1"})
		if got.Code != http.StatusPartialContent || got.Header.Get("Content-Disposition")[:6] != "inline" {
			t.Errorf("%s: %d %q", f.MIME, got.Code, got.Header.Get("Content-Disposition"))
		}
		if denied := other.Do("GET", path, nil); denied.Code != http.StatusNotFound {
			t.Errorf("other tenant file: %d", denied.Code)
		}
	}

	// Adding media to the existing ticket.
	if rr := tenant.Upload(fmt.Sprintf("/api/v1/portal/acme/maintenance/%d/files", ticket.ID), "more.png", append(append([]byte{}, testkit.PNG...), 9)); rr.Code != http.StatusCreated {
		t.Fatalf("add file: %d %s", rr.Code, rr.Body)
	}
}

func TestUploadToClosedTicketLeavesNothing(t *testing.T) {
	e := testkit.New(t)
	org := e.Org("acme")
	dir := t.TempDir()
	h := e.Handler(testkit.Store(storage.NewDisk(dir)))
	tina := e.Contact(org, testkit.Kind("tenant")).ID
	lease := e.Lease(org, e.Property(org).ID, tina)
	tenant := e.Login(e.PortalUser("tina@example.com", org, auth.Tenant, tina), h)
	tk := e.Ticket(org, property.NewTicket{LeaseID: &lease.ID, ContactID: &tina}, "completed", "")
	before := e.Count("files", "true")
	rr := tenant.Upload(fmt.Sprintf("/api/v1/portal/acme/maintenance/%d/files", tk.ID), "late.png", testkit.PNG)
	if rr.Code != http.StatusConflict {
		t.Fatalf("status %d %s", rr.Code, rr.Body)
	}
	if n := e.Count("files", "true"); n != before {
		t.Fatalf("files rows %d -> %d", before, n)
	}
	blobs, _ := os.ReadDir(filepath.Join(dir, strconv.FormatInt(org.ID, 10)))
	if len(blobs) != 0 {
		t.Fatalf("bytes left on disk: %v", blobs)
	}
}
