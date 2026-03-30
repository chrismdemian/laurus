// Package sync implements the sync command for populating the local cache.
package sync

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/chrismdemian/laurus/internal/cache"
	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

type syncResult struct {
	courseCode string
	resource   string
	count      int
	err        error
}

// NewCmdSync returns the sync command.
func NewCmdSync(f *cmdutil.Factory) *cobra.Command {
	var status bool

	cmd := &cobra.Command{
		Use:   "sync [course]",
		Short: "Sync Canvas data to local cache",
		Long:  "Download course data for offline access. Syncs all active courses by default.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if status {
				return statusRun(f)
			}
			var courseQuery string
			if len(args) > 0 {
				courseQuery = args[0]
			}
			return syncRun(f, courseQuery)
		},
	}

	cmd.Flags().BoolVar(&status, "status", false, "Show sync status and cache metadata")

	cmd.AddCommand(newCmdSyncFiles(f))

	return cmd
}

func syncRun(f *cmdutil.Factory, courseQuery string) error {
	client, err := f.Client()
	if err != nil {
		return err
	}
	db, err := f.Cache()
	if err != nil {
		return err
	}
	ios := f.IOStreams()
	ctx := context.Background()
	start := time.Now()

	// Fetch active courses.
	var courses []canvas.Course
	for c, err := range canvas.ListCourses(ctx, client, canvas.CourseListOptions{
		EnrollmentState: "active",
		Include:         []string{"enrollments", "total_scores"},
	}) {
		if err != nil {
			return fmt.Errorf("listing courses: %w", err)
		}
		courses = append(courses, c)
	}

	// Cache courses.
	items := make([]cache.CacheItem, len(courses))
	for i, c := range courses {
		items[i] = cache.CacheItem{ID: c.ID, CourseID: 0, Data: c}
	}
	if err := db.UpsertMany(cache.ResourceCourses, items); err != nil {
		return fmt.Errorf("caching courses: %w", err)
	}
	_ = db.SetSyncMeta(cache.ResourceCourses, 0, len(courses), "success")
	_, _ = fmt.Fprintf(ios.ErrOut, "  %-12s  %-20s  %d items\n", "all", "courses", len(courses))

	// Filter to specific course if requested.
	if courseQuery != "" {
		course, err := canvas.FindCourse(ctx, client, courseQuery)
		if err != nil {
			return fmt.Errorf("finding course %q: %w", courseQuery, err)
		}
		courses = []canvas.Course{course}
	}

	// Sync per-course data with bounded parallelism.
	var totalItems int
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(3)

	results := make(chan syncResult, len(courses)*7)

	for _, course := range courses {
		course := course // capture
		g.Go(func() error {
			return syncCourse(ctx, client, db, course, results)
		})
	}

	// Wait for all goroutines then close channel.
	// syncCourse never returns errors (errors go into the channel as syncResult.err),
	// so waitErr will be nil. We capture it for safety if that changes.
	var waitErr error
	go func() {
		waitErr = g.Wait()
		close(results)
	}()

	var syncErrors []string
	for r := range results {
		if r.err != nil {
			syncErrors = append(syncErrors, fmt.Sprintf("%s/%s: %v", r.courseCode, r.resource, r.err))
			continue
		}
		totalItems += r.count
		_, _ = fmt.Fprintf(ios.ErrOut, "  %-12s  %-20s  %d items\n", r.courseCode, r.resource, r.count)
	}

	if waitErr != nil {
		return waitErr
	}

	totalItems += len(courses) // include courses themselves
	elapsed := time.Since(start).Round(100 * time.Millisecond)
	_, _ = fmt.Fprintf(ios.ErrOut, "\nSynced %d courses in %s (%d items total)\n", len(courses), elapsed, totalItems)

	if len(syncErrors) > 0 {
		_, _ = fmt.Fprintf(ios.ErrOut, "\nWarnings (%d):\n", len(syncErrors))
		for _, e := range syncErrors {
			_, _ = fmt.Fprintf(ios.ErrOut, "  %s\n", e)
		}
	}

	return nil
}

func syncCourse(ctx context.Context, client *canvas.Client, db *cache.DB, course canvas.Course, results chan<- syncResult) error {
	code := course.CourseCode
	if code == "" {
		code = strconv.FormatInt(course.ID, 10)
	}

	// Assignment groups (includes assignments + submissions).
	syncResource(ctx, results, code, "assignment_groups", func() (int, error) {
		var groups []cache.CacheItem
		var assignments []cache.CacheItem
		var submissions []cache.CacheItem

		for ag, err := range canvas.ListAssignmentGroups(ctx, client, course.ID, []string{"assignments", "submission"}) {
			if err != nil {
				return 0, err
			}
			groups = append(groups, cache.CacheItem{ID: ag.ID, CourseID: course.ID, Data: ag})

			for _, a := range ag.Assignments {
				assignments = append(assignments, cache.CacheItem{ID: a.ID, CourseID: course.ID, Data: a})
				if a.Submission != nil {
					submissions = append(submissions, cache.CacheItem{
						ID: a.Submission.ID, CourseID: course.ID, Data: a.Submission,
					})
				}
			}
		}

		if err := db.UpsertMany(cache.ResourceAssignmentGroups, groups); err != nil {
			return 0, err
		}
		_ = db.SetSyncMeta(cache.ResourceAssignmentGroups, course.ID, len(groups), "success")

		if err := db.UpsertMany(cache.ResourceAssignments, assignments); err != nil {
			return 0, err
		}
		_ = db.SetSyncMeta(cache.ResourceAssignments, course.ID, len(assignments), "success")

		// Prune assignments that no longer exist.
		aIDs := make([]int64, len(assignments))
		for i, a := range assignments {
			aIDs[i] = a.ID
		}
		_ = db.Prune(cache.ResourceAssignments, course.ID, aIDs)

		if len(submissions) > 0 {
			if err := db.UpsertMany(cache.ResourceSubmissions, submissions); err != nil {
				return 0, err
			}
			_ = db.SetSyncMeta(cache.ResourceSubmissions, course.ID, len(submissions), "success")
		}

		return len(groups) + len(assignments) + len(submissions), nil
	})

	// Announcements.
	syncResource(ctx, results, code, "announcements", func() (int, error) {
		var items []cache.CacheItem
		contextCode := fmt.Sprintf("course_%d", course.ID)
		for a, err := range canvas.ListAnnouncements(ctx, client, canvas.ListAnnouncementsOptions{
			ContextCodes: []string{contextCode},
			StartDate:    "2000-01-01",
		}) {
			if err != nil {
				return 0, err
			}
			courseID := parseCourseIDFromContextCode(a.ContextCode)
			items = append(items, cache.CacheItem{ID: a.ID, CourseID: courseID, Data: a})
		}

		if err := db.UpsertMany(cache.ResourceAnnouncements, items); err != nil {
			return 0, err
		}
		_ = db.SetSyncMeta(cache.ResourceAnnouncements, course.ID, len(items), "success")
		return len(items), nil
	})

	// Discussion topics.
	syncResource(ctx, results, code, "discussions", func() (int, error) {
		var items []cache.CacheItem
		for d, err := range canvas.ListDiscussionTopics(ctx, client, course.ID, canvas.ListDiscussionTopicsOptions{}) {
			if err != nil {
				return 0, err
			}
			items = append(items, cache.CacheItem{ID: d.ID, CourseID: course.ID, Data: d})
		}

		if err := db.UpsertMany(cache.ResourceDiscussions, items); err != nil {
			return 0, err
		}
		_ = db.SetSyncMeta(cache.ResourceDiscussions, course.ID, len(items), "success")
		return len(items), nil
	})

	// Modules (with items + content details).
	syncResource(ctx, results, code, "modules", func() (int, error) {
		var mods []cache.CacheItem
		var modItems []cache.CacheItem

		for m, err := range canvas.ListModules(ctx, client, course.ID, canvas.ListModulesOptions{
			IncludeItems:          true,
			IncludeContentDetails: true,
		}) {
			if err != nil {
				return 0, err
			}
			mods = append(mods, cache.CacheItem{ID: m.ID, CourseID: course.ID, Data: m})
			for _, item := range m.Items {
				modItems = append(modItems, cache.CacheItem{ID: item.ID, CourseID: course.ID, Data: item})
			}
		}

		if err := db.UpsertMany(cache.ResourceModules, mods); err != nil {
			return 0, err
		}
		_ = db.SetSyncMeta(cache.ResourceModules, course.ID, len(mods), "success")

		if len(modItems) > 0 {
			if err := db.UpsertMany(cache.ResourceModuleItems, modItems); err != nil {
				return 0, err
			}
			_ = db.SetSyncMeta(cache.ResourceModuleItems, course.ID, len(modItems), "success")
		}

		return len(mods) + len(modItems), nil
	})

	// Pages (handle 404/403 gracefully — Pages tab may be disabled).
	syncResource(ctx, results, code, "pages", func() (int, error) {
		var items []cache.CacheItem
		for p, err := range canvas.ListPages(ctx, client, course.ID, canvas.ListPagesOptions{}) {
			if err != nil {
				if errors.Is(err, canvas.ErrNotFound) || errors.Is(err, canvas.ErrForbidden) {
					_ = db.SetSyncMeta(cache.ResourcePages, course.ID, 0, "skipped")
					return 0, nil // not an error — just disabled
				}
				return 0, err
			}
			items = append(items, cache.CacheItem{ID: p.PageID, CourseID: course.ID, Data: p})
		}

		if err := db.UpsertMany(cache.ResourcePages, items); err != nil {
			return 0, err
		}
		_ = db.SetSyncMeta(cache.ResourcePages, course.ID, len(items), "success")
		return len(items), nil
	})

	// Files metadata (handle 403 gracefully — Files tab may be restricted).
	syncResource(ctx, results, code, "files", func() (int, error) {
		var fileItems []cache.CacheItem
		for f, err := range canvas.ListFiles(ctx, client, course.ID, canvas.ListFilesOptions{}) {
			if err != nil {
				if errors.Is(err, canvas.ErrForbidden) {
					_ = db.SetSyncMeta(cache.ResourceFiles, course.ID, 0, "skipped")
					return 0, nil
				}
				return 0, err
			}
			fileItems = append(fileItems, cache.CacheItem{ID: f.ID, CourseID: course.ID, Data: f})
		}

		if err := db.UpsertMany(cache.ResourceFiles, fileItems); err != nil {
			return 0, err
		}
		_ = db.SetSyncMeta(cache.ResourceFiles, course.ID, len(fileItems), "success")
		return len(fileItems), nil
	})

	// Folders (handle 403 gracefully).
	syncResource(ctx, results, code, "folders", func() (int, error) {
		var items []cache.CacheItem
		for f, err := range canvas.ListFolders(ctx, client, course.ID) {
			if err != nil {
				if errors.Is(err, canvas.ErrForbidden) {
					_ = db.SetSyncMeta(cache.ResourceFolders, course.ID, 0, "skipped")
					return 0, nil
				}
				return 0, err
			}
			items = append(items, cache.CacheItem{ID: f.ID, CourseID: course.ID, Data: f})
		}

		if err := db.UpsertMany(cache.ResourceFolders, items); err != nil {
			return 0, err
		}
		_ = db.SetSyncMeta(cache.ResourceFolders, course.ID, len(items), "success")
		return len(items), nil
	})

	return nil
}

// syncResource runs a sync function and sends the result to the channel.
// Non-fatal errors (403, 404) are reported as warnings, not failures.
func syncResource(_ context.Context, results chan<- syncResult, courseCode, resource string, fn func() (int, error)) {
	count, err := fn()
	results <- syncResult{
		courseCode: courseCode,
		resource:   resource,
		count:      count,
		err:        err,
	}
}

// parseCourseIDFromContextCode extracts the course ID from "course_123".
func parseCourseIDFromContextCode(code string) int64 {
	parts := strings.SplitN(code, "_", 2)
	if len(parts) == 2 {
		id, _ := strconv.ParseInt(parts[1], 10, 64)
		return id
	}
	return 0
}

func statusRun(f *cmdutil.Factory) error {
	db, err := f.Cache()
	if err != nil {
		return err
	}
	ios := f.IOStreams()

	counts, fileSize, err := db.Stats()
	if err != nil {
		return fmt.Errorf("reading cache stats: %w", err)
	}

	metas, err := db.AllSyncMeta()
	if err != nil {
		return fmt.Errorf("reading sync metadata: %w", err)
	}

	if len(metas) == 0 {
		_, _ = fmt.Fprintln(ios.Out, "Cache is empty. Run 'laurus sync' to populate.")
		return nil
	}

	_, _ = fmt.Fprintf(ios.Out, "Cache: %s\n\n", cmdutil.FormatFileSize(fileSize))

	tbl := cmdutil.NewTable(ios)
	tbl.AddHeader("RESOURCE", "COURSE", "ITEMS", "LAST SYNC", "STATUS")

	for _, m := range metas {
		courseStr := "all"
		if m.CourseID > 0 {
			courseStr = strconv.FormatInt(m.CourseID, 10)
		}
		lastSync := "never"
		if !m.LastSyncAt.IsZero() {
			lastSync = cmdutil.RelativeTime(m.LastSyncAt)
		}
		tbl.AddRow(string(m.ResourceType), courseStr, strconv.Itoa(m.ItemCount), lastSync, m.Status)
	}

	_ = ios.StartPager()
	defer ios.StopPager()
	if err := tbl.Render(); err != nil {
		return err
	}

	if len(counts) > 0 {
		_, _ = fmt.Fprintf(ios.Out, "\nCached entities:\n")
		for rt, count := range counts {
			_, _ = fmt.Fprintf(ios.Out, "  %-20s  %d\n", rt, count)
		}
	}

	return nil
}
