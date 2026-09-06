package syncer

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chrismdemian/laurus/internal/cache"
	"github.com/chrismdemian/laurus/internal/canvas"
)

// Regression tests from the 2026-09-06 re-review.

// A cache write failure inside SyncJob goes through RecordSyncFailure like
// a fetch failure. On a closed DB the record itself cannot land either, so
// this proves the path runs (both errors are reported), not that a row is
// written.
func TestSyncJob_ReplaceAllErrorIsRecorded(t *testing.T) {
	srv := fakeCanvas(t)
	defer srv.Close()
	client := canvas.NewClient(srv.URL, "tok", "test")
	db := testDB(t)
	_ = db.Close()

	r := SyncJob(context.Background(), client, db, JobAnnouncements, 1)
	if r.Status != cache.StatusFailed || r.Err == nil {
		t.Fatalf("closed db: %+v", r)
	}
	if !strings.Contains(r.Err.Error(), "caching") || !strings.Contains(r.Err.Error(), "recording the failure") {
		t.Errorf("err = %v; want the cache error and the failed record both reported", r.Err)
	}
}

// A resource that synced fine and is then refused (403: a throttle that
// slipped through, or a tab disabled for a while) must keep serving the
// rows it has. Before, the skip advanced last_sync_at and hid every row for
// a full tier behind an empty set.
func TestSyncJob_SkipOverExistingRowsKeepsThem(t *testing.T) {
	var forbid atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/pages") {
			http.NotFound(w, r)
			return
		}
		if forbid.Load() {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"errors":[{"message":"user not authorized"}]}`)
			return
		}
		fmt.Fprint(w, `[{"page_id":1,"title":"Syllabus","url":"syllabus"},{"page_id":2,"title":"Week 1","url":"week-1"}]`)
	}))
	defer srv.Close()
	client := canvas.NewClient(srv.URL, "tok", "test")
	db := testDB(t)
	ctx := context.Background()

	if r := SyncJob(ctx, client, db, JobPages, 1); r.Status != cache.StatusSuccess || r.Count != 2 {
		t.Fatalf("first sync: %+v", r)
	}
	// Age the stamps so "unchanged" is distinguishable from "re-stamped
	// within the same second".
	if err := db.SetSyncMetaAt(cache.ResourcePages, 1, time.Now().Add(-10*time.Minute), 2, cache.StatusSuccess); err != nil {
		t.Fatal(err)
	}
	first, _ := db.GetSyncMeta(cache.ResourcePages, 1)

	forbid.Store(true)
	r := SyncJob(ctx, client, db, JobPages, 1)
	if r.Status != cache.StatusSkipped || r.Err != nil || r.Count != 2 {
		t.Fatalf("refused sync: %+v; want skipped with 2 rows kept", r)
	}
	var pages []map[string]any
	meta, err := db.ListFresh(cache.ResourcePages, 1, &pages)
	if err != nil || len(pages) != 2 {
		t.Errorf("after refusal: %d rows served, %v; want the 2 existing rows", len(pages), err)
	}
	if meta.Status != cache.StatusSkipped || !meta.LastSyncAt.Equal(first.LastSyncAt) || meta.ItemCount != 2 {
		t.Errorf("meta after refusal = %+v; want skipped, unchanged last_sync_at %v, item_count 2", meta, first.LastSyncAt)
	}
	if !meta.LastAttemptAt.After(first.LastAttemptAt) {
		t.Errorf("last_attempt_at did not advance: %v -> %v", first.LastAttemptAt, meta.LastAttemptAt)
	}

	// A course that never had pages: the empty set is the truth, stamp advances.
	if r := SyncJob(ctx, client, db, JobPages, 2); r.Status != cache.StatusSkipped || r.Count != 0 {
		t.Fatalf("refused sync, no rows: %+v", r)
	}
	if m, _ := db.GetSyncMeta(cache.ResourcePages, 2); m.LastSyncAt.IsZero() || m.ItemCount != 0 {
		t.Errorf("never-synced skip should advance the stamp with 0 rows: %+v", m)
	}
}
