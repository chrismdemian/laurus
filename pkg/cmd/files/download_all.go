package files

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/internal/iostreams"
	"github.com/chrismdemian/laurus/internal/render"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// NewCmdDownloadAll returns the download-all command.
func NewCmdDownloadAll(f *cmdutil.Factory) *cobra.Command {
	var opts downloadAllOpts

	cmd := &cobra.Command{
		Use:   "download-all <course>",
		Short: "Download all course content from modules",
		Long: `Download all files, pages, and assignment descriptions from a course's modules.

Content is organized by module name into folders. Files are downloaded as-is,
pages and assignments are saved as markdown. This works even on Canvas instances
that restrict the Files tab, because content is accessed through module items.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Course = args[0]
			return downloadAllRun(f, opts)
		},
	}

	cmd.Flags().StringVarP(&opts.OutputDir, "output", "o", "", "Output directory (default: ./<course_code>)")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Show what would be downloaded without downloading")

	return cmd
}

type downloadAllOpts struct {
	Course    string
	OutputDir string
	DryRun    bool
}

func downloadAllRun(f *cmdutil.Factory, opts downloadAllOpts) error {
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

	// Determine output directory
	outDir := opts.OutputDir
	if outDir == "" {
		outDir = sanitizeDirName(course.CourseCode)
	}

	// Fetch all modules with items
	var modules []canvas.Module
	for m, err := range canvas.ListModules(ctx, client, course.ID, canvas.ListModulesOptions{
		IncludeItems:          true,
		IncludeContentDetails: true,
	}) {
		if err != nil {
			return fmt.Errorf("listing modules: %w", err)
		}
		modules = append(modules, m)
	}

	if len(modules) == 0 {
		_, _ = fmt.Fprintln(ios.Out, "No modules found in this course.")
		return nil
	}

	// Count items
	var totalFiles, totalPages, totalAssignments int
	for _, m := range modules {
		for _, item := range m.Items {
			switch item.Type {
			case "File":
				totalFiles++
			case "Page":
				totalPages++
			case "Assignment":
				totalAssignments++
			}
		}
	}

	_, _ = fmt.Fprintf(ios.Out, "Course: %s (%s)\n", course.Name, course.CourseCode)
	_, _ = fmt.Fprintf(ios.Out, "Modules: %d  Files: %d  Pages: %d  Assignments: %d\n",
		len(modules), totalFiles, totalPages, totalAssignments)
	_, _ = fmt.Fprintf(ios.Out, "Output: %s/\n\n", outDir)

	if opts.DryRun {
		for _, m := range modules {
			_, _ = fmt.Fprintf(ios.Out, "%s/\n", sanitizeDirName(m.Name))
			for _, item := range m.Items {
				switch item.Type {
				case "File":
					_, _ = fmt.Fprintf(ios.Out, "  [File] %s\n", item.Title)
				case "Page":
					_, _ = fmt.Fprintf(ios.Out, "  [Page] %s.md\n", item.Title)
				case "Assignment":
					_, _ = fmt.Fprintf(ios.Out, "  [Assignment] %s.md\n", item.Title)
				}
			}
		}
		return nil
	}

	var downloaded, skipped, errored int

	for _, m := range modules {
		moduleDir := filepath.Join(outDir, sanitizeDirName(m.Name))

		for _, item := range m.Items {
			switch item.Type {
			case "File":
				ok, err := downloadFileItem(ctx, client, ios, moduleDir, item)
				if err != nil {
					_, _ = fmt.Fprintf(ios.Out, "  [FAIL] %s: %v\n", item.Title, err)
					errored++
				} else if ok {
					downloaded++
				} else {
					skipped++
				}

			case "Page":
				ok, err := downloadPageItem(ctx, client, ios, moduleDir, course.ID, item)
				if err != nil {
					_, _ = fmt.Fprintf(ios.Out, "  [FAIL] %s: %v\n", item.Title, err)
					errored++
				} else if ok {
					downloaded++
				} else {
					skipped++
				}

			case "Assignment":
				ok, err := downloadAssignmentItem(ctx, client, ios, moduleDir, course.ID, item)
				if err != nil {
					_, _ = fmt.Fprintf(ios.Out, "  [FAIL] %s: %v\n", item.Title, err)
					errored++
				} else if ok {
					downloaded++
				} else {
					skipped++
				}
			}
		}
	}

	_, _ = fmt.Fprintf(ios.Out, "\nDone: %d downloaded, %d skipped, %d errors\n",
		downloaded, skipped, errored)
	return nil
}

// downloadFileItem downloads a File-type module item.
// Returns (true, nil) on success, (false, nil) if skipped (already exists), or (false, err).
func downloadFileItem(ctx context.Context, c *canvas.Client, ios *iostreams.IOStreams, dir string, item canvas.ModuleItem) (bool, error) {
	if item.ContentID == nil {
		return false, nil
	}

	// Get file metadata
	file, err := canvas.GetFile(ctx, c, *item.ContentID)
	if err != nil {
		return false, fmt.Errorf("getting file info: %w", err)
	}

	safeName := filepath.Base(file.DisplayName)
	destPath := filepath.Join(dir, safeName)

	// Skip if already exists and same size
	if info, err := os.Stat(destPath); err == nil && info.Size() == file.Size {
		return false, nil // already downloaded
	}

	// Create directory
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, fmt.Errorf("creating directory: %w", err)
	}

	// Download
	out, err := os.Create(destPath)
	if err != nil {
		return false, fmt.Errorf("creating file: %w", err)
	}
	defer func() { _ = out.Close() }()

	n, err := canvas.DownloadFile(ctx, c, file.ID, out)
	if err != nil {
		_ = out.Close()
		_ = os.Remove(destPath)
		return false, err
	}

	_, _ = fmt.Fprintf(ios.Out, "  [OK] %s (%s)\n", safeName, cmdutil.FormatFileSize(n))
	return true, nil
}

// downloadPageItem fetches a Page-type module item and saves it as markdown.
func downloadPageItem(ctx context.Context, c *canvas.Client, ios *iostreams.IOStreams, dir string, courseID int64, item canvas.ModuleItem) (bool, error) {
	slug := ""
	if item.PageURL != nil {
		slug = *item.PageURL
	}
	if slug == "" {
		return false, nil // no page URL slug
	}

	destPath := filepath.Join(dir, sanitizeFileName(item.Title)+".md")

	// Skip if already exists
	if _, err := os.Stat(destPath); err == nil {
		return false, nil
	}

	page, err := canvas.GetPage(ctx, c, courseID, slug)
	if err != nil {
		if errors.Is(err, canvas.ErrNotFound) || errors.Is(err, canvas.ErrForbidden) {
			return false, nil // skip inaccessible pages
		}
		return false, err
	}

	if page.Body == nil || strings.TrimSpace(*page.Body) == "" {
		return false, nil // empty page
	}

	md, err := render.CanvasHTMLToMarkdown(*page.Body)
	if err != nil {
		md = *page.Body // fallback to raw HTML
	}

	// Add title header
	content := fmt.Sprintf("# %s\n\n%s", page.Title, md)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, fmt.Errorf("creating directory: %w", err)
	}

	if err := os.WriteFile(destPath, []byte(content), 0o644); err != nil {
		return false, fmt.Errorf("writing file: %w", err)
	}

	_, _ = fmt.Fprintf(ios.Out, "  [OK] %s.md\n", sanitizeFileName(item.Title))
	return true, nil
}

// downloadAssignmentItem fetches an Assignment-type module item and saves details as markdown.
func downloadAssignmentItem(ctx context.Context, c *canvas.Client, ios *iostreams.IOStreams, dir string, courseID int64, item canvas.ModuleItem) (bool, error) {
	if item.ContentID == nil {
		return false, nil
	}

	destPath := filepath.Join(dir, sanitizeFileName(item.Title)+".md")

	// Skip if already exists
	if _, err := os.Stat(destPath); err == nil {
		return false, nil
	}

	assignment, err := canvas.GetAssignment(ctx, c, courseID, *item.ContentID, []string{"submission"})
	if err != nil {
		if errors.Is(err, canvas.ErrNotFound) || errors.Is(err, canvas.ErrForbidden) {
			return false, nil
		}
		return false, err
	}

	// Build markdown content
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s\n\n", assignment.Name)

	if assignment.DueAt != nil {
		fmt.Fprintf(&sb, "**Due:** %s\n\n", assignment.DueAt.Format("January 2, 2006 at 3:04 PM"))
	}
	if assignment.PointsPossible != nil {
		fmt.Fprintf(&sb, "**Points:** %.0f\n\n", *assignment.PointsPossible)
	}
	if len(assignment.SubmissionTypes) > 0 {
		fmt.Fprintf(&sb, "**Submission Types:** %s\n\n", strings.Join(assignment.SubmissionTypes, ", "))
	}
	if assignment.HTMLURL != "" {
		fmt.Fprintf(&sb, "**URL:** %s\n\n", assignment.HTMLURL)
	}

	if assignment.Description != nil && strings.TrimSpace(*assignment.Description) != "" {
		fmt.Fprintf(&sb, "---\n\n")
		md, err := render.CanvasHTMLToMarkdown(*assignment.Description)
		if err != nil {
			sb.WriteString(*assignment.Description)
		} else {
			sb.WriteString(md)
		}
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, fmt.Errorf("creating directory: %w", err)
	}

	if err := os.WriteFile(destPath, []byte(sb.String()), 0o644); err != nil {
		return false, fmt.Errorf("writing file: %w", err)
	}

	_, _ = fmt.Fprintf(ios.Out, "  [OK] %s.md\n", sanitizeFileName(item.Title))
	return true, nil
}

// sanitizeDirName creates a safe directory name from a string.
func sanitizeDirName(name string) string {
	// Replace characters that are invalid in Windows/macOS/Linux paths
	replacer := strings.NewReplacer(
		"/", "-",
		"\\", "-",
		":", " -",
		"*", "",
		"?", "",
		"\"", "",
		"<", "",
		">", "",
		"|", "",
	)
	result := replacer.Replace(strings.TrimSpace(name))
	// Collapse multiple spaces
	for strings.Contains(result, "  ") {
		result = strings.ReplaceAll(result, "  ", " ")
	}
	return result
}

// sanitizeFileName creates a safe file name from a string (without extension).
func sanitizeFileName(name string) string {
	return sanitizeDirName(name)
}
