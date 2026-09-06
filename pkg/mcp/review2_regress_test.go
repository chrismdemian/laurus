package mcp

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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
