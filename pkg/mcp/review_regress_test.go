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
	"testing"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/chrismdemian/laurus/internal/cache"
	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// Regression tests from the 2026-09-06 review. Each one reproduced a real
// hole before its fix and is kept here so the hole cannot reopen.

// M2: the match offset used to be computed on strings.ToLower(text) and then
// used to slice the ORIGINAL text. Lower-casing can shrink a rune (İ, 2
// bytes, to i, 1 byte: silently wrong snippet) or grow one (Ⱥ, 2 bytes, to
// ⱥ, 3 bytes: index out of range panic). The snippet must come from the
// same string the search ran on and must contain the hit.
func TestSnippet_LowerCasingChangesByteLength(t *testing.T) {
	for _, tc := range []struct{ name, filler string }{
		{"shrinks", "İ"},
		{"grows", "Ⱥ"},
		{"ascii", "A"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panicked: %v", r)
				}
			}()
			text := strings.Repeat(tc.filler, 300) + " NEEDLE tail"
			idx := foldIndex(text, "needle")
			if idx < 0 || !strings.HasPrefix(text[idx:], "NEEDLE") {
				t.Fatalf("foldIndex = %d, does not point at the hit in the original text", idx)
			}
			out := snippet(text, idx)
			if !strings.Contains(out, "NEEDLE") {
				t.Errorf("snippet %q does not contain the hit", out)
			}
		})
	}
	if foldIndex("abc", "zzz") != -1 {
		t.Error("miss must return -1")
	}
	if foldIndex("straße", "STRASSE") != -1 {
		// Not a claim of full case folding: only per-rune lower-casing, the
		// same rule strings.ToLower applies. Documented, not a bug.
		t.Log("per-rune folding does not match ß against SS; expected")
	}
}

// twoCourseCanvas serves two courses; course 2's per-course endpoints fail
// so its data can never be cached.
func twoCourseCanvas(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case strings.HasSuffix(p, "/courses"):
			if r.URL.Query().Get("enrollment_state") == "completed" {
				fmt.Fprint(w, `[]`)
				return
			}
			fmt.Fprint(w, `[{"id":1,"name":"A","course_code":"AAA","workflow_state":"available"},{"id":2,"name":"B","course_code":"BBB","workflow_state":"available"}]`)
		case strings.Contains(p, "/courses/1/assignment_groups"):
			fmt.Fprint(w, `[{"id":10,"name":"HW","assignments":[{"id":101,"name":"PS1","course_id":1,"due_at":"2099-01-01T00:00:00Z"}]}]`)
		case strings.HasSuffix(p, "/announcements") && !strings.Contains(r.URL.RawQuery, "course_2"):
			fmt.Fprint(w, `[{"id":9,"title":"hi","context_code":"course_1"}]`)
		default:
			// course 2: assignment_groups and announcements both fail (400,
			// so the retrying client does not back off for seconds).
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"errors":[{"message":"boom"}]}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func serverFor(t *testing.T, srv *httptest.Server) *Server {
	t.Helper()
	db, err := cache.Open(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	factory := &cmdutil.Factory{
		Version: "test",
		Client:  func() (*canvas.Client, error) { return canvas.NewClient(srv.URL, "tok", "test"), nil },
		Cache:   func() (*cache.DB, error) { return db, nil },
	}
	return &Server{newClient: factory.Client, newCache: factory.Cache, config: factory.Config, version: "test"}
}

type aggregateEnv struct {
	Stale     bool   `json:"stale"`
	SyncError string `json:"sync_error"`
	Data      []any  `json:"data"`
}

func decodeAggregate(t *testing.T, res *mcplib.CallToolResult) aggregateEnv {
	t.Helper()
	tc := res.Content[0].(mcplib.TextContent)
	if res.IsError {
		t.Fatalf("tool error: %s", tc.Text)
	}
	var env aggregateEnv
	if err := json.Unmarshal([]byte(tc.Text), &env); err != nil {
		t.Fatalf("not an envelope: %v\n%s", err, tc.Text)
	}
	return env
}

// M1: a multi-course aggregate where one course has nothing cached and its
// refresh fails used to append a note and report stale=false. Missing data
// is stale data.
func TestListAssignments_AggregateStaleWhenOneCourseMissing(t *testing.T) {
	s := serverFor(t, twoCourseCanvas(t))
	res, err := s.handleListAssignments(context.Background(), mcplib.CallToolRequest{}, listAssignmentsArgs{})
	if err != nil {
		t.Fatal(err)
	}
	env := decodeAggregate(t, res)
	if env.SyncError == "" || !strings.Contains(env.SyncError, "BBB") {
		t.Fatalf("expected a sync_error naming BBB, got %q", env.SyncError)
	}
	if !env.Stale {
		t.Errorf("course BBB's data is missing (sync_error=%q) but stale=false", env.SyncError)
	}
	if len(env.Data) != 1 {
		t.Errorf("course AAA's rows must still be served: got %d", len(env.Data))
	}
}

func TestListAnnouncements_AggregateStaleWhenOneCourseMissing(t *testing.T) {
	s := serverFor(t, twoCourseCanvas(t))
	res, err := s.handleListAnnouncements(context.Background(), mcplib.CallToolRequest{}, listAnnouncementsArgs{})
	if err != nil {
		t.Fatal(err)
	}
	env := decodeAggregate(t, res)
	if env.SyncError == "" || !strings.Contains(env.SyncError, "BBB") {
		t.Fatalf("expected a sync_error naming BBB, got %q", env.SyncError)
	}
	if !env.Stale {
		t.Errorf("announcements aggregate: course BBB missing but stale=false")
	}
	if len(env.Data) != 1 {
		t.Errorf("course AAA's rows must still be served: got %d", len(env.Data))
	}
}

// M4: get_next_assignment's found case returned a bare object while every
// other read is enveloped.
func TestGetNextAssignment_FoundCaseIsEnveloped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/upcoming_events") {
			fmt.Fprint(w, `[{"title":"PS1","html_url":"u","assignment":{"id":1,"name":"PS1","due_at":"2099-01-01T00:00:00Z"}}]`)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	s := &Server{newClient: func() (*canvas.Client, error) { return canvas.NewClient(srv.URL, "tok", "test"), nil }}
	res, err := s.handleGetNextAssignment(context.Background(), mcplib.CallToolRequest{}, getNextAssignmentArgs{})
	if err != nil {
		t.Fatal(err)
	}
	tc := res.Content[0].(mcplib.TextContent)
	var env struct {
		Source string         `json:"source"`
		AsOf   time.Time      `json:"as_of"`
		Data   map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(tc.Text), &env); err != nil {
		t.Fatal(err)
	}
	if env.Source != sourceLive || env.AsOf.IsZero() || env.Data["name"] != "PS1" {
		t.Errorf("get_next_assignment found-case is not a live envelope: %s", tc.Text)
	}
}

// mutableCanvas lets a test add courses after the first sync.
type mutableCanvas struct {
	srv     *httptest.Server
	mu      sync.Mutex
	courses []string // JSON objects
}

func newMutableCanvas(t *testing.T) *mutableCanvas {
	t.Helper()
	m := &mutableCanvas{}
	m.addCourse(1, "AAA")
	m.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case strings.HasSuffix(p, "/courses"):
			if r.URL.Query().Get("enrollment_state") == "completed" {
				fmt.Fprint(w, `[]`)
				return
			}
			m.mu.Lock()
			fmt.Fprint(w, "["+strings.Join(m.courses, ",")+"]")
			m.mu.Unlock()
		case strings.Contains(p, "/assignment_groups"):
			var id int
			fmt.Sscanf(p, "/api/v1/courses/%d/", &id)
			fmt.Fprintf(w, `[{"id":%d,"name":"HW","assignments":[{"id":%d,"name":"PS-%d","course_id":%d}]}]`, id*10, id*100, id, id)
		default:
			fmt.Fprint(w, `[]`)
		}
	}))
	t.Cleanup(m.srv.Close)
	return m
}

func (m *mutableCanvas) addCourse(id int, code string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.courses = append(m.courses, fmt.Sprintf(`{"id":%d,"name":"%s","course_code":"%s","workflow_state":"available"}`, id, code, code))
}

// M3: the no-course aggregates enumerated courses on the 24-hour identity
// tier with no way to bypass it, so a new enrolment was invisible (with
// stale=false) for a day. Enumeration now uses the 5-minute tier and
// honours fresh.
func TestListAssignments_NewEnrolmentAppears(t *testing.T) {
	m := newMutableCanvas(t)
	s := serverFor(t, m.srv)
	ctx := context.Background()
	codes := func(res *mcplib.CallToolResult) []string {
		env := decodeAggregate(t, res)
		var out []string
		for _, d := range env.Data {
			out = append(out, d.(map[string]any)["course_name"].(string))
		}
		return out
	}
	res, _ := s.handleListAssignments(ctx, mcplib.CallToolRequest{}, listAssignmentsArgs{})
	if got := codes(res); len(got) != 1 || got[0] != "AAA" {
		t.Fatalf("initial: %v", got)
	}

	// Enrol in BBB, then age the course list past the 5-minute tier (but
	// well inside the 24-hour identity tier the old code used).
	m.addCourse(2, "BBB")
	db, _ := s.getCache()
	meta, _ := db.GetSyncMeta(cache.ResourceCourses, 0)
	if err := db.SetSyncMetaAt(cache.ResourceCourses, 0, time.Now().Add(-tierGrade-time.Second), meta.ItemCount, cache.StatusSuccess); err != nil {
		t.Fatal(err)
	}
	res, _ = s.handleListAssignments(ctx, mcplib.CallToolRequest{}, listAssignmentsArgs{})
	if got := codes(res); len(got) != 2 {
		t.Errorf("after the 5-minute tier: %v, want AAA and BBB", got)
	}

	// Enrol in CCC; fresh=true must show it at once.
	m.addCourse(3, "CCC")
	res, _ = s.handleListAssignments(ctx, mcplib.CallToolRequest{}, listAssignmentsArgs{Fresh: true})
	if got := codes(res); len(got) != 3 {
		t.Errorf("fresh after a third enrolment: %v, want three courses", got)
	}
}

// Minor: a refresh is shared by every caller in a burst, so the first
// caller's context ending must not cancel it and record a context-canceled
// failure (and a 2-minute backoff) for everyone. The caller returns with its
// own error; the refresh completes detached.
func TestRefresh_CallerCancellationDoesNotPoisonBackoff(t *testing.T) {
	f := newFakeCanvas(t)
	f.delay = 300 * time.Millisecond
	s := newTestServer(t, f)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	var rows []canvas.Assignment
	_, err := s.read(ctx, readSpec{rt: cache.ResourceAssignments, courseID: 1, ttl: tierGrade}, &rows, nil)
	if err == nil || !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("impatient caller: err=%v, want its own deadline error", err)
	}

	// Let the detached refresh finish.
	deadline := time.Now().Add(3 * time.Second)
	db, _ := s.getCache()
	var meta cache.SyncMeta
	for time.Now().Before(deadline) {
		meta, _ = db.GetSyncMeta(cache.ResourceAssignments, 1)
		if meta.Status == cache.StatusSuccess {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if meta.Status != cache.StatusSuccess || meta.Error != "" {
		t.Fatalf("detached refresh did not complete cleanly: %+v", meta)
	}

	// A patient caller is served from the completed refresh without a
	// re-fetch and without a backoff-induced stale flag.
	before := f.count("assignment_groups")
	env, err := s.read(context.Background(), readSpec{rt: cache.ResourceAssignments, courseID: 1, ttl: tierGrade}, &rows, nil)
	if err != nil || env.Stale || env.SyncError != "" || len(rows) != 1 {
		t.Errorf("patient caller: env=%+v rows=%d err=%v", env, len(rows), err)
	}
	if f.count("assignment_groups") != before {
		t.Errorf("patient caller re-fetched; the detached refresh's result was not used")
	}
}
