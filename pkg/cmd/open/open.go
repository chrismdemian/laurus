// Package open implements the open command for opening Canvas resources in a browser.
package open

import (
	"context"
	"fmt"
	"strings"

	"github.com/pkg/browser"
	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// NewCmdOpen returns the open command.
func NewCmdOpen(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "open <course|url> [assignment]",
		Short: "Open a course or assignment in your browser",
		Long: `Open a Canvas course page or specific assignment in your default browser.

If the first argument is a URL (http:// or https://), it is opened directly.
This is useful as a fallback for complex Canvas content.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			assignment := ""
			if len(args) > 1 {
				assignment = args[1]
			}
			return openRun(f, args[0], assignment)
		},
	}
	return cmd
}

func openRun(f *cmdutil.Factory, courseQuery, assignmentQuery string) error {
	// Direct URL fallback: if the argument is a URL, open it directly.
	if strings.HasPrefix(courseQuery, "http://") || strings.HasPrefix(courseQuery, "https://") {
		ios := f.IOStreams()
		_, _ = fmt.Fprintf(ios.Out, "Opening %s in browser...\n", courseQuery)
		return browser.OpenURL(courseQuery)
	}

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

	if assignmentQuery == "" {
		_, _ = fmt.Fprintf(ios.Out, "Opening %s in browser...\n", course.CourseCode)
		return browser.OpenURL(course.HTMLURL)
	}

	a, err := canvas.FindAssignment(ctx, client, course.ID, assignmentQuery)
	if err != nil {
		return fmt.Errorf("finding assignment %q: %w", assignmentQuery, err)
	}

	_, _ = fmt.Fprintf(ios.Out, "Opening %s — %s in browser...\n", course.CourseCode, a.Name)
	return browser.OpenURL(a.HTMLURL)
}
