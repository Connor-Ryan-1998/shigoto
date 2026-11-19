//go:build integration

package sandbox

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"shigoto/internal/core"
	"shigoto/internal/property"
	"shigoto/internal/storage"
	"shigoto/internal/testkit"
)

func TestPopulateRepairMedia(t *testing.T) {
	e := testkit.New(t)
	store := storage.NewDisk(t.TempDir())
	result, err := Populate(e.Ctx, e.Pool, store, testDay)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := e.Pool.Query(e.Ctx, `SELECT f.id FROM files f
		JOIN maintenance_ticket_files a ON a.file_id=f.id
		JOIN maintenance_tickets t ON t.id=a.ticket_id
		WHERE t.org_id=$1 AND t.title='Kitchen tap leaking'
		AND t.property_id=(SELECT id FROM properties WHERE org_id=$1 AND name='River Cottage')`, result.Orgs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(ids) != 3 {
		t.Fatalf("repair has %d media examples, want photo, video and voice note", len(ids))
	}
	// The configured store must contain each file's exact content, not just metadata.
	for _, id := range ids {
		file, err := core.GetFile(e.Ctx, e.Pool, result.Orgs[0].ID, id)
		if err != nil {
			t.Fatal(err)
		}
		blob, err := core.OpenFile(e.Ctx, store, file)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(blob)
		blob.Close()
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(data)
		if !bytes.Equal(hash[:], file.SHA256) || int64(len(data)) != file.SizeBytes {
			t.Fatal("stored media does not match its metadata")
		}
	}
	for _, mime := range []string{"image/png", "video/mp4", "audio/mp4"} {
		if got := count(t, e.Pool, `SELECT count(*) FROM files WHERE org_id=$1 AND mime=$2`, result.Orgs[0].ID, mime); got != 1 {
			t.Errorf("missing %s fixture", mime)
		}
	}
}

type failMediaCommitStore struct {
	storage.Store
	commits int
}

func (s *failMediaCommitStore) Begin(ctx context.Context, orgID int64) (storage.Pending, error) {
	pending, err := s.Store.Begin(ctx, orgID)
	return &failMediaCommit{Pending: pending, store: s}, err
}

type failMediaCommit struct {
	storage.Pending
	store *failMediaCommitStore
}

func (p *failMediaCommit) Commit(ctx context.Context, key storage.Key) error {
	p.store.commits++
	if p.store.commits == 2 {
		return errors.New("injected sandbox media failure")
	}
	return p.Pending.Commit(ctx, key)
}

func TestPopulateMediaFailureRollsBack(t *testing.T) {
	for _, failure := range []string{"storage", "database commit"} {
		t.Run(failure, func(t *testing.T) {
			e := testkit.New(t)
			dir := t.TempDir()
			disk := storage.NewDisk(dir)
			if _, err := Populate(e.Ctx, e.Pool, disk, testDay); err != nil {
				t.Fatal(err)
			}
			mustExec(t, e.Pool, `UPDATE orgs SET name='Tester edit' WHERE slug='sbx-property'`)
			paths := func() []string {
				var paths []string
				if err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
					if err == nil && !entry.IsDir() {
						paths = append(paths, path)
					}
					return err
				}); err != nil {
					t.Fatal(err)
				}
				return paths
			}
			before := paths()
			var store storage.Store = &failMediaCommitStore{Store: disk}
			if failure == "database commit" {
				store = disk
				mustExec(t, e.Pool, `CREATE FUNCTION fail_media_commit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected database commit failure'; END $$`)
				mustExec(t, e.Pool, `CREATE CONSTRAINT TRIGGER fail_media_commit AFTER INSERT ON files DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fail_media_commit()`)
			}
			result, err := Populate(e.Ctx, e.Pool, store, testDay)
			if err == nil || len(result.Accounts) != 0 {
				t.Fatalf("failed population returned access details: %+v, %v", result, err)
			}
			if got := count(t, e.Pool, `SELECT count(*) FROM orgs WHERE name='Tester edit'`); got != 1 {
				t.Fatal("failed media population did not restore previous sandbox")
			}
			if after := paths(); !reflect.DeepEqual(before, after) {
				t.Fatalf("failed population changed stored media: before=%v after=%v", before, after)
			}
		})
	}
}

type lostCommitReply struct{ pgx.Tx }

func (tx lostCommitReply) Commit(ctx context.Context) error {
	if err := tx.Tx.Commit(ctx); err != nil {
		return err
	}
	return io.ErrUnexpectedEOF
}

func TestPopulateLostCommitReplyPreservesMedia(t *testing.T) {
	e := testkit.New(t)
	store := storage.NewDisk(t.TempDir())
	tx, err := e.Pool.Begin(e.Ctx)
	if err != nil {
		t.Fatal(err)
	}
	result, err := populateTransaction(e.Ctx, lostCommitReply{tx}, store, testDay, time.UTC)
	if !errors.Is(err, io.ErrUnexpectedEOF) || len(result.Accounts) != 0 {
		t.Fatalf("expected uncertain commit without access details, got %+v, %v", result, err)
	}
	if got := count(t, e.Pool, `SELECT count(*) FROM sandbox_orgs`); got != 2 {
		t.Fatal("test must commit before losing the reply")
	}
	rows, err := e.Pool.Query(e.Ctx, `SELECT id,org_id FROM files`)
	if err != nil {
		t.Fatal(err)
	}
	var files []struct{ id, orgID int64 }
	for rows.Next() {
		var file struct{ id, orgID int64 }
		if err := rows.Scan(&file.id, &file.orgID); err != nil {
			t.Fatal(err)
		}
		files = append(files, file)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no committed media to verify")
	}
	for _, id := range files {
		file, err := core.GetFile(e.Ctx, e.Pool, id.orgID, id.id)
		if err != nil {
			t.Fatal(err)
		}
		blob, err := core.OpenFile(e.Ctx, store, file)
		if err != nil {
			t.Fatalf("commit reply was lost, but committed media must survive: %v", err)
		}
		blob.Close()
	}
}

func TestPopulateVisitsUsePopulationTimezone(t *testing.T) {
	e := testkit.New(t)
	location, err := time.LoadLocation("Australia/Brisbane")
	if err != nil {
		t.Fatal(err)
	}
	result, err := Populate(e.Ctx, e.Pool, storage.NewDisk(t.TempDir()), time.Date(2026, 11, 19, 23, 30, 0, 0, location))
	if err != nil {
		t.Fatal(err)
	}
	var start time.Time
	if err := e.Pool.QueryRow(e.Ctx, `SELECT starts_at FROM maintenance_visits v JOIN maintenance_tickets t ON t.id=v.ticket_id
		WHERE v.org_id=$1 AND t.title='Laundry tap washer'`, result.Orgs[0].ID).Scan(&start); err != nil {
		t.Fatal(err)
	}
	if got := start.In(location).Format(time.RFC3339); got != "2026-11-24T09:00:00+10:00" {
		t.Fatalf("booked visit = %s, want 9am on the fifth day in population timezone", got)
	}
}

func TestPopulateRepairScenarios(t *testing.T) {
	e := testkit.New(t)
	result := populateTest(t, e.Pool)
	org := result.Orgs[0].ID
	for _, status := range []string{"new", "viewed", "assigned", "scheduled", "completed", "cancelled"} {
		if got := count(t, e.Pool, `SELECT count(*) FROM maintenance_tickets WHERE org_id=$1 AND status=$2`, org, status); got == 0 {
			t.Errorf("no %s repair example", status)
		}
	}
	for name, query := range map[string]string{
		"emergency phone":      `SELECT count(*) FROM orgs WHERE id=$1 AND emergency_phone<>''`,
		"availability":         `SELECT count(*) FROM maintenance_availability WHERE org_id=$1`,
		"shared message":       `SELECT count(*) FROM maintenance_events WHERE org_id=$1 AND kind='comment' AND tenant_visible`,
		"internal note":        `SELECT count(*) FROM maintenance_events WHERE org_id=$1 AND kind='note' AND NOT tenant_visible`,
		"tenant-home visit":    `SELECT count(*) FROM maintenance_visits WHERE org_id=$1 AND status='confirmed' AND entry_mode='tenant_home'`,
		"entry permitted":      `SELECT count(*) FROM maintenance_visits WHERE org_id=$1 AND status='confirmed' AND entry_mode='permitted' AND entry_granted_at IS NOT NULL AND entry_withdrawn_at IS NULL`,
		"entry withdrawn":      `SELECT count(*) FROM maintenance_visits WHERE org_id=$1 AND status='confirmed' AND entry_withdrawn_at IS NOT NULL`,
		"change requested":     `SELECT count(*) FROM maintenance_visits WHERE org_id=$1 AND status='change_requested' AND change_note<>'' AND entry_mode IS NULL`,
		"waiting confirmation": `SELECT count(*) FROM maintenance_visits WHERE org_id=$1 AND status='booked' AND tenant_confirmed_at IS NULL AND entry_mode IS NULL`,
		"completed visit":      `SELECT count(*) FROM maintenance_visits WHERE org_id=$1 AND status='done'`,
		"other tenant repair":  `SELECT count(*) FROM maintenance_tickets t JOIN contacts c ON c.id=t.raised_by_contact_id WHERE t.org_id=$1 AND c.email='tenant-other@example.test'`,
	} {
		if got := count(t, e.Pool, query, org); got == 0 {
			t.Errorf("missing %s example", name)
		}
	}
	if got := count(t, e.Pool, `SELECT count(*) FROM maintenance_tickets WHERE org_id=$1`, result.Orgs[1].ID); got == 0 {
		t.Error("second organisation has no repair example")
	}
	if got := count(t, e.Pool, `SELECT count(*) FROM maintenance_visits WHERE status IN ('booked','confirmed','change_requested') AND starts_at<=$1`, testDay); got != 0 {
		t.Error("open visits must be after population day")
	}
}

func TestPopulateRentWriteOffAndRecovery(t *testing.T) {
	e := testkit.New(t)
	result := populateTest(t, e.Pool)
	var leaseID int64
	if err := e.Pool.QueryRow(e.Ctx, `SELECT l.id FROM leases l JOIN properties p ON p.id=l.property_id
		WHERE l.org_id=$1 AND p.name='River Cottage'`, result.Orgs[0].ID).Scan(&leaseID); err != nil {
		t.Fatal(err)
	}
	account, err := property.LeaseAccount(e.Ctx, e.Pool, result.Orgs[0].ID, leaseID)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[property.EntryKind]bool{}
	for _, entry := range account.Entries {
		kinds[entry.Kind] = true
	}
	for _, kind := range []property.EntryKind{property.EntryCharge, property.EntryPayment, property.EntryWriteOff, property.EntryWriteOffReversed} {
		if !kinds[kind] {
			t.Errorf("lease account missing %s example", kind)
		}
	}
}
