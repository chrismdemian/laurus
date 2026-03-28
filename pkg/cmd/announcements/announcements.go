// Package announcements implements the announcements command group.
package announcements

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/internal/iostreams"
	"github.com/chrismdemian/laurus/internal/render"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// NewCmdAnnouncements returns the announcements list command.
func NewCmdAnnouncements(f *cmdutil.Factory) *cobra.Command {
	var opts listOpts

	cmd := &cobra.Command{
		Use:   "announcements [course]",
		Short: "List announcements across your courses",
		Long:  "Display announcements from all active courses or a specific course.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				opts.Course = args[0]
			}
			return listRun(f, opts)
		},
	}

	cmd.Flags().BoolVar(&opts.Unread, "unread", false, "Show only unread announcements")
	cmd.Flags().StringVar(&opts.Since, "since", "", "Show announcements from the last duration (e.g., 3d, 1w, 24h)")

	return cmd
}

type listOpts struct {
	Course string
	Unread bool
	Since  string
}

func listRun(f *cmdutil.Factory, opts listOpts) error {
	client, err := f.Client()
	if err != nil {
		return err
	}
	ios := f.IOStreams()
	ctx := context.Background()

	// Build context codes and course name map
	var contextCodes []string
	courseNames := make(map[string]string) // "course_123" -> "CSC108"

	if opts.Course != "" {
		course, err := canvas.FindCourse(ctx, client, opts.Course)
		if err != nil {
			return fmt.Errorf("finding course %q: %w", opts.Course, err)
		}
		code := fmt.Sprintf("course_%d", course.ID)
		contextCodes = []string{code}
		courseNames[code] = course.CourseCode
	} else {
		for c, err := range canvas.ListCourses(ctx, client, canvas.CourseListOptions{
			EnrollmentState: "active",
		}) {
			if err != nil {
				return fmt.Errorf("listing courses: %w", err)
			}
			code := fmt.Sprintf("course_%d", c.ID)
			contextCodes = append(contextCodes, code)
			courseNames[code] = c.CourseCode
		}
	}

	if len(contextCodes) == 0 {
		_, _ = fmt.Fprintln(ios.Out, "No active courses found.")
		return nil
	}

	// Compute start_date: --since flag or default to all time
	startDate := "2000-01-01"
	if opts.Since != "" {
		dur, err := parseSinceDuration(opts.Since)
		if err != nil {
			return fmt.Errorf("invalid --since value %q: %w", opts.Since, err)
		}
		startDate = time.Now().Add(-dur).Format("2006-01-02")
	}

	// Fetch announcements
	var items []canvas.Announcement
	for a, err := range canvas.ListAnnouncements(ctx, client, canvas.ListAnnouncementsOptions{
		ContextCodes: contextCodes,
		StartDate:    startDate,
	}) {
		if err != nil {
			return fmt.Errorf("listing announcements: %w", err)
		}
		if opts.Unread && a.ReadState != "unread" {
			continue
		}
		items = append(items, a)
	}

	// Sort newest first (nil PostedAt sorts to end)
	sort.Slice(items, func(i, j int) bool {
		pi, pj := items[i].PostedAt, items[j].PostedAt
		if pi == nil && pj == nil {
			return items[i].Title < items[j].Title
		}
		if pi == nil {
			return false
		}
		if pj == nil {
			return true
		}
		return pi.After(*pj)
	})

	if ios.IsJSON {
		return cmdutil.RenderJSON(ios, items)
	}

	if len(items) == 0 {
		_, _ = fmt.Fprintln(ios.Out, "No announcements found.")
		return nil
	}

	palette := cmdutil.NewPalette(ios)
	tbl := cmdutil.NewTable(ios)
	tbl.AddHeader("COURSE", "TITLE", "DATE", "STATUS")

	for _, a := range items {
		courseName := courseNames[a.ContextCode]
		if courseName == "" {
			courseName = a.ContextCode
		}

		style := palette.Neutral
		status := ""
		if a.ReadState == "unread" {
			style = palette.DueToday
			status = "unread"
		}

		tbl.AddStyledRow(
			cmdutil.StyledCell{Value: courseName, Style: style},
			cmdutil.StyledCell{Value: a.Title, Style: style},
			cmdutil.StyledCell{Value: formatPostedAt(a.PostedAt), Style: style},
			cmdutil.StyledCell{Value: status, Style: style},
		)
	}

	_ = ios.StartPager()
	defer ios.StopPager()
	return tbl.Render()
}

// NewCmdAnnouncement returns the singular announcement detail command.
func NewCmdAnnouncement(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "announcement <course> <title>",
		Short: "View a specific announcement",
		Long:  "Show the full content of an announcement.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return viewRun(f, args[0], args[1])
		},
	}
	return cmd
}

func viewRun(f *cmdutil.Factory, courseQuery, titleQuery string) error {
	client, err := f.Client()
	if err != nil {
		return err
	}
	ios := f.IOStreams()
	ctx := context.Background()

	// Resolve course
	course, err := canvas.FindCourse(ctx, client, courseQuery)
	if err != nil {
		return fmt.Errorf("finding course %q: %w", courseQuery, err)
	}

	// Fetch all announcements for this course
	contextCode := fmt.Sprintf("course_%d", course.ID)
	var items []canvas.Announcement
	for a, err := range canvas.ListAnnouncements(ctx, client, canvas.ListAnnouncementsOptions{
		ContextCodes: []string{contextCode},
		StartDate:    "2000-01-01",
	}) {
		if err != nil {
			return fmt.Errorf("listing announcements: %w", err)
		}
		items = append(items, a)
	}

	// Fuzzy match title
	ann, err := findAnnouncement(items, titleQuery)
	if err != nil {
		return err
	}

	if ios.IsJSON {
		return cmdutil.RenderJSON(ios, ann)
	}

	return renderAnnouncementDetail(ios, course, ann)
}

func findAnnouncement(items []canvas.Announcement, query string) (canvas.Announcement, error) {
	// Try numeric ID
	if id, err := strconv.ParseInt(query, 10, 64); err == nil {
		for _, a := range items {
			if a.ID == id {
				return a, nil
			}
		}
	}

	q := strings.ToLower(query)

	// Exact title match
	for _, a := range items {
		if strings.EqualFold(a.Title, query) {
			return a, nil
		}
	}

	// Substring match
	for _, a := range items {
		if strings.Contains(strings.ToLower(a.Title), q) {
			return a, nil
		}
	}

	return canvas.Announcement{}, fmt.Errorf("no announcement matching %q: %w", query, canvas.ErrNotFound)
}

func renderAnnouncementDetail(ios *iostreams.IOStreams, course canvas.Course, a canvas.Announcement) error {
	palette := cmdutil.NewPalette(ios)

	_ = ios.StartPager()
	defer ios.StopPager()

	_, _ = fmt.Fprintln(ios.Out, palette.Header.Render(a.Title))

	printField(ios, palette, "Course", course.CourseCode)
	printField(ios, palette, "Author", a.Author.Name)
	printField(ios, palette, "Date", formatPostedAt(a.PostedAt))
	if a.HTMLURL != "" {
		printField(ios, palette, "URL", a.HTMLURL)
	}

	if strings.TrimSpace(a.Message) != "" {
		_, _ = fmt.Fprintln(ios.Out)
		rendered, err := render.CanvasHTML(a.Message, ios.TerminalWidth()-4)
		if err != nil {
			_, _ = fmt.Fprintln(ios.Out, a.Message)
		} else {
			_, _ = fmt.Fprint(ios.Out, rendered)
		}
	}

	return nil
}

func printField(ios *iostreams.IOStreams, palette *cmdutil.Palette, label, value string) {
	_, _ = fmt.Fprintf(ios.Out, "  %s  %s\n",
		palette.Muted.Render(fmt.Sprintf("%-14s", label)),
		value,
	)
}

func formatPostedAt(t *time.Time) string {
	if t == nil {
		return ""
	}
	return cmdutil.RelativeTime(*t)
}

// parseSinceDuration parses a human-friendly duration: 3d, 1w, 24h.
func parseSinceDuration(s string) (time.Duration, error) {
	if len(s) < 2 {
		return 0, fmt.Errorf("duration too short")
	}

	suffix := s[len(s)-1]
	numStr := s[:len(s)-1]
	n, err := strconv.Atoi(numStr)
	if err != nil {
		return 0, fmt.Errorf("invalid number: %s", numStr)
	}
	if n <= 0 {
		return 0, fmt.Errorf("duration must be positive")
	}

	switch suffix {
	case 'h':
		return time.Duration(n) * time.Hour, nil
	case 'd':
		return time.Duration(n) * 24 * time.Hour, nil
	case 'w':
		return time.Duration(n) * 7 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unknown suffix %q (use h, d, or w)", string(suffix))
	}
}
