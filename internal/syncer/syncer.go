// Package syncer fills the local cache from Canvas in per-(job, course)
// units that both the CLI (`laurus sync`) and the MCP server can call.
//
// The unit is a job, not a table: one Canvas call for assignment groups
// fills assignment_groups, assignments and submissions, and one modules
// call fills modules and module_items. A job replaces all of its tables
// with the same truncation flag, so a partial page can never prune any of
// them (see cache.ReplaceAll).
package syncer

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/chrismdemian/laurus/internal/cache"
	"github.com/chrismdemian/laurus/internal/canvas"
)

// Job names one fetch unit.
type Job string

const (
	JobCourses          Job = "courses"
	JobAssignmentGroups Job = "assignment_groups" // + assignments, submissions
	JobAnnouncements    Job = "announcements"
	JobDiscussions      Job = "discussions"
	JobModules          Job = "modules" // + module_items
	JobPages            Job = "pages"
	JobFiles            Job = "files"
	JobFolders          Job = "folders"
)

// CourseJobs lists every per-course job in the order the CLI reports them.
var CourseJobs = []Job{
	JobAssignmentGroups, JobAnnouncements, JobDiscussions,
	JobModules, JobPages, JobFiles, JobFolders,
}

// jobTables maps a job to the tables it owns and replaces together.
var jobTables = map[Job][]cache.ResourceType{
	JobCourses:          {cache.ResourceCourses},
	JobAssignmentGroups: {cache.ResourceAssignmentGroups, cache.ResourceAssignments, cache.ResourceSubmissions},
	JobAnnouncements:    {cache.ResourceAnnouncements},
	JobDiscussions:      {cache.ResourceDiscussions},
	JobModules:          {cache.ResourceModules, cache.ResourceModuleItems},
	JobPages:            {cache.ResourcePages},
	JobFiles:            {cache.ResourceFiles},
	JobFolders:          {cache.ResourceFolders},
}

// Tables returns the cache tables a job owns.
func Tables(job Job) []cache.ResourceType { return jobTables[job] }

// JobFor returns the job that fills a resource type, or "" if none does.
// Resources with no sync path (conversations, calendar events, enrollments,
// grading standards) return "" and must be served live.
func JobFor(rt cache.ResourceType) Job {
	for job, tables := range jobTables {
		for _, t := range tables {
			if t == rt {
				return job
			}
		}
	}
	return ""
}

// Result describes one job run.
type Result struct {
	Job        Job
	CourseID   int64
	CourseCode string
	Count      int    // items stored (all tables of the job)
	Status     string // cache.Status*: success, suspect, skipped, failed
	Err        error  // non-nil for failed and suspect (ErrPaginationTruncated)
}

// ErrNoJob is returned by SyncResource for a resource nothing syncs.
var ErrNoJob = errors.New("syncer: resource has no sync job")

// SyncResource fills the job that owns rt for courseID (0 for courses).
func SyncResource(ctx context.Context, client *canvas.Client, db *cache.DB, rt cache.ResourceType, courseID int64) Result {
	job := JobFor(rt)
	if job == "" {
		return Result{CourseID: courseID, Status: cache.StatusFailed, Err: fmt.Errorf("%w: %s", ErrNoJob, rt)}
	}
	if job == JobCourses {
		_, res := SyncCourses(ctx, client, db)
		return res
	}
	return SyncJob(ctx, client, db, job, courseID)
}

// SyncCourses replaces the cached course list with the user's active AND
// completed courses (completed ones keep grade history readable offline),
// merged by ID and pruned within that fetched set. It returns the courses
// so callers can fan out per-course jobs; use ActiveCourses to pick the
// ones worth syncing. A truncated list is refused outright: syncing (and
// pruning) against half a root set is worse than not syncing.
func SyncCourses(ctx context.Context, client *canvas.Client, db *cache.DB) ([]canvas.Course, Result) {
	res := Result{Job: JobCourses}
	include := []string{"enrollments", "total_scores"}
	queries := []canvas.CourseListOptions{
		{EnrollmentState: "active", Include: include},
		{EnrollmentState: "completed", Include: include},
	}
	seen := map[int64]bool{}
	var courses []canvas.Course
	for _, q := range queries {
		for c, err := range canvas.ListCourses(ctx, client, q) {
			if err != nil {
				res.Status, res.Err = cache.StatusFailed, fmt.Errorf("listing courses: %w", err)
				_ = db.RecordSyncFailure(cache.ResourceCourses, 0, res.Err)
				return nil, res
			}
			if seen[c.ID] {
				continue
			}
			seen[c.ID] = true
			courses = append(courses, c)
		}
	}
	items := make([]cache.CacheItem, len(courses))
	for i, c := range courses {
		items[i] = cache.CacheItem{ID: c.ID, CourseID: 0, Data: c}
	}
	status, err := db.ReplaceAll(cache.ResourceCourses, 0, items, cache.ReplaceOptions{})
	if err != nil {
		res.Status, res.Err = cache.StatusFailed, fmt.Errorf("caching courses: %w", err)
		return nil, res
	}
	res.Status, res.Count = status, len(courses)
	return courses, res
}

// fetch collects one paginated listing into cache items, reporting
// truncation separately from hard errors.
type fetched struct {
	items     []cache.CacheItem
	truncated bool
	skipped   bool // endpoint disabled (403/404): record "skipped", keep going
}

// SyncJob runs one per-course job and replaces its tables.
func SyncJob(ctx context.Context, client *canvas.Client, db *cache.DB, job Job, courseID int64) Result {
	res := Result{Job: job, CourseID: courseID}
	tables := jobTables[job]
	if len(tables) == 0 || job == JobCourses {
		res.Status, res.Err = cache.StatusFailed, fmt.Errorf("%w: %s", ErrNoJob, job)
		return res
	}

	perTable, err := fetchJob(ctx, client, job, courseID)
	if err != nil {
		res.Status, res.Err = cache.StatusFailed, err
		for _, t := range tables {
			if rerr := db.RecordSyncFailure(t, courseID, err); rerr != nil {
				res.Err = fmt.Errorf("%w (and recording the failure: %v)", err, rerr)
			}
		}
		return res
	}

	var truncated, skipped bool
	for _, f := range perTable {
		truncated = truncated || f.truncated
		skipped = skipped || f.skipped
	}
	if skipped {
		for _, t := range tables {
			if err := db.SetSyncMeta(t, courseID, 0, cache.StatusSkipped); err != nil {
				res.Status, res.Err = cache.StatusFailed, fmt.Errorf("recording skip: %w", err)
				return res
			}
		}
		res.Status = cache.StatusSkipped
		return res
	}

	opts := cache.ReplaceOptions{Truncated: truncated}
	status := cache.StatusSuccess
	for _, t := range tables {
		st, err := db.ReplaceAll(t, courseID, perTable[t].items, opts)
		if err != nil {
			// Recorded like a fetch failure so readers back off instead of
			// re-fetching Canvas on every call while the cache is contended.
			res.Status, res.Err = cache.StatusFailed, fmt.Errorf("caching %s: %w", t, err)
			for _, rt := range tables {
				if rerr := db.RecordSyncFailure(rt, courseID, res.Err); rerr != nil {
					res.Err = fmt.Errorf("%w (and recording the failure: %v)", res.Err, rerr)
				}
			}
			return res
		}
		if st == cache.StatusSuspect {
			status = cache.StatusSuspect
		}
		res.Count += len(perTable[t].items)
	}
	res.Status = status
	if truncated {
		res.Err = fmt.Errorf("%w; cached set kept, marked suspect", canvas.ErrPaginationTruncated)
	}
	return res
}

// fetchJob performs the job's Canvas call(s) and buckets items per table.
func fetchJob(ctx context.Context, client *canvas.Client, job Job, courseID int64) (map[cache.ResourceType]*fetched, error) {
	out := map[cache.ResourceType]*fetched{}
	for _, t := range jobTables[job] {
		out[t] = &fetched{}
	}

	switch job {
	case JobAssignmentGroups:
		groups, assignments, submissions := out[cache.ResourceAssignmentGroups], out[cache.ResourceAssignments], out[cache.ResourceSubmissions]
		for ag, err := range canvas.ListAssignmentGroups(ctx, client, courseID, []string{"assignments", "submission"}) {
			if err != nil {
				if errors.Is(err, canvas.ErrPaginationTruncated) {
					groups.truncated = true
					break
				}
				return nil, err
			}
			groups.items = append(groups.items, cache.CacheItem{ID: ag.ID, CourseID: courseID, Data: ag})
			for _, a := range ag.Assignments {
				assignments.items = append(assignments.items, cache.CacheItem{ID: a.ID, CourseID: courseID, Data: a})
				if a.Submission != nil {
					submissions.items = append(submissions.items, cache.CacheItem{ID: a.Submission.ID, CourseID: courseID, Data: a.Submission})
				}
			}
		}

	case JobAnnouncements:
		f := out[cache.ResourceAnnouncements]
		for a, err := range canvas.ListAnnouncements(ctx, client, canvas.ListAnnouncementsOptions{
			ContextCodes: []string{fmt.Sprintf("course_%d", courseID)},
			StartDate:    "2000-01-01",
		}) {
			if err != nil {
				if errors.Is(err, canvas.ErrPaginationTruncated) {
					f.truncated = true
					break
				}
				return nil, err
			}
			f.items = append(f.items, cache.CacheItem{ID: a.ID, CourseID: courseID, Data: a})
		}

	case JobDiscussions:
		f := out[cache.ResourceDiscussions]
		for d, err := range canvas.ListDiscussionTopics(ctx, client, courseID, canvas.ListDiscussionTopicsOptions{}) {
			if err != nil {
				if errors.Is(err, canvas.ErrPaginationTruncated) {
					f.truncated = true
					break
				}
				return nil, err
			}
			f.items = append(f.items, cache.CacheItem{ID: d.ID, CourseID: courseID, Data: d})
		}

	case JobModules:
		mods, items := out[cache.ResourceModules], out[cache.ResourceModuleItems]
		for m, err := range canvas.ListModules(ctx, client, courseID, canvas.ListModulesOptions{
			IncludeItems:          true,
			IncludeContentDetails: true,
		}) {
			if err != nil {
				if errors.Is(err, canvas.ErrPaginationTruncated) {
					mods.truncated = true
					break
				}
				return nil, err
			}
			mods.items = append(mods.items, cache.CacheItem{ID: m.ID, CourseID: courseID, Data: m})
			for _, it := range m.Items {
				items.items = append(items.items, cache.CacheItem{ID: it.ID, CourseID: courseID, Data: it})
			}
		}

	case JobPages:
		f := out[cache.ResourcePages]
		for p, err := range canvas.ListPages(ctx, client, courseID, canvas.ListPagesOptions{}) {
			if err != nil {
				if errors.Is(err, canvas.ErrNotFound) || errors.Is(err, canvas.ErrForbidden) {
					f.skipped = true
					break
				}
				if errors.Is(err, canvas.ErrPaginationTruncated) {
					f.truncated = true
					break
				}
				return nil, err
			}
			f.items = append(f.items, cache.CacheItem{ID: p.PageID, CourseID: courseID, Data: p})
		}

	case JobFiles:
		f := out[cache.ResourceFiles]
		for file, err := range canvas.ListFiles(ctx, client, courseID, canvas.ListFilesOptions{}) {
			if err != nil {
				if errors.Is(err, canvas.ErrForbidden) || errors.Is(err, canvas.ErrNotFound) {
					f.skipped = true
					break
				}
				if errors.Is(err, canvas.ErrPaginationTruncated) {
					f.truncated = true
					break
				}
				return nil, err
			}
			f.items = append(f.items, cache.CacheItem{ID: file.ID, CourseID: courseID, Data: file})
		}

	case JobFolders:
		f := out[cache.ResourceFolders]
		for folder, err := range canvas.ListFolders(ctx, client, courseID) {
			if err != nil {
				if errors.Is(err, canvas.ErrForbidden) || errors.Is(err, canvas.ErrNotFound) {
					f.skipped = true
					break
				}
				if errors.Is(err, canvas.ErrPaginationTruncated) {
					f.truncated = true
					break
				}
				return nil, err
			}
			f.items = append(f.items, cache.CacheItem{ID: folder.ID, CourseID: courseID, Data: folder})
		}

	default:
		return nil, fmt.Errorf("%w: %s", ErrNoJob, job)
	}
	return out, nil
}

// Options tunes SyncAll.
type Options struct {
	Parallel int   // concurrent courses; 0 means 3
	Jobs     []Job // per-course jobs to run; nil means CourseJobs
	OnResult func(Result)
}

// Summary is what SyncAll returns.
type Summary struct {
	Results []Result
	Elapsed time.Duration
}

// Errors lists the non-success results as "CODE/job: err" strings.
func (s Summary) Errors() []string {
	var out []string
	for _, r := range s.Results {
		if r.Err != nil {
			out = append(out, fmt.Sprintf("%s/%s: %v", r.CourseCode, r.Job, r.Err))
		}
	}
	return out
}

// Items sums the stored item counts.
func (s Summary) Items() int {
	n := 0
	for _, r := range s.Results {
		n += r.Count
	}
	return n
}

// SyncAll runs the per-course jobs for every course with bounded
// parallelism. Individual job failures are collected, never fatal.
func SyncAll(ctx context.Context, client *canvas.Client, db *cache.DB, courses []canvas.Course, opts Options) Summary {
	start := time.Now()
	parallel := opts.Parallel
	if parallel <= 0 {
		parallel = 3
	}
	jobs := opts.Jobs
	if jobs == nil {
		jobs = CourseJobs
	}

	results := make(chan Result, len(courses)*len(jobs))
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(parallel)
	for _, course := range courses {
		course := course
		g.Go(func() error {
			code := CourseLabel(course)
			for _, job := range jobs {
				r := SyncJob(ctx, client, db, job, course.ID)
				r.CourseCode = code
				results <- r
			}
			return nil
		})
	}
	_ = g.Wait()
	close(results)

	var sum Summary
	for r := range results {
		sum.Results = append(sum.Results, r)
		if opts.OnResult != nil {
			opts.OnResult(r)
		}
	}
	sum.Elapsed = time.Since(start)
	return sum
}

// ActiveCourses filters to courses still running: workflow_state
// "available" (Canvas marks concluded ones "completed") and no end date
// in the past. Per-course jobs run for these only.
func ActiveCourses(courses []canvas.Course) []canvas.Course {
	now := time.Now()
	var out []canvas.Course
	for _, c := range courses {
		if c.WorkflowState == "completed" {
			continue
		}
		if c.EndAt != nil && c.EndAt.Before(now) {
			continue
		}
		out = append(out, c)
	}
	return out
}

// CourseLabel is the code used in reports, falling back to the numeric ID.
func CourseLabel(c canvas.Course) string {
	if c.CourseCode != "" {
		return c.CourseCode
	}
	return strconv.FormatInt(c.ID, 10)
}
