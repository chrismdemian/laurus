// Package sync implements the sync command for populating the local cache.
package sync

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/internal/cache"
	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/internal/syncer"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

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

	courses, res := syncer.SyncCourses(ctx, client, db)
	if res.Err != nil {
		return res.Err
	}
	_, _ = fmt.Fprintf(ios.ErrOut, "  %-12s  %-20s  %d items\n", "all", "courses", res.Count)

	if courseQuery != "" {
		course, err := canvas.FindCourse(ctx, client, courseQuery)
		if err != nil {
			return fmt.Errorf("finding course %q: %w", courseQuery, err)
		}
		courses = []canvas.Course{course}
	}

	sum := syncer.SyncAll(ctx, client, db, courses, syncer.Options{
		OnResult: func(r syncer.Result) {
			if r.Err != nil {
				return
			}
			label := fmt.Sprintf("%d items", r.Count)
			if r.Status == cache.StatusSkipped {
				label = "skipped (not available)"
			}
			_, _ = fmt.Fprintf(ios.ErrOut, "  %-12s  %-20s  %s\n", r.CourseCode, r.Job, label)
		},
	})

	elapsed := time.Since(start).Round(100 * time.Millisecond)
	_, _ = fmt.Fprintf(ios.ErrOut, "\nSynced %d courses in %s (%d items total)\n", len(courses), elapsed, sum.Items()+res.Count)

	if errs := sum.Errors(); len(errs) > 0 {
		_, _ = fmt.Fprintf(ios.ErrOut, "\nWarnings (%d):\n", len(errs))
		for _, e := range errs {
			_, _ = fmt.Fprintf(ios.ErrOut, "  %s\n", e)
		}
	}
	return nil
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
