// Package search implements the search command.
package search

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// NewCmdSearch returns the search command.
func NewCmdSearch(f *cmdutil.Factory) *cobra.Command {
	var opts searchOpts

	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search across your Canvas courses",
		Long:  "Search assignments, pages, and discussions using Canvas Smart Search (with REST fallback).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Query = args[0]
			return searchRun(f, opts)
		},
	}

	cmd.Flags().StringVarP(&opts.Course, "course", "c", "", "Search within a specific course")

	return cmd
}

type searchOpts struct {
	Query  string
	Course string
}

func searchRun(f *cmdutil.Factory, opts searchOpts) error {
	client, err := f.Client()
	if err != nil {
		return err
	}
	ios := f.IOStreams()
	ctx := context.Background()

	var allResults []courseSearchResult

	if opts.Course != "" {
		course, err := canvas.FindCourse(ctx, client, opts.Course)
		if err != nil {
			return fmt.Errorf("finding course %q: %w", opts.Course, err)
		}
		results, err := canvas.SearchCourseWithSmartFallback(ctx, client, course.ID, opts.Query)
		if err != nil {
			return fmt.Errorf("searching %s: %w", course.CourseCode, err)
		}
		if len(results) > 0 {
			allResults = append(allResults, courseSearchResult{CourseName: course.CourseCode, Results: results})
		}
	} else {
		// Search all active courses.
		var courses []canvas.Course
		for c, err := range canvas.ListCourses(ctx, client, canvas.CourseListOptions{
			EnrollmentState: "active",
		}) {
			if err != nil {
				return fmt.Errorf("listing courses: %w", err)
			}
			courses = append(courses, c)
		}

		for _, course := range courses {
			results, err := canvas.SearchCourseWithSmartFallback(ctx, client, course.ID, opts.Query)
			if err != nil {
				continue // Skip courses that error (permissions, etc.)
			}
			if len(results) > 0 {
				allResults = append(allResults, courseSearchResult{CourseName: course.CourseCode, Results: results})
			}
		}
	}

	if ios.IsJSON {
		flat := flattenResults(allResults)
		return cmdutil.RenderJSON(ios, flat)
	}

	if len(allResults) == 0 {
		_, _ = fmt.Fprintf(ios.Out, "No results for %q.\n", opts.Query)
		return nil
	}

	palette := cmdutil.NewPalette(ios)
	tbl := cmdutil.NewTable(ios)
	tbl.AddHeader("COURSE", "TYPE", "TITLE", "URL")

	for _, cr := range allResults {
		for _, r := range cr.Results {
			tbl.AddStyledRow(
				cmdutil.StyledCell{Value: cr.CourseName, Style: palette.Neutral},
				cmdutil.StyledCell{Value: r.Type, Style: palette.Muted},
				cmdutil.StyledCell{Value: r.Title, Style: palette.Neutral},
				cmdutil.StyledCell{Value: r.HTMLURL, Style: palette.Muted},
			)
		}
	}

	_ = ios.StartPager()
	defer ios.StopPager()
	return tbl.Render()
}

type courseSearchResult struct {
	CourseName string
	Results    []canvas.SearchResult
}

type jsonSearchResult struct {
	Course  string `json:"course"`
	Type    string `json:"type"`
	ID      int64  `json:"id,omitempty"`
	Title   string `json:"title"`
	HTMLURL string `json:"html_url,omitempty"`
}

func flattenResults(allResults []courseSearchResult) []jsonSearchResult {
	var flat []jsonSearchResult
	for _, cr := range allResults {
		for _, r := range cr.Results {
			flat = append(flat, jsonSearchResult{
				Course:  cr.CourseName,
				Type:    r.Type,
				ID:      r.ID,
				Title:   r.Title,
				HTMLURL: r.HTMLURL,
			})
		}
	}
	return flat
}
