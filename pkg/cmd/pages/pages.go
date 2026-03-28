// Package pages implements the pages command group.
package pages

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/internal/iostreams"
	"github.com/chrismdemian/laurus/internal/render"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// NewCmdPages returns the pages list command.
func NewCmdPages(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pages <course>",
		Short: "List wiki pages for a course",
		Long:  "Display wiki pages with their titles, update dates, and status.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return listRun(f, args[0])
		},
	}
	return cmd
}

func listRun(f *cmdutil.Factory, courseQuery string) error {
	client, err := f.Client()
	if err != nil {
		return err
	}
	ios := f.IOStreams()
	ctx := context.Background()

	course, err := canvas.FindCourse(ctx, client, courseQuery)
	if err != nil {
		return fmt.Errorf("finding course %q: %w", courseQuery, err)
	}

	var pages []canvas.Page
	for p, err := range canvas.ListPages(ctx, client, course.ID, canvas.ListPagesOptions{
		Sort:  "title",
		Order: "asc",
	}) {
		if err != nil {
			return fmt.Errorf("listing pages: %w", err)
		}
		pages = append(pages, p)
	}

	if ios.IsJSON {
		return cmdutil.RenderJSON(ios, pages)
	}

	if len(pages) == 0 {
		_, _ = fmt.Fprintln(ios.Out, "No pages found.")
		return nil
	}

	palette := cmdutil.NewPalette(ios)
	tbl := cmdutil.NewTable(ios)
	tbl.AddHeader("TITLE", "UPDATED", "STATUS")

	for _, p := range pages {
		style := palette.Neutral
		status := ""

		if p.FrontPage {
			status = "Front Page"
		}
		if p.LockedForUser {
			style = palette.Muted
			status = "Locked"
		}

		tbl.AddStyledRow(
			cmdutil.StyledCell{Value: p.Title, Style: style},
			cmdutil.StyledCell{Value: cmdutil.RelativeTime(p.UpdatedAt), Style: style},
			cmdutil.StyledCell{Value: status, Style: style},
		)
	}

	_ = ios.StartPager()
	defer ios.StopPager()
	return tbl.Render()
}

// NewCmdPage returns the singular page detail command.
func NewCmdPage(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "page <course> <title>",
		Short: "View a wiki page",
		Long:  "Show the full rendered content of a wiki page.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return viewRun(f, args[0], args[1])
		},
	}
	return cmd
}

func viewRun(f *cmdutil.Factory, courseQuery, pageQuery string) error {
	client, err := f.Client()
	if err != nil {
		return err
	}
	ios := f.IOStreams()
	ctx := context.Background()

	course, err := canvas.FindCourse(ctx, client, courseQuery)
	if err != nil {
		return fmt.Errorf("finding course %q: %w", courseQuery, err)
	}

	page, err := canvas.FindPage(ctx, client, course.ID, pageQuery)
	if err != nil {
		return fmt.Errorf("finding page %q: %w", pageQuery, err)
	}

	// Re-fetch with body if not present (FindPage may have returned a list result)
	if page.Body == nil {
		page, err = canvas.GetPage(ctx, client, course.ID, page.URL)
		if err != nil {
			return fmt.Errorf("fetching page content: %w", err)
		}
	}

	if ios.IsJSON {
		return cmdutil.RenderJSON(ios, page)
	}

	return renderPageDetail(ios, course, page)
}

func renderPageDetail(ios *iostreams.IOStreams, course canvas.Course, page canvas.Page) error {
	palette := cmdutil.NewPalette(ios)

	_ = ios.StartPager()
	defer ios.StopPager()

	_, _ = fmt.Fprintln(ios.Out, palette.Header.Render(page.Title))

	printField(ios, palette, "Course", course.CourseCode)
	printField(ios, palette, "Updated", cmdutil.RelativeTime(page.UpdatedAt))
	if page.FrontPage {
		printField(ios, palette, "Status", "Front Page")
	}

	if page.Body != nil && strings.TrimSpace(*page.Body) != "" {
		_, _ = fmt.Fprintln(ios.Out)
		rendered, err := render.CanvasHTML(*page.Body, ios.TerminalWidth()-4)
		if err != nil {
			_, _ = fmt.Fprintln(ios.Out, *page.Body)
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
