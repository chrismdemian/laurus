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
			// A truncated course list is not a usable root set; stop rather
			// than sync (and prune) against half of it.
			return fmt.Errorf("listing courses: %w", err)
		}
		courses = append(courses, c)
	}

	// Cache courses (one transaction: upsert, prune, stamp).
	items := make([]cache.CacheItem, len(courses))
	for i, c := range courses {
		items[i] = cache.CacheItem{ID: c.ID, CourseID: 0, Data: c}
	}
	if _, err := db.ReplaceAll(cache.ResourceCourses, 0, items, cache.ReplaceOptions{}); err != nil {
		return fmt.Errorf("caching courses: %w", err)
	}
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

		var truncated bool
		for ag, err := range canvas.ListAssignmentGroups(ctx, client, course.ID, []string{"assignments", "submission"}) {
			if err != nil {
				if errors.Is(err, canvas.ErrPaginationTruncated) {
					truncated = true
					break
				}
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

		// One fetch fills three tables; they are replaced together with the
		// same truncation flag so a partial page can never prune any of them.
		opts := cache.ReplaceOptions{Truncated: truncated}
		for rt, items := range map[cache.ResourceType][]cache.CacheItem{
			cache.ResourceAssignmentGroups: groups,
			cache.ResourceAssignments:      assignments,
			cache.ResourceSubmissions:      submissions,
		} {
			if _, err := db.ReplaceAll(rt, course.ID, items, opts); err != nil {
				return 0, err
			}
		}
		if truncated {
			return len(groups) + len(assignments) + len(submissions), fmt.Errorf("%w; cached set kept, marked suspect", canvas.ErrPaginationTruncated)
		}
		return len(groups) + len(assignments) + len(submissions), nil
	})

	// Announcements.
	syncResource(ctx, results, code, "announcements", func() (int, error) {
		var items []cache.CacheItem
		var truncated bool
		contextCode := fmt.Sprintf("course_%d", course.ID)
		for a, err := range canvas.ListAnnouncements(ctx, client, canvas.ListAnnouncementsOptions{
			ContextCodes: []string{contextCode},
			StartDate:    "2000-01-01",
		}) {
			if err != nil {
				if errors.Is(err, canvas.ErrPaginationTruncated) {
					truncated = true
					break
				}
				return 0, err
			}
			items = append(items, cache.CacheItem{ID: a.ID, CourseID: course.ID, Data: a})
		}
		return replaceJob(db, cache.ResourceAnnouncements, course.ID, items, truncated)
	})

	// Discussion topics.
	syncResource(ctx, results, code, "discussions", func() (int, error) {
		var items []cache.CacheItem
		var truncated bool
		for d, err := range canvas.ListDiscussionTopics(ctx, client, course.ID, canvas.ListDiscussionTopicsOptions{}) {
			if err != nil {
				if errors.Is(err, canvas.ErrPaginationTruncated) {
					truncated = true
					break
				}
				return 0, err
			}
			items = append(items, cache.CacheItem{ID: d.ID, CourseID: course.ID, Data: d})
		}
		return replaceJob(db, cache.ResourceDiscussions, course.ID, items, truncated)
	})

	// Modules (with items + content details).
	syncResource(ctx, results, code, "modules", func() (int, error) {
		var mods []cache.CacheItem
		var modItems []cache.CacheItem
		var truncated bool

		for m, err := range canvas.ListModules(ctx, client, course.ID, canvas.ListModulesOptions{
			IncludeItems:          true,
			IncludeContentDetails: true,
		}) {
			if err != nil {
				if errors.Is(err, canvas.ErrPaginationTruncated) {
					truncated = true
					break
				}
				return 0, err
			}
			mods = append(mods, cache.CacheItem{ID: m.ID, CourseID: course.ID, Data: m})
			for _, item := range m.Items {
				modItems = append(modItems, cache.CacheItem{ID: item.ID, CourseID: course.ID, Data: item})
			}
		}

		opts := cache.ReplaceOptions{Truncated: truncated}
		if _, err := db.ReplaceAll(cache.ResourceModules, course.ID, mods, opts); err != nil {
			return 0, err
		}
		if _, err := db.ReplaceAll(cache.ResourceModuleItems, course.ID, modItems, opts); err != nil {
			return 0, err
		}
		if truncated {
			return len(mods) + len(modItems), fmt.Errorf("%w; cached set kept, marked suspect", canvas.ErrPaginationTruncated)
		}
		return len(mods) + len(modItems), nil
	})

	// Pages (handle 404/403 gracefully — Pages tab may be disabled).
	syncResource(ctx, results, code, "pages", func() (int, error) {
		var items []cache.CacheItem
		var truncated bool
		for p, err := range canvas.ListPages(ctx, client, course.ID, canvas.ListPagesOptions{}) {
			if err != nil {
				if errors.Is(err, canvas.ErrNotFound) || errors.Is(err, canvas.ErrForbidden) {
					return 0, db.SetSyncMeta(cache.ResourcePages, course.ID, 0, cache.StatusSkipped) // disabled, not an error
				}
				if errors.Is(err, canvas.ErrPaginationTruncated) {
					truncated = true
					break
				}
				return 0, err
			}
			items = append(items, cache.CacheItem{ID: p.PageID, CourseID: course.ID, Data: p})
		}
		return replaceJob(db, cache.ResourcePages, course.ID, items, truncated)
	})

	// Files metadata (handle 403 gracefully — Files tab may be restricted).
	syncResource(ctx, results, code, "files", func() (int, error) {
		var fileItems []cache.CacheItem
		var truncated bool
		for f, err := range canvas.ListFiles(ctx, client, course.ID, canvas.ListFilesOptions{}) {
			if err != nil {
				if errors.Is(err, canvas.ErrForbidden) {
					return 0, db.SetSyncMeta(cache.ResourceFiles, course.ID, 0, cache.StatusSkipped)
				}
				if errors.Is(err, canvas.ErrPaginationTruncated) {
					truncated = true
					break
				}
				return 0, err
			}
			fileItems = append(fileItems, cache.CacheItem{ID: f.ID, CourseID: course.ID, Data: f})
		}
		return replaceJob(db, cache.ResourceFiles, course.ID, fileItems, truncated)
	})

	// Folders (handle 403 gracefully).
	syncResource(ctx, results, code, "folders", func() (int, error) {
		var items []cache.CacheItem
		var truncated bool
		for f, err := range canvas.ListFolders(ctx, client, course.ID) {
			if err != nil {
				if errors.Is(err, canvas.ErrForbidden) {
					return 0, db.SetSyncMeta(cache.ResourceFolders, course.ID, 0, cache.StatusSkipped)
				}
				if errors.Is(err, canvas.ErrPaginationTruncated) {
					truncated = true
					break
				}
				return 0, err
			}
			items = append(items, cache.CacheItem{ID: f.ID, CourseID: course.ID, Data: f})
		}
		return replaceJob(db, cache.ResourceFolders, course.ID, items, truncated)
	})

	return nil
}

// replaceJob stores a single-table fetch through ReplaceAll and turns a
// truncated fetch into a warning the caller lists, while the cache keeps the
// previous complete set (status "suspect").
func replaceJob(db *cache.DB, rt cache.ResourceType, courseID int64, items []cache.CacheItem, truncated bool) (int, error) {
	if _, err := db.ReplaceAll(rt, courseID, items, cache.ReplaceOptions{Truncated: truncated}); err != nil {
		return 0, err
	}
	if truncated {
		return len(items), fmt.Errorf("%w; cached set kept, marked suspect", canvas.ErrPaginationTruncated)
	}
	return len(items), nil
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
