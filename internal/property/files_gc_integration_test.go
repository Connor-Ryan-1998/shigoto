//go:build integration

package property_test

import (
	"bytes"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"shigoto/internal/core"
	"shigoto/internal/property"
	"shigoto/internal/storage"
	"shigoto/internal/testkit"
)

// TestFileReferencesCoverAllForeignKeys keeps property.FileReferences in
// step with the schema: a new table pointing at files must be listed, or
// the pending-upload clean-up could delete a file it still uses.
func TestFileReferencesCoverAllForeignKeys(t *testing.T) {
	e := testkit.New(t)
	rows, err := e.Pool.Query(e.Ctx, `
		SELECT c.conrelid::regclass::text, a.attname
		FROM pg_constraint c
		JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = ANY(c.conkey)
		WHERE c.contype = 'f' AND c.confrelid = 'files'::regclass`)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for rows.Next() {
		var table, col string
		if err := rows.Scan(&table, &col); err != nil {
			t.Fatal(err)
		}
		got = append(got, table+"."+col)
	}
	var want []string
	for _, r := range property.FileReferences {
		want = append(want, r.Table+"."+r.Column)
	}
	want = append(want, "maintenance_pending_files.file_id")
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("foreign keys to files = %v, FileReferences (+pending) = %v", got, want)
	}
}

func TestPrunePendingFiles(t *testing.T) {
	w := newTicketWorld(t)
	store := storage.NewDisk(t.TempDir())
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	save := func(b byte) core.File {
		f, err := core.SaveFile(w.e.Ctx, w.e.Pool, store, w.org.ID, "p.png", bytes.NewReader(append(append([]byte{}, testkit.PNG...), b)), core.MediaPolicy(0))
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	pend := func(f core.File, age time.Duration) int64 {
		var id int64
		if err := w.tx(t, func(tx pgx.Tx) error {
			var err error
			id, err = property.SavePendingFile(w.e.Ctx, tx, w.org.ID, w.tenantUser.ID, f)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := w.e.Pool.Exec(w.e.Ctx, `UPDATE maintenance_pending_files SET created_at = $2 WHERE id = $1`, id, now.Add(-age)); err != nil {
			t.Fatal(err)
		}
		return id
	}
	stale, shared, fresh := save(1), save(2), save(3)
	stalePending := pend(stale, 25*time.Hour)
	sharedPending := pend(shared, 25*time.Hour)
	freshPending := pend(fresh, time.Hour)
	tk := w.tenantTicket(t)
	if err := w.tx(t, func(tx pgx.Tx) error {
		return property.AttachTicketFile(w.e.Ctx, tx, w.org.ID, tk.ID, shared.ID, w.staff.ID)
	}); err != nil {
		t.Fatal(err)
	}

	if err := property.PrunePendingFiles(w.e.Ctx, w.e.Pool, store, now); err != nil {
		t.Fatalf("PrunePendingFiles: %v", err)
	}

	for id, want := range map[int64]int{stalePending: 0, sharedPending: 0, freshPending: 1} {
		if n := w.e.Count("maintenance_pending_files", "id = $1", id); n != want {
			t.Errorf("pending %d rows = %d, want %d", id, n, want)
		}
	}
	if n := w.e.Count("files", "id = $1", stale.ID); n != 0 {
		t.Errorf("stale file row kept")
	}
	if _, err := core.OpenFile(w.e.Ctx, store, stale); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("stale blob kept: %v", err)
	}
	for _, f := range []core.File{shared, fresh} {
		if n := w.e.Count("files", "id = $1", f.ID); n != 1 {
			t.Errorf("file %d deleted", f.ID)
		}
		b, err := core.OpenFile(w.e.Ctx, store, f)
		if err != nil {
			t.Errorf("blob %d: %v", f.ID, err)
		} else {
			b.Close()
		}
	}
}
