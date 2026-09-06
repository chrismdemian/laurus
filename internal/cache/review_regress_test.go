package cache

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Regression tests from the 2026-09-06 review. Each one reproduced a real
// hole before its fix and is kept here so the hole cannot reopen.

// C2: an honest empty fetch (no truncation error) is the truth: upstream
// deleted everything, so the cached rows are pruned and readers see zero.
// Before the fix every empty fetch against a populated table was marked
// suspect, so the deleted rows were served forever and re-fetched every
// backoff window forever.
func TestReplaceAll_HonestEmptyPrunesDeletedRows(t *testing.T) {
	db := testDB(t)
	mk := func(ids ...int64) []CacheItem {
		items := make([]CacheItem, len(ids))
		for i, id := range ids {
			items[i] = CacheItem{ID: id, CourseID: 100, Data: testAssignment{ID: id, CourseID: 100}}
		}
		return items
	}
	if _, err := db.ReplaceAll(ResourceAnnouncements, 100, mk(1, 2), ReplaceOptions{}); err != nil {
		t.Fatal(err)
	}
	st, err := db.ReplaceAll(ResourceAnnouncements, 100, nil, ReplaceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var got []testAssignment
	meta, err := db.ListFresh(ResourceAnnouncements, 100, &got)
	if err != nil {
		t.Fatal(err)
	}
	if st != StatusSuccess || len(got) != 0 || meta.Status != StatusSuccess || meta.ItemCount != 0 {
		t.Errorf("after honest empty fetch: status=%s served=%d meta=%+v; want success, 0 rows", st, len(got), meta)
	}
	if n, _ := db.Count(ResourceAnnouncements, 100); n != 0 {
		t.Errorf("table still holds %d rows, want 0", n)
	}

	// The guard that remains: a TRUNCATED empty fetch keeps the rows.
	if _, err := db.ReplaceAll(ResourceAnnouncements, 100, mk(1, 2), ReplaceOptions{}); err != nil {
		t.Fatal(err)
	}
	st, err = db.ReplaceAll(ResourceAnnouncements, 100, nil, ReplaceOptions{Truncated: true})
	if err != nil || st != StatusSuspect {
		t.Fatalf("truncated empty fetch: status=%s err=%v", st, err)
	}
	got = nil
	if _, err := db.ListFresh(ResourceAnnouncements, 100, &got); err != nil || len(got) != 2 {
		t.Errorf("after truncated empty fetch: %d rows, want 2 kept", len(got))
	}
}

func bigItems(course int64, n int) []CacheItem {
	items := make([]CacheItem, n)
	for i := range items {
		items[i] = CacheItem{ID: int64(i), CourseID: course, Data: testAssignment{ID: int64(i), CourseID: course, Name: strings.Repeat("x", 200)}}
	}
	return items
}

// C1: two handles (two processes) hammering ReplaceAll on the same course.
// ReplaceAll is a write transaction; with a DEFERRED BEGIN a concurrent
// commit between the tx start and its first write returned SQLITE_BUSY at
// once (busy_timeout does not cover a snapshot upgrade) and 40/80 calls
// failed. BEGIN IMMEDIATE (_txlock in the DSN) makes them wait instead.
func TestReplaceAll_TwoHandlesSameCourse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.db")
	a, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	big := bigItems(7, 300)
	const rounds = 40
	var wg sync.WaitGroup
	errs := make(chan error, 2*rounds)
	for _, db := range []*DB{a, b} {
		wg.Add(1)
		go func(db *DB) {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				if _, err := db.ReplaceAll(ResourceAssignments, 7, big, ReplaceOptions{}); err != nil {
					errs <- err
				}
			}
		}(db)
	}
	wg.Wait()
	close(errs)
	var fails []string
	for e := range errs {
		fails = append(fails, e.Error())
	}
	if len(fails) > 0 {
		t.Errorf("%d/%d ReplaceAll failed across two handles; first: %s", len(fails), 2*rounds, fails[0])
	}
	if n, _ := b.Count(ResourceAssignments, 7); n != 300 {
		t.Errorf("rows = %d, want 300", n)
	}
}

// C1 variant: different tables and courses, paced like a real sync.
func TestReplaceAll_TwoHandlesDifferentTablesPaced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.db")
	a, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	const rounds = 20
	var wg sync.WaitGroup
	errs := make(chan error, 2*rounds)
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			if _, err := a.ReplaceAll(ResourceAssignments, 7, bigItems(7, 200), ReplaceOptions{}); err != nil {
				errs <- err
			}
			time.Sleep(3 * time.Millisecond)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			if _, err := b.ReplaceAll(ResourceAnnouncements, 8, bigItems(8, 200), ReplaceOptions{}); err != nil {
				errs <- err
			}
			time.Sleep(3 * time.Millisecond)
		}
	}()
	wg.Wait()
	close(errs)
	n := 0
	var first string
	for e := range errs {
		n++
		if first == "" {
			first = e.Error()
		}
	}
	if n > 0 {
		t.Errorf("%d/%d ReplaceAll failed (different tables, paced); first: %s", n, 2*rounds, first)
	}
}

// Control for the two tests above: the same load through UpsertMany, a
// transaction that starts with a write, passed even before the fix. Kept so
// a future regression in the ReplaceAll tests can be attributed correctly.
func TestUpsertMany_TwoHandlesControl(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.db")
	a, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	const rounds = 40
	var wg sync.WaitGroup
	errs := make(chan error, 2*rounds)
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			if err := a.UpsertMany(ResourceAssignments, bigItems(7, 200)); err != nil {
				errs <- err
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			if err := b.UpsertMany(ResourceAnnouncements, bigItems(8, 200)); err != nil {
				errs <- err
			}
		}
	}()
	wg.Wait()
	close(errs)
	n := 0
	for range errs {
		n++
	}
	if n > 0 {
		t.Errorf("control: %d/%d UpsertMany failed", n, 2*rounds)
	}
}

// C1: several processes opening a V2 file at once. migrate() used to read
// user_version outside the transaction, so every opener saw 2 and all but
// the first failed with "duplicate column name". Now the version is read
// inside the immediate tx and re-checked after the lock is held.
func TestMigrate_ConcurrentOpenOfOldFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db0, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		"ALTER TABLE sync_meta DROP COLUMN last_attempt_at",
		"ALTER TABLE sync_meta DROP COLUMN error",
		"PRAGMA user_version = 2",
	} {
		if _, err := db0.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	_ = db0.Close()

	const n = 4
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := Open(path)
			if err != nil {
				errs <- err
				return
			}
			_ = d.Close()
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Errorf("concurrent Open on a V2 file: %v", e)
	}

	// Positive checks on the end state: version 3, each V3 column exactly once.
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var v int
	if err := d.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil || v != len(migrations) {
		t.Errorf("user_version = %d (%v), want %d", v, err, len(migrations))
	}
	rows, err := d.db.Query("PRAGMA table_info(sync_meta)")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols := map[string]int{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		cols[name]++
	}
	for _, c := range []string{"last_attempt_at", "error"} {
		if cols[c] != 1 {
			t.Errorf("column %s present %d times, want 1", c, cols[c])
		}
	}
}
