package syncer

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chrismdemian/laurus/internal/cache"
	"github.com/chrismdemian/laurus/internal/canvas"
)

// fakeCanvas serves just enough of the API for the jobs: assignment groups
// (with a truncated second page), announcements, pages (403), modules.
func fakeCanvas(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case strings.HasSuffix(p, "/assignment_groups"):
			// Canvas bug: next advertised with an empty URL.
			w.Header().Set("Link", `<>; rel="next"`)
			fmt.Fprint(w, `[{"id":10,"name":"HW","assignments":[{"id":101,"name":"A1","submission":{"id":501}},{"id":102,"name":"A2"}]}]`)
		case strings.HasSuffix(p, "/announcements"):
			fmt.Fprint(w, `[{"id":7,"title":"Welcome","context_code":"course_1"}]`)
		case strings.HasSuffix(p, "/pages"):
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"errors":[{"message":"forbidden"}]}`)
		case strings.HasSuffix(p, "/modules"):
			fmt.Fprint(w, `[{"id":3,"name":"Week 1","items":[{"id":31,"title":"Slides"},{"id":32,"title":"Reading"}]}]`)
		case strings.HasSuffix(p, "/discussion_topics"), strings.HasSuffix(p, "/files"), strings.HasSuffix(p, "/folders"):
			fmt.Fprint(w, `[]`)
		case p == "/api/v1/courses":
			fmt.Fprint(w, `[{"id":1,"name":"Intro","course_code":"CSC108"}]`)
		default:
			http.NotFound(w, r)
		}
	}))
}

func testDB(t *testing.T) *cache.DB {
	t.Helper()
	db, err := cache.Open(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestJobTablesAndJobFor(t *testing.T) {
	if JobFor(cache.ResourceSubmissions) != JobAssignmentGroups || JobFor(cache.ResourceModuleItems) != JobModules {
		t.Error("derived tables must map to the job that fills them")
	}
	if JobFor(cache.ResourceConversations) != "" || JobFor(cache.ResourceCalendarEvents) != "" {
		t.Error("resources with no sync path must report no job")
	}
	seen := map[cache.ResourceType]bool{}
	for _, job := range append([]Job{JobCourses}, CourseJobs...) {
		for _, tbl := range Tables(job) {
			if seen[tbl] {
				t.Errorf("table %s owned by two jobs", tbl)
			}
			seen[tbl] = true
		}
	}
}

func TestSyncJob_FillsTablesAndMarksSuspect(t *testing.T) {
	srv := fakeCanvas(t)
	defer srv.Close()
	client := canvas.NewClient(srv.URL, "tok", "test")
	db := testDB(t)
	ctx := context.Background()

	// Assignment groups: truncated fetch -> all three tables stored, suspect.
	r := SyncJob(ctx, client, db, JobAssignmentGroups, 1)
	if r.Status != cache.StatusSuspect || !errors.Is(r.Err, canvas.ErrPaginationTruncated) {
		t.Fatalf("assignment_groups: status=%s err=%v", r.Status, r.Err)
	}
	if r.Count != 1+2+1 {
		t.Errorf("count = %d, want 4 (1 group, 2 assignments, 1 submission)", r.Count)
	}
	for _, tbl := range Tables(JobAssignmentGroups) {
		meta, err := db.GetSyncMeta(tbl, 1)
		if err != nil || meta.Status != cache.StatusSuspect {
			t.Errorf("%s meta = %+v, %v; want suspect", tbl, meta, err)
		}
	}
	var subs []map[string]any
	if _, err := db.ListFresh(cache.ResourceSubmissions, 1, &subs); err != nil || len(subs) != 1 {
		t.Errorf("submissions = %d, %v", len(subs), err)
	}

	// Announcements: clean fetch -> success.
	r = SyncJob(ctx, client, db, JobAnnouncements, 1)
	if r.Status != cache.StatusSuccess || r.Err != nil || r.Count != 1 {
		t.Errorf("announcements: %+v", r)
	}

	// Pages: 403 -> skipped, recorded, not an error.
	r = SyncJob(ctx, client, db, JobPages, 1)
	if r.Status != cache.StatusSkipped || r.Err != nil {
		t.Errorf("pages: %+v", r)
	}
	if meta, _ := db.GetSyncMeta(cache.ResourcePages, 1); meta.Status != cache.StatusSkipped {
		t.Errorf("pages meta = %+v", meta)
	}

	// Modules: two tables from one call.
	r = SyncJob(ctx, client, db, JobModules, 1)
	if r.Status != cache.StatusSuccess || r.Count != 3 {
		t.Errorf("modules: %+v", r)
	}
	var items []map[string]any
	if _, err := db.ListFresh(cache.ResourceModuleItems, 1, &items); err != nil || len(items) != 2 {
		t.Errorf("module_items = %d, %v", len(items), err)
	}

	// SyncResource maps a derived table to its job.
	r = SyncResource(ctx, client, db, cache.ResourceModuleItems, 1)
	if r.Job != JobModules || r.Status != cache.StatusSuccess {
		t.Errorf("SyncResource(module_items) = %+v", r)
	}
	if r := SyncResource(ctx, client, db, cache.ResourceConversations, 0); !errors.Is(r.Err, ErrNoJob) {
		t.Errorf("SyncResource(conversations) err = %v, want ErrNoJob", r.Err)
	}
}

func TestSyncAll_CollectsResults(t *testing.T) {
	srv := fakeCanvas(t)
	defer srv.Close()
	client := canvas.NewClient(srv.URL, "tok", "test")
	db := testDB(t)
	ctx := context.Background()

	courses, res := SyncCourses(ctx, client, db)
	if res.Status != cache.StatusSuccess || len(courses) != 1 {
		t.Fatalf("SyncCourses: %+v, %d courses", res, len(courses))
	}
	var n int
	sum := SyncAll(ctx, client, db, courses, Options{OnResult: func(Result) { n++ }})
	if len(sum.Results) != len(CourseJobs) || n != len(CourseJobs) {
		t.Fatalf("results = %d, callbacks = %d, want %d", len(sum.Results), n, len(CourseJobs))
	}
	errs := sum.Errors()
	if len(errs) != 1 || !strings.Contains(errs[0], "CSC108/assignment_groups") {
		t.Errorf("errors = %v, want exactly the truncated assignment_groups job", errs)
	}
	if sum.Items() != 1+2+1+1+3 {
		t.Errorf("items = %d", sum.Items())
	}
}
