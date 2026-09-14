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
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// NewCmdDownload returns the download command.
func NewCmdDownload(f *cmdutil.Factory) *cobra.Command {
	var outputPath string

	cmd := &cobra.Command{
		Use:   "download <course> <file>",
		Short: "Download a course file",
		Long: "Download a file from a course to the current directory or a specified path.\n\n" +
			"<file> may be a file name, a numeric Canvas file ID, or a Canvas file link\n" +
			"copied from a page. An ID or link is fetched directly, which works in courses\n" +
			"whose Files tab is hidden; <course> is used only for name lookups.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return downloadRun(f, args[0], args[1], outputPath)
		},
	}

	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "Output file path (default: ./<filename>)")

	return cmd
}

func downloadRun(f *cmdutil.Factory, courseQuery, fileQuery, outputPath string) error {
	client, err := f.Client()
	if err != nil {
		return err
	}
	ios := f.IOStreams()
	ctx := context.Background()

	// A numeric ID or a Canvas file link resolves through the user-scoped
	// files endpoint, which answers even when the course Files tab is hidden.
	var file canvas.File
	byID := false
	if fileID, ok := canvas.ParseFileRef(fileQuery); ok {
		byID = true
		file, err = canvas.GetFile(ctx, client, fileID)
		if err != nil {
			if errors.Is(err, canvas.ErrForbidden) || errors.Is(err, canvas.ErrPermissionDenied) {
				return fmt.Errorf("file %d: Canvas will not let this account read it", fileID)
			}
			return fmt.Errorf("fetching file %d: %w", fileID, err)
		}
	} else {
		course, cErr := canvas.FindCourse(ctx, client, courseQuery)
		if cErr != nil {
			return fmt.Errorf("finding course %q: %w", courseQuery, cErr)
		}

		file, err = canvas.FindFile(ctx, client, course.ID, fileQuery)
		if err != nil {
			if errors.Is(err, canvas.ErrForbidden) || errors.Is(err, canvas.ErrPermissionDenied) {
				return fmt.Errorf("this course hides its file list, so names cannot be searched; "+
					"pass the file ID or the Canvas file link from the page instead (looking up %q)", fileQuery)
			}
			return fmt.Errorf("finding file %q: %w", fileQuery, err)
		}
	}

	// Determine output path — sanitize filename to prevent path traversal
	name := file.DisplayName
	if strings.TrimSpace(name) == "" {
		name = file.Filename
	}
	if strings.TrimSpace(name) == "" {
		name = fmt.Sprintf("file-%d", file.ID)
	}
	safeName := filepath.Base(name)
	if outputPath == "" {
		outputPath = safeName
	}

	// If outputPath is a directory, append the sanitized filename
	if info, err := os.Stat(outputPath); err == nil && info.IsDir() {
		outputPath = filepath.Join(outputPath, safeName)
	}

	// Create parent directories if needed
	dir := filepath.Dir(outputPath)
	if dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating directory %q: %w", dir, err)
		}
	}

	// Create the output file
	out, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("creating file %q: %w", outputPath, err)
	}
	defer func() { _ = out.Close() }()

	// A file reached by ID carries a verifier-signed URL that works without the
	// course files scope, which is the whole point of that path; a file found
	// by name keeps the pre-signed public_url route that has always worked.
	// Neither request sends auth headers to the CDN.
	var n int64
	if byID && file.URL != "" {
		n, err = canvas.DownloadFromURL(ctx, file.URL, file.Size, out)
	} else {
		n, err = canvas.DownloadFile(ctx, client, file.ID, out)
	}
	if err != nil {
		// Clean up partial file on error
		_ = out.Close()
		_ = os.Remove(outputPath)
		return fmt.Errorf("downloading %q: %w", name, err)
	}

	_, _ = fmt.Fprintf(ios.Out, "Downloaded %s (%s) to %s\n",
		name,
		cmdutil.FormatFileSize(n),
		outputPath,
	)
	return nil
}
