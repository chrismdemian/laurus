package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chrismdemian/laurus/internal/cache"
	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// Regression tests from the 2026-09-06 re-review.

// A panic inside a singleflight DoChan fn is re-raised on a fresh goroutine
// where no handler-level recovery can catch it, so the whole MCP server
// process died. refresh() now recovers inside the fn and hands the waiters
// a failed Result. The child process must survive (exit 0) and its read
// must see a "refresh panicked" error.
func TestRefresh_PanicInSyncBecomesToolError(t *testing.T) {
	if os.Getenv("LAURUS_SF_CHILD") == "1" {
		db, err := cache.Open(filepath.Join(t.TempDir(), "cache.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		f := &cmdutil.Factory{
			Version: "test",
			Client:  func() (*canvas.Client, error) { panic("boom in sync") },
			Cache:   func() (*cache.DB, error) { return db, nil },
		}
		s := &Server{newClient: f.Client, newCache: f.Cache, config: f.Config, version: "test"}
		var rows []canvas.Assignment
		_, err = s.read(context.Background(), readSpec{rt: cache.ResourceAssignments, courseID: 1, ttl: tierGrade}, &rows, nil)
		if err == nil || !strings.Contains(err.Error(), "refresh panicked") {
			t.Fatalf("child: err = %v, want a refresh panicked error", err)
		}
		// A second read must work too: the mutexes were released.
		if _, err := s.read(context.Background(), readSpec{rt: cache.ResourceAssignments, courseID: 1, ttl: tierGrade}, &rows, nil); err == nil {
			t.Fatal("child: second read unexpectedly succeeded")
		}
		os.Exit(0)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestRefresh_PanicInSyncBecomesToolError$")
	cmd.Env = append(os.Environ(), "LAURUS_SF_CHILD=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			t.Fatalf("child process died (exit %d) instead of returning a tool error; tail:\n%s", exit.ExitCode(), tailOf(string(out), 600))
		}
		t.Fatalf("running child: %v", err)
	}
}

func tailOf(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

// A resource refused on the last refresh (403) but holding rows from an
// earlier complete sync is served from those rows, flagged stale, and not
// re-fetched on every read inside the tier.
func TestRead_SkippedOverExistingRowsServesThemStale(t *testing.T) {
	f := newFakeCanvas(t)
	s := newTestServer(t, f)
	ctx := context.Background()
	var rows []canvas.Assignment
	if _, err := s.read(ctx, readSpec{rt: cache.ResourceAssignments, courseID: 1, ttl: tierGrade}, &rows, nil); err != nil || len(rows) != 1 {
		t.Fatalf("seed: rows=%d err=%v", len(rows), err)
	}
	db, _ := s.getCache()
	if kept, err := db.RecordSkipped(cache.ResourceAssignments, 1); err != nil || kept != 1 {
		t.Fatalf("RecordSkipped: kept=%d err=%v", kept, err)
	}

	before := f.count("assignment_groups")
	for i := 0; i < 3; i++ {
		rows = nil
		env, err := s.read(ctx, readSpec{rt: cache.ResourceAssignments, courseID: 1, ttl: tierGrade}, &rows, nil)
		if err != nil || len(rows) != 1 || !env.Stale || !strings.Contains(env.Note, "previous set") {
			t.Errorf("read %d: env=%+v rows=%d err=%v; want the kept row, stale, noted", i, env, len(rows), err)
		}
	}
	if f.count("assignment_groups") != before {
		t.Errorf("skipped resource re-fetched inside the tier")
	}
}

// Rows written opportunistically by a CLI command (no sync_meta), or by a
// truncated sync (suspect, last_sync_at NULL), then a 403 on the first MCP
// refresh: there is no complete set to keep, so the reader must get an
// honest empty set flagged with the skip note, not an error for a whole
// tier.
func TestGate_SkipOverOpportunisticRowsNoPriorSync(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/pages") {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"errors":[{"message":"user not authorized"}]}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	db, err := cache.Open(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Server{newClient: func() (*canvas.Client, error) { return canvas.NewClient(srv.URL, "tok", "test"), nil },
		newCache: func() (*cache.DB, error) { return db, nil }}

	// Course 1: opportunistic CLI rows, no sync_meta at all.
	if err := db.UpsertMany(cache.ResourcePages, []cache.CacheItem{{ID: 1, CourseID: 1, Data: map[string]any{"page_id": 1, "title": "Syllabus"}}}); err != nil {
		t.Fatal(err)
	}
	// Course 2: rows from a truncated sync only (suspect, stamp NULL).
	if st, err := db.ReplaceAll(cache.ResourcePages, 2, []cache.CacheItem{{ID: 2, CourseID: 2, Data: map[string]any{"page_id": 2, "title": "Week 1"}}}, cache.ReplaceOptions{Truncated: true}); err != nil || st != cache.StatusSuspect {
		t.Fatalf("seed suspect: %s %v", st, err)
	}

	for _, courseID := range []int64{1, 2} {
		for i := 0; i < 2; i++ { // second read is inside the tier: same outcome
			var pages []canvas.Page
			env, err := s.read(context.Background(), readSpec{rt: cache.ResourcePages, courseID: courseID, ttl: tierStable}, &pages, nil)
			if err != nil {
				t.Fatalf("course %d read %d: reader errors instead of an honest empty set: %v", courseID, i, err)
			}
			if len(pages) != 0 || env.SyncStatus != cache.StatusSkipped || !strings.Contains(env.Note, "forbidden") || env.Stale {
				t.Errorf("course %d read %d: env=%+v rows=%d; want empty, skipped, noted, not stale", courseID, i, env, len(pages))
			}
		}
		if meta, _ := db.GetSyncMeta(cache.ResourcePages, courseID); meta.LastSyncAt.IsZero() || meta.ItemCount != 0 {
			t.Errorf("course %d meta = %+v; want an advanced stamp with item_count 0", courseID, meta)
		}
	}
}

// A panic inside the refresh fn must surface as a failed Result AND release
// the singleflight key so the next call runs a fresh flight (in-process
// counterpart of the child-process test above).
func TestGate_RefreshPanicSurvivesAndReleasesKey(t *testing.T) {
	f := newFakeCanvas(t)
	s := newTestServer(t, f)
	good := s.newClient
	s.newClient = func() (*canvas.Client, error) { panic("factory exploded") }

	var rows []canvas.Assignment
	_, err := s.read(context.Background(), readSpec{rt: cache.ResourceAssignments, courseID: 1, ttl: tierGrade}, &rows, nil)
	if err == nil || !strings.Contains(err.Error(), "refresh panicked") {
		t.Fatalf("want a 'refresh panicked' error, got %v", err)
	}
	s.newClient = good
	done := make(chan struct{})
	go func() {
		defer close(done)
		env, err := s.read(context.Background(), readSpec{rt: cache.ResourceAssignments, courseID: 1, ttl: tierGrade, fresh: true}, &rows, nil)
		if err != nil || len(rows) != 1 {
			t.Errorf("second read after panic: env=%+v rows=%d err=%v", env, len(rows), err)
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("second read hung: singleflight key not released after panic")
	}
}
