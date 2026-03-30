package sync

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/internal/cache"
	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// newCmdSyncFiles returns the "sync files" subcommand.
func newCmdSyncFiles(f *cmdutil.Factory) *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "files [course]",
		Short: "Sync course files to local disk",
		Long: `Download course files to the sync directory (default: ~/School).

Files are organized as: sync_dir/<course_code>/<folder_path>/<filename>.
Only new or changed files are downloaded (based on size and modification date).`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var courseQuery string
			if len(args) > 0 {
				courseQuery = args[0]
			}
			return syncFilesRun(f, courseQuery, dryRun)
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be downloaded without downloading")

	return cmd
}

func syncFilesRun(f *cmdutil.Factory, courseQuery string, dryRun bool) error {
	client, err := f.Client()
	if err != nil {
		return err
	}
	db, err := f.Cache()
	if err != nil {
		return err
	}
	cfg, err := f.Config()
	if err != nil {
		return err
	}
	ios := f.IOStreams()
	ctx := context.Background()
	start := time.Now()

	// Resolve sync directory.
	syncDir := cfg.SyncDir
	if syncDir == "" {
		syncDir = "~/School"
	}
	syncDir = expandHome(syncDir)

	// Fetch courses.
	var courses []canvas.Course
	if courseQuery != "" {
		course, err := canvas.FindCourse(ctx, client, courseQuery)
		if err != nil {
			return fmt.Errorf("finding course %q: %w", courseQuery, err)
		}
		courses = []canvas.Course{course}
	} else {
		for c, err := range canvas.ListCourses(ctx, client, canvas.CourseListOptions{
			EnrollmentState: "active",
		}) {
			if err != nil {
				return fmt.Errorf("listing courses: %w", err)
			}
			courses = append(courses, c)
		}
	}

	var totalDownloaded, totalSkipped int
	var totalBytes int64

	for _, course := range courses {
		code := course.CourseCode
		if code == "" {
			code = fmt.Sprintf("course_%d", course.ID)
		}

		// Fetch folders to build ID -> path map.
		folderPaths := make(map[int64]string)
		for folder, err := range canvas.ListFolders(ctx, client, course.ID) {
			if err != nil {
				if errors.Is(err, canvas.ErrForbidden) {
					_, _ = fmt.Fprintf(ios.ErrOut, "  %s  Files restricted, skipping\n", code)
					break
				}
				return fmt.Errorf("listing folders for %s: %w", code, err)
			}
			// Strip "course files/" prefix from FullName.
			path := folder.FullName
			path = strings.TrimPrefix(path, "course files/")
			path = strings.TrimPrefix(path, "course files")
			path = strings.TrimPrefix(path, "/")
			folderPaths[folder.ID] = path
		}

		if len(folderPaths) == 0 {
			continue
		}

		// Fetch files.
		for file, err := range canvas.ListFiles(ctx, client, course.ID, canvas.ListFilesOptions{}) {
			if err != nil {
				if errors.Is(err, canvas.ErrForbidden) {
					_, _ = fmt.Fprintf(ios.ErrOut, "  %s  Files restricted, skipping\n", code)
					break
				}
				return fmt.Errorf("listing files for %s: %w", code, err)
			}

			// Determine local path.
			folderPath := folderPaths[file.FolderID]
			localPath := filepath.Join(syncDir, code, folderPath, file.DisplayName)

			// Check if download is needed.
			entry, entryErr := db.GetFileCacheEntry(file.ID)
			needsDownload := true
			if entryErr == nil {
				// Entry exists — check if file changed.
				if entry.Size == file.Size && entry.ModifiedAt == file.ModifiedAt.Format(time.RFC3339) {
					needsDownload = false
				}
			} else if !errors.Is(entryErr, sql.ErrNoRows) {
				return fmt.Errorf("checking file cache: %w", entryErr)
			}

			if !needsDownload {
				totalSkipped++
				continue
			}

			relPath := filepath.Join(code, folderPath, file.DisplayName)

			if dryRun {
				_, _ = fmt.Fprintf(ios.Out, "  [DOWNLOAD]  %s  (%s)\n", relPath, cmdutil.FormatFileSize(file.Size))
				totalDownloaded++
				totalBytes += file.Size
				continue
			}

			// Create directory and download.
			dir := filepath.Dir(localPath)
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("creating directory %s: %w", dir, err)
			}

			dest, err := os.Create(localPath)
			if err != nil {
				return fmt.Errorf("creating file %s: %w", localPath, err)
			}

			n, err := canvas.DownloadFile(ctx, client, file.ID, dest)
			_ = dest.Close()
			if err != nil {
				_ = os.Remove(localPath)
				return fmt.Errorf("downloading %s: %w", file.DisplayName, err)
			}

			// Update file cache entry.
			_ = db.UpsertFileCacheEntry(cache.FileCacheEntry{
				CanvasID:   file.ID,
				CourseID:   course.ID,
				Filename:   file.DisplayName,
				Size:       file.Size,
				ModifiedAt: file.ModifiedAt.Format(time.RFC3339),
				LocalPath:  localPath,
			})

			totalDownloaded++
			totalBytes += n
			_, _ = fmt.Fprintf(ios.ErrOut, "  [OK]  %s  (%s)\n", relPath, cmdutil.FormatFileSize(n))
		}
	}

	elapsed := time.Since(start).Round(100 * time.Millisecond)
	action := "Downloaded"
	if dryRun {
		action = "Would download"
	}
	_, _ = fmt.Fprintf(ios.ErrOut, "\n%s %d files (%s), skipped %d unchanged, %s\n",
		action, totalDownloaded, cmdutil.FormatFileSize(totalBytes), totalSkipped, elapsed)

	return nil
}

// expandHome replaces a leading ~ with the user's home directory.
func expandHome(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[1:])
}
