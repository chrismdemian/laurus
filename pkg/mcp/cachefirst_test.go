package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/chrismdemian/laurus/internal/cache"
	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// fakeCanvas counts requests per path suffix and can be told to fail.
type fakeCanvas struct {
	srv   *httptest.Server
	hits  sync.Map // suffix -> *int32
	fail  atomic.Bool
	delay time.Duration
}

func newFakeCanvas(t *testing.T) *fakeCanvas {
	t.Helper()
	f := &fakeCanvas{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		key := p[strings.LastIndex(p, "/")+1:]
		if strings.HasPrefix(p, "/api/v1/courses/") && strings.Count(p, "/") == 4 {
			key = "course"
		}
		v, _ := f.hits.LoadOrStore(key, new(int32))
		atomic.AddInt32(v.(*int32), 1)
		if f.delay > 0 {
			time.Sleep(f.delay)
		}
		if f.fail.Load() && key != "courses" {
			// 400-class so the retrying client does not back off for seconds.
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"errors":[{"message":"boom"}]}`)
			return
		}
		switch key {
		case "courses":
			if r.URL.Query().Get("enrollment_state") == "completed" {
				fmt.Fprint(w, `[]`)
				return
			}
			fmt.Fprint(w, `[{"id":1,"name":"Intro to CS","course_code":"CSC108","workflow_state":"available","enrollments":[{"type":"student","computed_current_score":91.5}]}]`)
		case "assignment_groups":
			fmt.Fprint(w, `[{"id":10,"name":"HW","assignments":[{"id":101,"name":"Problem Set 1","course_id":1,"due_at":"2099-01-01T00:00:00Z","points_possible":10,"submission":{"id":501,"score":9}}]}]`)
		case "assignments":
			fmt.Fprint(w, `[{"id":101,"name":"Problem Set 1","course_id":1,"due_at":"2099-01-01T00:00:00Z"}]`)
		case "unread_count":
			fmt.Fprint(w, `{"unread_count":"3"}`)
		case "pages":
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"errors":[{"message":"forbidden"}]}`)
		case "announcements", "discussion_topics", "modules", "files", "folders":
			fmt.Fprint(w, `[]`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeCanvas) count(key string) int32 {
	v, ok := f.hits.Load(key)
	if !ok {
		return 0
	}
	return atomic.LoadInt32(v.(*int32))
}

func newTestServer(t *testing.T, f *fakeCanvas) *Server {
	t.Helper()
	db, err := cache.Open(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	var clients int32
	factory := &cmdutil.Factory{
		Version: "test",
		Client: func() (*canvas.Client, error) {
			atomic.AddInt32(&clients, 1)
			return canvas.NewClient(f.srv.URL, "tok", "test"), nil
		},
		Cache: func() (*cache.DB, error) { return db, nil },
	}
	s := &Server{newClient: factory.Client, newCache: factory.Cache, config: factory.Config, version: "test"}
	t.Cleanup(func() {
		if n := atomic.LoadInt32(&clients); n > 1 {
			t.Errorf("client constructed %d times; must be memoised", n)
		}
	})
	return s
}

func decodeEnvelope(t *testing.T, res *mcplib.CallToolResult) envelope {
	t.Helper()
	if res == nil || len(res.Content) == 0 {
		t.Fatal("empty result")
	}
	tc, ok := res.Content[0].(mcplib.TextContent)
	if !ok {
		t.Fatalf("content = %T", res.Content[0])
	}
	if res.IsError {
		t.Fatalf("tool error: %s", tc.Text)
	}
	var env envelope
	if err := json.Unmarshal([]byte(tc.Text), &env); err != nil {
		t.Fatalf("not an envelope: %v\n%s", err, tc.Text)
	}
	return env
}

func TestCacheFirst_ServesFromCacheAfterFirstFetch(t *testing.T) {
	f := newFakeCanvas(t)
	s := newTestServer(t, f)
	ctx := context.Background()

	res, err := s.handleListAssignments(ctx, mcplib.CallToolRequest{}, listAssignmentsArgs{Course: "CSC108"})
	if err != nil {
		t.Fatal(err)
	}
	env := decodeEnvelope(t, res)
	if env.Source != sourceCache || env.Stale || env.AsOf.IsZero() {
		t.Errorf("first read envelope = %+v, want cache/fresh/stamped", env)
	}
	data, _ := json.Marshal(env.Data)
	if !strings.Contains(string(data), "Problem Set 1") || !strings.Contains(string(data), `"score":9`) {
		t.Errorf("data = %s", data)
	}
	firstGroups, firstCourses := f.count("assignment_groups"), f.count("courses")
	if firstGroups != 1 {
		t.Fatalf("assignment_groups fetched %d times on first read, want 1", firstGroups)
	}

	// Second read: no Canvas traffic at all (course resolution included).
	res, _ = s.handleListAssignments(ctx, mcplib.CallToolRequest{}, listAssignmentsArgs{Course: "csc108"})
	env = decodeEnvelope(t, res)
	if env.Source != sourceCache {
		t.Errorf("second read source = %s", env.Source)
	}
	if f.count("assignment_groups") != firstGroups || f.count("courses") != firstCourses {
		t.Errorf("second read hit Canvas: groups %d->%d courses %d->%d", firstGroups, f.count("assignment_groups"), firstCourses, f.count("courses"))
	}

	// fresh=true re-fetches.
	res, _ = s.handleListAssignments(ctx, mcplib.CallToolRequest{}, listAssignmentsArgs{Course: "CSC108", Fresh: true})
	decodeEnvelope(t, res)
	if f.count("assignment_groups") != firstGroups+1 {
		t.Errorf("fresh=true did not refetch (groups=%d)", f.count("assignment_groups"))
	}
}

func TestCacheFirst_ExpiredTierRefreshesOnce(t *testing.T) {
	f := newFakeCanvas(t)
	s := newTestServer(t, f)
	ctx := context.Background()
	res, _ := s.handleListAssignments(ctx, mcplib.CallToolRequest{}, listAssignmentsArgs{Course: "CSC108"})
	decodeEnvelope(t, res)
	db, _ := s.getCache()

	// Age the stamp past the 5-minute tier and fire 10 concurrent reads.
	if err := db.SetSyncMetaAt(cache.ResourceAssignments, 1, time.Now().Add(-10*time.Minute), 1, cache.StatusSuccess); err != nil {
		t.Fatal(err)
	}
	before := f.count("assignment_groups")
	f.delay = 100 * time.Millisecond
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, _ := s.handleListAssignments(ctx, mcplib.CallToolRequest{}, listAssignmentsArgs{Course: "CSC108"})
			decodeEnvelope(t, res)
		}()
	}
	wg.Wait()
	if got := f.count("assignment_groups") - before; got != 1 {
		t.Errorf("expired tier + 10 concurrent reads fetched %d times, want 1 (singleflight)", got)
	}
}

func TestCacheFirst_SyncFailureServesStaleWithError(t *testing.T) {
	f := newFakeCanvas(t)
	s := newTestServer(t, f)
	ctx := context.Background()
	res, _ := s.handleListAssignments(ctx, mcplib.CallToolRequest{}, listAssignmentsArgs{Course: "CSC108"})
	decodeEnvelope(t, res)
	db, _ := s.getCache()
	if err := db.SetSyncMetaAt(cache.ResourceAssignments, 1, time.Now().Add(-10*time.Minute), 1, cache.StatusSuccess); err != nil {
		t.Fatal(err)
	}

	f.fail.Store(true)
	res, _ = s.handleListAssignments(ctx, mcplib.CallToolRequest{}, listAssignmentsArgs{Course: "CSC108"})
	env := decodeEnvelope(t, res)
	if !env.Stale || env.SyncError == "" || env.Source != sourceCache {
		t.Fatalf("after failed refresh: %+v; want stale cached data with sync_error", env)
	}
	data, _ := json.Marshal(env.Data)
	if !strings.Contains(string(data), "Problem Set 1") {
		t.Errorf("stale data missing: %s", data)
	}

	// Backoff: the next read within the window does not retry Canvas.
	before := f.count("assignment_groups")
	res, _ = s.handleListAssignments(ctx, mcplib.CallToolRequest{}, listAssignmentsArgs{Course: "CSC108"})
	env = decodeEnvelope(t, res)
	if f.count("assignment_groups") != before {
		t.Errorf("read during backoff retried Canvas")
	}
	if !env.Stale {
		t.Errorf("read during backoff must still say stale: %+v", env)
	}

	// fresh=true bypasses the backoff and, with Canvas healthy again, clears it.
	f.fail.Store(false)
	res, _ = s.handleListAssignments(ctx, mcplib.CallToolRequest{}, listAssignmentsArgs{Course: "CSC108", Fresh: true})
	env = decodeEnvelope(t, res)
	if env.Stale || env.SyncError != "" {
		t.Errorf("after recovery: %+v", env)
	}
}

func TestCacheFirst_NothingCachedAndFetchFailsIsError(t *testing.T) {
	f := newFakeCanvas(t)
	s := newTestServer(t, f)
	f.fail.Store(true)
	res, err := s.handleListAssignments(context.Background(), mcplib.CallToolRequest{}, listAssignmentsArgs{Course: "CSC108"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatalf("expected a tool error when nothing is cached and the fetch fails; got %+v", res.Content)
	}
}

func TestForceLive_AlwaysHitsCanvas(t *testing.T) {
	f := newFakeCanvas(t)
	s := newTestServer(t, f)
	ctx := context.Background()
	for i := 1; i <= 3; i++ {
		res, err := s.handleGetUnreadCount(ctx, mcplib.CallToolRequest{}, getUnreadCountArgs{})
		if err != nil {
			t.Fatal(err)
		}
		env := decodeEnvelope(t, res)
		if env.Source != sourceLive || env.Stale {
			t.Errorf("call %d: %+v, want live", i, env)
		}
		if f.count("unread_count") != int32(i) {
			t.Errorf("call %d: unread_count fetched %d times", i, f.count("unread_count"))
		}
	}
}

func TestFindCourse_ResolvesFromCache(t *testing.T) {
	f := newFakeCanvas(t)
	s := newTestServer(t, f)
	ctx := context.Background()
	for _, q := range []string{"CSC108", "csc108", "1", "intro"} {
		c, err := s.findCourse(ctx, q)
		if err != nil || c.ID != 1 {
			t.Errorf("findCourse(%q) = %+v, %v", q, c, err)
		}
	}
	if n := f.count("courses"); n != 2 { // active + completed, once
		t.Errorf("course list fetched %d times for 4 lookups, want 2 (one sync)", n)
	}
	if _, err := s.findCourse(ctx, "nope"); err == nil {
		t.Error("unknown course must fall through to an error")
	}
}

// A resource Canvas refuses (403) is recorded as skipped and served as an
// honest empty set without a request per read.
func TestCacheFirst_SkippedResourceIsNotRefetchedEveryRead(t *testing.T) {
	f := newFakeCanvas(t)
	s := newTestServer(t, f)
	ctx := context.Background()
	for i := 1; i <= 3; i++ {
		res, err := s.handleListPages(ctx, mcplib.CallToolRequest{}, listPagesArgs{Course: "CSC108"})
		if err != nil {
			t.Fatal(err)
		}
		env := decodeEnvelope(t, res)
		if env.SyncStatus != cache.StatusSkipped || env.Note == "" || env.Stale {
			t.Errorf("read %d: %+v; want skipped, noted, not stale", i, env)
		}
	}
	if n := f.count("pages"); n != 1 {
		t.Errorf("pages fetched %d times across 3 reads, want 1", n)
	}
}

// The backoff lives in sync_meta, so a second process (a new Server on the
// same database) honours it too, and laurus_sync reports the failure.
func TestBackoff_CrossProcessAndSyncToolReportsIt(t *testing.T) {
	f := newFakeCanvas(t)
	s := newTestServer(t, f)
	ctx := context.Background()
	res, _ := s.handleListAssignments(ctx, mcplib.CallToolRequest{}, listAssignmentsArgs{Course: "CSC108"})
	decodeEnvelope(t, res)
	db, _ := s.getCache()
	if err := db.SetSyncMetaAt(cache.ResourceAssignments, 1, time.Now().Add(-10*time.Minute), 1, cache.StatusSuccess); err != nil {
		t.Fatal(err)
	}
	f.fail.Store(true)
	res, _ = s.handleListAssignments(ctx, mcplib.CallToolRequest{}, listAssignmentsArgs{Course: "CSC108"})
	if env := decodeEnvelope(t, res); !env.Stale || env.SyncError == "" {
		t.Fatalf("expected stale+sync_error, got %+v", env)
	}
	meta, _ := db.GetSyncMeta(cache.ResourceAssignments, 1)
	if meta.Status != cache.StatusFailed || meta.Error == "" {
		t.Fatalf("failure not recorded in sync_meta: %+v", meta)
	}

	// A different Server sharing the database (another process) must not
	// retry inside the window. It builds its own client, as a process would.
	other := &Server{newClient: func() (*canvas.Client, error) { return canvas.NewClient(f.srv.URL, "tok", "test"), nil }, newCache: s.newCache, version: "test"}
	before := f.count("assignment_groups")
	res, _ = other.handleListAssignments(ctx, mcplib.CallToolRequest{}, listAssignmentsArgs{Course: "CSC108"})
	if env := decodeEnvelope(t, res); !env.Stale {
		t.Errorf("second process served non-stale data during backoff: %+v", env)
	}
	if f.count("assignment_groups") != before {
		t.Errorf("second process retried Canvas during backoff")
	}

	// laurus_sync runs regardless and lists the failure with non-success status.
	res, err := other.handleSync(ctx, mcplib.CallToolRequest{}, syncArgs{Course: "CSC108"})
	if err != nil {
		t.Fatal(err)
	}
	tc := res.Content[0].(mcplib.TextContent)
	var rep syncReport
	if err := json.Unmarshal([]byte(tc.Text), &rep); err != nil {
		t.Fatalf("sync report: %v\n%s", err, tc.Text)
	}
	if rep.Status == "success" || len(rep.Errors) == 0 {
		t.Errorf("sync report = %+v; want non-success with errors", rep)
	}

	// Recovery through the tool clears the failure and the next read is fresh.
	f.fail.Store(false)
	res, _ = other.handleSync(ctx, mcplib.CallToolRequest{}, syncArgs{Course: "CSC108"})
	tc = res.Content[0].(mcplib.TextContent)
	_ = json.Unmarshal([]byte(tc.Text), &rep)
	if rep.Status != "success" {
		t.Errorf("after recovery sync report = %+v", rep)
	}
	res, _ = s.handleListAssignments(ctx, mcplib.CallToolRequest{}, listAssignmentsArgs{Course: "CSC108"})
	if env := decodeEnvelope(t, res); env.Stale || env.SyncError != "" {
		t.Errorf("after recovery read = %+v", env)
	}
}

// A suspect (truncated) resource is served from the previous complete set
// and not re-fetched on every read; after the backoff it is retried.
func TestCacheFirst_SuspectBacksOff(t *testing.T) {
	f := newFakeCanvas(t)
	s := newTestServer(t, f)
	ctx := context.Background()
	res, _ := s.handleListAssignments(ctx, mcplib.CallToolRequest{}, listAssignmentsArgs{Course: "CSC108"})
	decodeEnvelope(t, res)
	db, _ := s.getCache()
	// Simulate a truncated sync moments ago: status suspect, attempt just now.
	if err := db.SetSyncMetaAt(cache.ResourceAssignments, 1, time.Now(), 1, cache.StatusSuspect); err != nil {
		t.Fatal(err)
	}
	before := f.count("assignment_groups")
	// The single-resource read reports the suspect status verbatim.
	var rows []canvas.Assignment
	env, err := s.read(ctx, readSpec{rt: cache.ResourceAssignments, courseID: 1, ttl: tierGrade}, &rows, nil)
	if err != nil || !env.Stale || env.SyncStatus != cache.StatusSuspect || len(rows) != 1 {
		t.Fatalf("direct read: env=%+v rows=%d err=%v; want stale suspect with the previous set", env, len(rows), err)
	}
	// The aggregate handler folds it into stale=true (it merges courses, so
	// it carries no single sync_status).
	for i := 0; i < 3; i++ {
		res, _ = s.handleListAssignments(ctx, mcplib.CallToolRequest{}, listAssignmentsArgs{Course: "CSC108"})
		if env := decodeEnvelope(t, res); !env.Stale || len(env.Data.([]any)) != 1 {
			t.Errorf("read %d: %+v; want stale with the previous set", i, env)
		}
	}
	if f.count("assignment_groups") != before {
		t.Errorf("suspect resource was re-fetched inside the backoff window")
	}
	// Age the attempt past the window: the next read retries and recovers.
	if err := db.SetSyncMetaAt(cache.ResourceAssignments, 1, time.Now().Add(-backoffAfterFailure-time.Second), 1, cache.StatusSuspect); err != nil {
		t.Fatal(err)
	}
	res, _ = s.handleListAssignments(ctx, mcplib.CallToolRequest{}, listAssignmentsArgs{Course: "CSC108"})
	if env := decodeEnvelope(t, res); env.Stale {
		t.Errorf("after backoff: %+v", env)
	}
	if m, _ := db.GetSyncMeta(cache.ResourceAssignments, 1); m.Status != cache.StatusSuccess {
		t.Errorf("after backoff status = %q, want success", m.Status)
	}
	if f.count("assignment_groups") != before+1 {
		t.Errorf("expected exactly one retry after the backoff window")
	}
}
