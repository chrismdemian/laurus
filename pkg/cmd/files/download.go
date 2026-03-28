package files

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

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
		Long:  "Download a file from a course to the current directory or a specified path.",
		Args:  cobra.ExactArgs(2),
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

	course, err := canvas.FindCourse(ctx, client, courseQuery)
	if err != nil {
		return fmt.Errorf("finding course %q: %w", courseQuery, err)
	}

	file, err := canvas.FindFile(ctx, client, course.ID, fileQuery)
	if err != nil {
		return fmt.Errorf("finding file %q: %w", fileQuery, err)
	}

	// Determine output path — sanitize filename to prevent path traversal
	safeName := filepath.Base(file.DisplayName)
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

	// Download via pre-signed public URL (no auth headers sent to CDN)
	n, err := canvas.DownloadFile(ctx, client, file.ID, out)
	if err != nil {
		// Clean up partial file on error
		_ = out.Close()
		_ = os.Remove(outputPath)
		return fmt.Errorf("downloading %q: %w", file.DisplayName, err)
	}

	_, _ = fmt.Fprintf(ios.Out, "Downloaded %s (%s) to %s\n",
		file.DisplayName,
		cmdutil.FormatFileSize(n),
		outputPath,
	)
	return nil
}
