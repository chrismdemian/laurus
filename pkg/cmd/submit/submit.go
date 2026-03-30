package submit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

type submitOpts struct {
	text string
	url  string
}

// NewCmdSubmit returns the submit command.
func NewCmdSubmit(f *cmdutil.Factory) *cobra.Command {
	var opts submitOpts

	cmd := &cobra.Command{
		Use:   "submit <course> <assignment> [files...]",
		Short: "Submit an assignment",
		Long: `Submit an assignment via file upload, text entry, or URL.

Files are uploaded using Canvas's 3-step upload flow and attached to the submission.
Use --text for inline text submissions or --url for URL submissions.`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return submitRun(f, args[0], args[1], args[2:], opts)
		},
	}

	cmd.Flags().StringVar(&opts.text, "text", "", "Submit inline text instead of files")
	cmd.Flags().StringVar(&opts.url, "url", "", "Submit a URL instead of files")
	cmd.MarkFlagsMutuallyExclusive("text", "url")

	return cmd
}

func submitRun(f *cmdutil.Factory, courseQuery, assignmentQuery string, files []string, opts submitOpts) error {
	client, err := f.Client()
	if err != nil {
		return err
	}
	ios := f.IOStreams()
	ctx := context.Background()

	// Determine submission type
	submissionType, err := resolveSubmissionType(files, opts)
	if err != nil {
		return err
	}

	// Resolve course and assignment
	course, err := canvas.FindCourse(ctx, client, courseQuery)
	if err != nil {
		return fmt.Errorf("finding course %q: %w", courseQuery, err)
	}

	assignment, err := canvas.FindAssignment(ctx, client, course.ID, assignmentQuery)
	if err != nil {
		return fmt.Errorf("finding assignment %q: %w", assignmentQuery, err)
	}

	req := canvas.CreateSubmissionRequest{
		SubmissionType: submissionType,
	}

	switch submissionType {
	case "online_text_entry":
		req.Body = opts.text

	case "online_url":
		req.URL = opts.url

	case "online_upload":
		// Upload each file sequentially
		var fileIDs []int64
		for _, filePath := range files {
			absPath, err := filepath.Abs(filePath)
			if err != nil {
				return fmt.Errorf("resolving path %q: %w", filePath, err)
			}

			_, _ = fmt.Fprintf(ios.Out, "Uploading %s...\n", filepath.Base(absPath))

			preflightPath := fmt.Sprintf("/api/v1/courses/%d/assignments/%d/submissions/self/files",
				course.ID, assignment.ID)

			file, err := canvas.UploadFile(ctx, client, preflightPath, absPath)
			if err != nil {
				return fmt.Errorf("uploading %q: %w", filepath.Base(absPath), err)
			}

			fileIDs = append(fileIDs, file.ID)
			_, _ = fmt.Fprintf(ios.Out, "  Uploaded %s (ID: %d)\n", file.DisplayName, file.ID)
		}
		req.FileIDs = fileIDs
	}

	sub, err := canvas.CreateSubmission(ctx, client, course.ID, assignment.ID, req)
	if err != nil {
		return fmt.Errorf("submitting assignment: %w", err)
	}

	_, _ = fmt.Fprintf(ios.Out, "Submitted %q in %s", assignment.Name, course.CourseCode)
	if sub.SubmittedAt != nil {
		_, _ = fmt.Fprintf(ios.Out, " at %s", sub.SubmittedAt.Local().Format("Jan 2 3:04 PM"))
	}
	_, _ = fmt.Fprintln(ios.Out)

	return nil
}

func resolveSubmissionType(files []string, opts submitOpts) (string, error) {
	hasFiles := len(files) > 0
	hasText := opts.text != ""
	hasURL := opts.url != ""

	switch {
	case hasText:
		return "online_text_entry", nil
	case hasURL:
		return "online_url", nil
	case hasFiles:
		// Validate all files exist
		for _, f := range files {
			info, err := os.Stat(f)
			if err != nil {
				return "", fmt.Errorf("file %q: %w", f, err)
			}
			if info.IsDir() {
				return "", fmt.Errorf("%q is a directory, not a file", f)
			}
		}
		return "online_upload", nil
	default:
		return "", fmt.Errorf("provide files to upload, --text for text entry, or --url for URL submission")
	}
}
