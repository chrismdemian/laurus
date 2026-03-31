// Package calendar implements the calendar command.
package calendar

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	ics "github.com/arran4/golang-ical"
	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/internal/cache"
	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/internal/iostreams"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// NewCmdCalendar returns the calendar command.
func NewCmdCalendar(f *cmdutil.Factory) *cobra.Command {
	var opts calendarOpts

	cmd := &cobra.Command{
		Use:   "calendar [course]",
		Short: "Show upcoming events and assignment deadlines",
		Long:  "Display calendar events and assignment due dates from Canvas.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				opts.Course = args[0]
			}
			return calendarRun(f, opts)
		},
	}

	cmd.Flags().BoolVar(&opts.Month, "month", false, "Show the full current month")
	cmd.Flags().BoolVar(&opts.Export, "export", false, "Output iCal (.ics) format to stdout")
	cmd.Flags().StringVar(&opts.From, "from", "", "Start date (YYYY-MM-DD)")
	cmd.Flags().StringVar(&opts.To, "to", "", "End date (YYYY-MM-DD)")

	return cmd
}

type calendarOpts struct {
	Course string
	Month  bool
	Export bool
	From   string
	To     string
}

func calendarRun(f *cmdutil.Factory, opts calendarOpts) error {
	client, err := f.Client()
	if err != nil {
		return err
	}
	ios := f.IOStreams()
	ctx := context.Background()

	// Determine date range.
	now := time.Now()
	startDate, endDate := weekRange(now)

	if opts.Month {
		startDate = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		endDate = startDate.AddDate(0, 1, -1)
	}
	if opts.From != "" {
		if t, err := time.Parse("2006-01-02", opts.From); err == nil {
			startDate = t
		} else {
			return fmt.Errorf("invalid --from date %q (use YYYY-MM-DD): %w", opts.From, err)
		}
	}
	if opts.To != "" {
		if t, err := time.Parse("2006-01-02", opts.To); err == nil {
			endDate = t
		} else {
			return fmt.Errorf("invalid --to date %q (use YYYY-MM-DD): %w", opts.To, err)
		}
	}

	// Build context codes.
	var contextCodes []string
	courseNames := make(map[string]string)

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

	// Fetch both event types and merge.
	var events []canvas.CalendarEvent
	sd := startDate.Format("2006-01-02")
	ed := endDate.Format("2006-01-02")

	for _, eventType := range []string{"event", "assignment"} {
		for ev, err := range canvas.ListCalendarEvents(ctx, client, canvas.ListCalendarEventsOptions{
			Type:         eventType,
			StartDate:    sd,
			EndDate:      ed,
			ContextCodes: contextCodes,
		}) {
			if err != nil {
				return fmt.Errorf("listing %s events: %w", eventType, err)
			}
			events = append(events, ev)
		}
	}

	// Opportunistic cache write.
	if db, err := f.Cache(); err == nil {
		cacheItems := make([]cache.CacheItem, len(events))
		for i, ev := range events {
			cacheItems[i] = cache.CacheItem{ID: ev.ID, CourseID: parseCourseID(ev.ContextCode), Data: ev}
		}
		_ = db.UpsertMany(cache.ResourceCalendarEvents, cacheItems)
		_ = db.SetSyncMeta(cache.ResourceCalendarEvents, 0, len(cacheItems), "success")
	}

	// Sort by start time.
	sort.Slice(events, func(i, j int) bool {
		si, sj := events[i].StartAt, events[j].StartAt
		if si == nil && sj == nil {
			return events[i].Title < events[j].Title
		}
		if si == nil {
			return false
		}
		if sj == nil {
			return true
		}
		return si.Before(*sj)
	})

	if opts.Export {
		return exportICS(ios, events, courseNames)
	}

	if ios.IsJSON {
		return cmdutil.RenderJSON(ios, events)
	}

	if len(events) == 0 {
		_, _ = fmt.Fprintf(ios.Out, "No events from %s to %s.\n", sd, ed)
		return nil
	}

	return renderTable(ios, events, courseNames, now)
}

func renderTable(ios *iostreams.IOStreams, events []canvas.CalendarEvent, courseNames map[string]string, now time.Time) error {
	palette := cmdutil.NewPalette(ios)
	tbl := cmdutil.NewTable(ios)
	tbl.AddHeader("DATE", "TIME", "COURSE", "TITLE")

	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	lastDate := ""

	for _, ev := range events {
		courseName := courseNames[ev.ContextCode]
		if courseName == "" {
			courseName = ev.ContextCode
		}

		dateStr := ""
		timeStr := ""
		style := palette.Neutral

		if ev.StartAt != nil {
			evDate := time.Date(ev.StartAt.Year(), ev.StartAt.Month(), ev.StartAt.Day(), 0, 0, 0, 0, ev.StartAt.Location())

			// Group by date: only show date on first row of each day.
			ds := ev.StartAt.Format("Mon Jan 2")
			if ds != lastDate {
				dateStr = ds
				lastDate = ds
			}

			if !ev.AllDay {
				timeStr = ev.StartAt.Local().Format("3:04 PM")
			}

			if ev.StartAt.Before(now) {
				style = palette.Overdue
			} else if evDate.Equal(today) {
				style = palette.DueToday
			}
		}

		tbl.AddStyledRow(
			cmdutil.StyledCell{Value: dateStr, Style: style},
			cmdutil.StyledCell{Value: timeStr, Style: style},
			cmdutil.StyledCell{Value: courseName, Style: style},
			cmdutil.StyledCell{Value: ev.Title, Style: style},
		)
	}

	_ = ios.StartPager()
	defer ios.StopPager()
	return tbl.Render()
}

func exportICS(ios *iostreams.IOStreams, events []canvas.CalendarEvent, courseNames map[string]string) error {
	cal := ics.NewCalendar()
	cal.SetMethod(ics.MethodPublish)
	cal.SetProductId("-//Laurus//Canvas LMS CLI//EN")

	for _, ev := range events {
		uid := fmt.Sprintf("event-%d@laurus", ev.ID)
		event := cal.AddEvent(uid)
		event.SetDtStampTime(time.Now().UTC())

		courseName := courseNames[ev.ContextCode]
		if courseName != "" {
			event.SetSummary(fmt.Sprintf("%s: %s", courseName, ev.Title))
		} else {
			event.SetSummary(ev.Title)
		}

		if ev.StartAt != nil {
			event.SetStartAt(*ev.StartAt)
			if ev.EndAt != nil {
				event.SetEndAt(*ev.EndAt)
			} else {
				event.SetEndAt(ev.StartAt.Add(time.Hour))
			}
		}

		if ev.Description != nil && *ev.Description != "" {
			event.SetDescription(*ev.Description)
		}
		if ev.HTMLURL != "" {
			event.SetURL(ev.HTMLURL)
		}
	}

	_, err := fmt.Fprint(ios.Out, cal.Serialize())
	return err
}

// weekRange returns Monday through Sunday of the current week.
func weekRange(now time.Time) (time.Time, time.Time) {
	weekday := now.Weekday()
	if weekday == time.Sunday {
		weekday = 7
	}
	monday := now.AddDate(0, 0, -int(weekday-time.Monday))
	monday = time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, monday.Location())
	sunday := monday.AddDate(0, 0, 6)
	return monday, sunday
}

func parseCourseID(contextCode string) int64 {
	parts := strings.SplitN(contextCode, "_", 2)
	if len(parts) == 2 {
		id, _ := strconv.ParseInt(parts[1], 10, 64)
		return id
	}
	return 0
}
