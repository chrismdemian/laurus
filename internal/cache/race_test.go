package cache

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestConcurrentWrites_TwoHandles reproduces the pre-fix failure: two
// independent Open() handles on the same file (the MCP server and a cron
// `laurus sync`) writing at once. Before the DSN pragma fix the second
// handle's pooled connections had busy_timeout=0 and most writes failed
// with SQLITE_BUSY; measured 46/60 and 98/120 in the review. The test
// carries a deadline so a MaxOpenConns(1) deadlock fails instead of hanging.
func TestConcurrentWrites_TwoHandles(t *testing.T) {
	// A path with a space mirrors Chris's real cache dir
	// (~/Library/Application Support/laurus) and proves the DSN survives it.
	path := filepath.Join(t.TempDir(), "with space", "cache.db")
	a, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	b, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })

	const writers, perWriter = 6, 20
	var wg sync.WaitGroup
	errs := make(chan error, writers*perWriter)
	for w := 0; w < writers; w++ {
		db := a
		if w%2 == 1 {
			db = b
		}
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				id := int64(w*1000 + i)
				items := []CacheItem{{ID: id, CourseID: 100, Data: testAssignment{ID: id, CourseID: 100, Name: fmt.Sprintf("w%d-%d", w, i)}}}
				if err := db.UpsertMany(ResourceAssignments, items); err != nil {
					errs <- err
					continue
				}
				if err := db.SetSyncMeta(ResourceAssignments, int64(w), i, "success"); err != nil {
					errs <- err
				}
			}
		}(w)
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("writers did not finish within 60s (deadlock?)")
	}
	close(errs)

	var failures []string
	for err := range errs {
		failures = append(failures, err.Error())
	}
	if len(failures) > 0 {
		busy := 0
		for _, f := range failures {
			if strings.Contains(f, "busy") || strings.Contains(f, "locked") {
				busy++
			}
		}
		t.Fatalf("%d/%d writes failed (%d busy/locked); first: %s", len(failures), writers*perWriter, busy, failures[0])
	}

	count, err := b.Count(ResourceAssignments, 100)
	if err != nil {
		t.Fatal(err)
	}
	if count != writers*perWriter {
		t.Errorf("rows = %d, want %d", count, writers*perWriter)
	}
}

// TestOpen_PragmasOnEveryConnection checks the pragmas are connection-level
// (in the DSN), not applied once to whichever connection Exec happened to use.
func TestOpen_PragmasOnEveryConnection(t *testing.T) {
	db := testDB(t)
	for i := 0; i < 5; i++ {
		var timeout int
		if err := db.db.QueryRow("PRAGMA busy_timeout").Scan(&timeout); err != nil {
			t.Fatal(err)
		}
		if timeout < 1000 {
			t.Fatalf("connection %d: busy_timeout = %d, want >= 1000", i, timeout)
		}
		var mode string
		if err := db.db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
			t.Fatal(err)
		}
		if !strings.EqualFold(mode, "wal") {
			t.Fatalf("connection %d: journal_mode = %s, want wal", i, mode)
		}
	}
}
