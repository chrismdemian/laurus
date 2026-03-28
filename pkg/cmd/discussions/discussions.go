// Package discussions implements the discussions command group.
package discussions

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/internal/iostreams"
	"github.com/chrismdemian/laurus/internal/render"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// NewCmdDiscussions returns the discussions list command.
func NewCmdDiscussions(f *cmdutil.Factory) *cobra.Command {
	var opts listOpts

	cmd := &cobra.Command{
		Use:   "discussions <course>",
		Short: "List discussion topics for a course",
		Long:  "Display discussion topics with reply counts, activity, and read status.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Course = args[0]
			return listRun(f, opts)
		},
	}

	cmd.Flags().BoolVar(&opts.Unread, "unread", false, "Show only unread topics")
	cmd.Flags().BoolVar(&opts.Pinned, "pinned", false, "Show only pinned topics")

	return cmd
}

type listOpts struct {
	Course string
	Unread bool
	Pinned bool
}

func listRun(f *cmdutil.Factory, opts listOpts) error {
	client, err := f.Client()
	if err != nil {
		return err
	}
	ios := f.IOStreams()
	ctx := context.Background()

	course, err := canvas.FindCourse(ctx, client, opts.Course)
	if err != nil {
		return fmt.Errorf("finding course %q: %w", opts.Course, err)
	}

	apiOpts := canvas.ListDiscussionTopicsOptions{
		OrderBy: "recent_activity",
	}
	if opts.Unread {
		apiOpts.FilterBy = "unread"
	}

	var items []canvas.DiscussionTopic
	for t, err := range canvas.ListDiscussionTopics(ctx, client, course.ID, apiOpts) {
		if err != nil {
			return fmt.Errorf("listing discussion topics: %w", err)
		}
		if opts.Pinned && !t.Pinned {
			continue
		}
		items = append(items, t)
	}

	// Sort: pinned first, then by most recent activity
	sort.Slice(items, func(i, j int) bool {
		if items[i].Pinned != items[j].Pinned {
			return items[i].Pinned
		}
		ti, tj := items[i].LastReplyAt, items[j].LastReplyAt
		if ti == nil && tj == nil {
			return items[i].Title < items[j].Title
		}
		if ti == nil {
			return false
		}
		if tj == nil {
			return true
		}
		return ti.After(*tj)
	})

	if ios.IsJSON {
		return cmdutil.RenderJSON(ios, items)
	}

	if len(items) == 0 {
		_, _ = fmt.Fprintln(ios.Out, "No discussion topics found.")
		return nil
	}

	palette := cmdutil.NewPalette(ios)
	tbl := cmdutil.NewTable(ios)
	tbl.AddHeader("TITLE", "REPLIES", "LAST ACTIVITY", "UNREAD", "STATUS")

	for _, t := range items {
		style := palette.Neutral
		if t.UnreadCount > 0 {
			style = palette.DueToday
		}
		if t.Locked {
			style = palette.Muted
		}
		if t.Pinned {
			style = palette.Header
		}

		lastActivity := "No replies"
		if t.LastReplyAt != nil {
			lastActivity = cmdutil.RelativeTime(*t.LastReplyAt)
		}

		unread := ""
		if t.UnreadCount > 0 {
			unread = fmt.Sprintf("%d", t.UnreadCount)
		}

		status := topicStatus(t)

		tbl.AddStyledRow(
			cmdutil.StyledCell{Value: t.Title, Style: style},
			cmdutil.StyledCell{Value: fmt.Sprintf("%d", t.DiscussionSubentryCount), Style: style},
			cmdutil.StyledCell{Value: lastActivity, Style: style},
			cmdutil.StyledCell{Value: unread, Style: style},
			cmdutil.StyledCell{Value: status, Style: style},
		)
	}

	_ = ios.StartPager()
	defer ios.StopPager()
	return tbl.Render()
}

func topicStatus(t canvas.DiscussionTopic) string {
	var parts []string
	if t.Pinned {
		parts = append(parts, "Pinned")
	}
	if t.Locked {
		parts = append(parts, "Locked")
	}
	if len(parts) == 0 {
		if t.Published {
			return ""
		}
		return "Unpublished"
	}
	return strings.Join(parts, ", ")
}

// NewCmdDiscussion returns the singular discussion detail command.
func NewCmdDiscussion(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "discussion <course> <topic>",
		Short: "View a discussion thread",
		Long:  "Show the full discussion topic with all replies.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return viewRun(f, args[0], args[1])
		},
	}
	return cmd
}

func viewRun(f *cmdutil.Factory, courseQuery, topicQuery string) error {
	client, err := f.Client()
	if err != nil {
		return err
	}
	ios := f.IOStreams()
	ctx := context.Background()

	// Resolve course and topic
	course, err := canvas.FindCourse(ctx, client, courseQuery)
	if err != nil {
		return fmt.Errorf("finding course %q: %w", courseQuery, err)
	}

	topic, err := canvas.FindDiscussionTopic(ctx, client, course.ID, topicQuery)
	if err != nil {
		return fmt.Errorf("finding topic %q: %w", topicQuery, err)
	}

	// Fetch full thread
	view, err := canvas.GetDiscussionTopicView(ctx, client, course.ID, topic.ID)
	if err != nil {
		return fmt.Errorf("fetching discussion thread: %w", err)
	}

	if ios.IsJSON {
		data := struct {
			Topic canvas.DiscussionTopic     `json:"topic"`
			View  canvas.DiscussionTopicView `json:"view"`
		}{
			Topic: topic,
			View:  view,
		}
		return cmdutil.RenderJSON(ios, data)
	}

	return renderDiscussionDetail(ios, course, topic, view)
}

func renderDiscussionDetail(ios *iostreams.IOStreams, course canvas.Course, topic canvas.DiscussionTopic, view canvas.DiscussionTopicView) error {
	palette := cmdutil.NewPalette(ios)

	_ = ios.StartPager()
	defer ios.StopPager()

	_, _ = fmt.Fprintln(ios.Out, palette.Header.Render(topic.Title))

	printField(ios, palette, "Course", course.CourseCode)
	printField(ios, palette, "Author", topic.Author.Name)
	if topic.PostedAt != nil {
		printField(ios, palette, "Date", cmdutil.RelativeTime(*topic.PostedAt))
	}
	printField(ios, palette, "Replies", fmt.Sprintf("%d", topic.DiscussionSubentryCount))
	if topic.HTMLURL != "" {
		printField(ios, palette, "URL", topic.HTMLURL)
	}

	// Topic body
	if topic.Message != nil && strings.TrimSpace(*topic.Message) != "" {
		_, _ = fmt.Fprintln(ios.Out)
		rendered, err := render.CanvasHTML(*topic.Message, ios.TerminalWidth()-4)
		if err != nil {
			_, _ = fmt.Fprintln(ios.Out, *topic.Message)
		} else {
			_, _ = fmt.Fprint(ios.Out, rendered)
		}
	}

	// Replies
	if len(view.View) > 0 {
		_, _ = fmt.Fprintln(ios.Out)
		_, _ = fmt.Fprintln(ios.Out, palette.Muted.Render("  --- Replies ---"))

		// Build lookup maps
		participants := make(map[int64]string)
		for _, p := range view.Participants {
			participants[p.ID] = p.DisplayName
		}
		unreadSet := make(map[int64]bool)
		for _, id := range view.UnreadEntries {
			unreadSet[id] = true
		}

		for _, entry := range view.View {
			renderEntry(ios, palette, participants, unreadSet, entry, 1)
		}
	}

	return nil
}

func renderEntry(ios *iostreams.IOStreams, palette *cmdutil.Palette, participants map[int64]string, unreadSet map[int64]bool, entry canvas.DiscussionEntry, indent int) {
	prefix := strings.Repeat("  ", indent)

	authorName := entry.UserName
	if authorName == "" {
		if name, ok := participants[entry.UserID]; ok {
			authorName = name
		} else {
			authorName = "Unknown"
		}
	}

	nameStyle := palette.Header
	dateStyle := palette.Muted
	if unreadSet[entry.ID] {
		nameStyle = palette.DueToday
	}

	_, _ = fmt.Fprintf(ios.Out, "%s%s  %s\n",
		prefix,
		nameStyle.Render(authorName),
		dateStyle.Render(entry.CreatedAt.Format("Jan 2, 3:04 PM")),
	)

	// Render message HTML (Canvas uses "<deleted>" for removed entries)
	msg := strings.TrimSpace(entry.Message)
	if msg == "" || msg == "<deleted>" {
		if msg == "<deleted>" {
			_, _ = fmt.Fprintf(ios.Out, "%s%s\n", prefix, palette.Muted.Render("[deleted]"))
		}
	} else {
		rendered, err := render.CanvasHTML(entry.Message, ios.TerminalWidth()-len(prefix)-4)
		if err != nil {
			_, _ = fmt.Fprintf(ios.Out, "%s%s\n", prefix, entry.Message)
		} else {
			for _, line := range strings.Split(rendered, "\n") {
				if line != "" {
					_, _ = fmt.Fprintf(ios.Out, "%s%s\n", prefix, line)
				}
			}
		}
	}

	// Recursive replies
	for _, reply := range entry.Replies {
		renderEntry(ios, palette, participants, unreadSet, reply, indent+1)
	}
}

func printField(ios *iostreams.IOStreams, palette *cmdutil.Palette, label, value string) {
	_, _ = fmt.Fprintf(ios.Out, "  %s  %s\n",
		palette.Muted.Render(fmt.Sprintf("%-14s", label)),
		value,
	)
}
