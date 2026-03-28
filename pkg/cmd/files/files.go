// Package files implements the files command group.
package files

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/internal/iostreams"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// NewCmdFiles returns the files list command.
func NewCmdFiles(f *cmdutil.Factory) *cobra.Command {
	var opts listOpts

	cmd := &cobra.Command{
		Use:   "files <course>",
		Short: "List course files",
		Long:  "Display files with size, type, and last update. Use --tree for a folder view.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Course = args[0]
			return listRun(f, opts)
		},
	}

	cmd.Flags().BoolVar(&opts.Tree, "tree", false, "Show files in a folder tree")

	return cmd
}

type listOpts struct {
	Course string
	Tree   bool
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

	if opts.Tree {
		return renderFileTree(ctx, client, ios, course)
	}

	return renderFileTable(ctx, client, ios, course)
}

func renderFileTable(ctx context.Context, client *canvas.Client, ios *iostreams.IOStreams, course canvas.Course) error {
	var files []canvas.File
	for f, err := range canvas.ListFiles(ctx, client, course.ID, canvas.ListFilesOptions{
		Sort:  "name",
		Order: "asc",
	}) {
		if err != nil {
			if errors.Is(err, canvas.ErrForbidden) {
				_, _ = fmt.Fprintln(ios.Out, "You don't have permission to view files in this course.")
				return nil
			}
			if errors.Is(err, canvas.ErrNotFound) {
				_, _ = fmt.Fprintln(ios.Out, "Files are disabled for this course.")
				return nil
			}
			return fmt.Errorf("listing files: %w", err)
		}
		files = append(files, f)
	}

	if ios.IsJSON {
		return cmdutil.RenderJSON(ios, files)
	}

	if len(files) == 0 {
		_, _ = fmt.Fprintln(ios.Out, "No files found.")
		return nil
	}

	palette := cmdutil.NewPalette(ios)
	tbl := cmdutil.NewTable(ios)
	tbl.AddHeader("NAME", "SIZE", "TYPE", "UPDATED")

	for _, f := range files {
		style := palette.Neutral
		if f.HiddenForUser || f.LockedForUser {
			style = palette.Muted
		}

		fileType := f.MimeClass
		if fileType == "" {
			fileType = f.ContentType
		}

		tbl.AddStyledRow(
			cmdutil.StyledCell{Value: f.DisplayName, Style: style},
			cmdutil.StyledCell{Value: cmdutil.FormatFileSize(f.Size), Style: style},
			cmdutil.StyledCell{Value: fileType, Style: style},
			cmdutil.StyledCell{Value: cmdutil.RelativeTime(f.UpdatedAt), Style: style},
		)
	}

	_ = ios.StartPager()
	defer ios.StopPager()
	return tbl.Render()
}

func renderFileTree(ctx context.Context, client *canvas.Client, ios *iostreams.IOStreams, course canvas.Course) error {
	// Fetch all folders and files
	var folders []canvas.Folder
	for f, err := range canvas.ListFolders(ctx, client, course.ID) {
		if err != nil {
			if errors.Is(err, canvas.ErrForbidden) {
				_, _ = fmt.Fprintln(ios.Out, "You don't have permission to view files in this course.")
				return nil
			}
			return fmt.Errorf("listing folders: %w", err)
		}
		folders = append(folders, f)
	}

	var files []canvas.File
	for f, err := range canvas.ListFiles(ctx, client, course.ID, canvas.ListFilesOptions{}) {
		if err != nil {
			if errors.Is(err, canvas.ErrForbidden) {
				_, _ = fmt.Fprintln(ios.Out, "You don't have permission to view files in this course.")
				return nil
			}
			return fmt.Errorf("listing files: %w", err)
		}
		files = append(files, f)
	}

	if ios.IsJSON {
		data := struct {
			Folders []canvas.Folder `json:"folders"`
			Files   []canvas.File   `json:"files"`
		}{folders, files}
		return cmdutil.RenderJSON(ios, data)
	}

	// Build folder tree
	palette := cmdutil.NewPalette(ios)
	roots := buildFolderTree(palette, folders, files)

	if len(roots) == 0 {
		_, _ = fmt.Fprintln(ios.Out, "No files found.")
		return nil
	}

	_ = ios.StartPager()
	defer ios.StopPager()
	return cmdutil.RenderTree(ios, roots)
}

// buildFolderTree constructs a tree of TreeNodes from a flat list of folders and files.
func buildFolderTree(palette *cmdutil.Palette, folders []canvas.Folder, files []canvas.File) []*cmdutil.TreeNode {
	// Map folder ID -> TreeNode
	nodeMap := make(map[int64]*cmdutil.TreeNode)
	folderMap := make(map[int64]*canvas.Folder)
	for i := range folders {
		f := &folders[i]
		folderMap[f.ID] = f
		nodeMap[f.ID] = &cmdutil.TreeNode{
			Label: f.Name + "/",
			Style: palette.Header,
		}
	}

	// Group files by folder
	filesByFolder := make(map[int64][]canvas.File)
	for _, f := range files {
		filesByFolder[f.FolderID] = append(filesByFolder[f.FolderID], f)
	}

	// Sort files within each folder by name
	for fid := range filesByFolder {
		sort.Slice(filesByFolder[fid], func(i, j int) bool {
			return filesByFolder[fid][i].DisplayName < filesByFolder[fid][j].DisplayName
		})
	}

	// Add files as children of their folders
	for fid, folderFiles := range filesByFolder {
		node, ok := nodeMap[fid]
		if !ok {
			continue
		}
		for _, f := range folderFiles {
			style := palette.Neutral
			if f.HiddenForUser || f.LockedForUser {
				style = palette.Muted
			}
			node.Children = append(node.Children, &cmdutil.TreeNode{
				Label:  f.DisplayName,
				Style:  style,
				Detail: cmdutil.FormatFileSize(f.Size),
			})
		}
	}

	// Build parent-child relationships between folders
	var roots []*cmdutil.TreeNode
	for _, f := range folders {
		node := nodeMap[f.ID]
		if f.ParentFolderID == nil {
			roots = append(roots, node)
		} else if parentNode, ok := nodeMap[*f.ParentFolderID]; ok {
			parentNode.Children = append(parentNode.Children, node)
		} else {
			// Orphan folder — treat as root
			roots = append(roots, node)
		}
	}

	// Sort root-level nodes by name
	sort.Slice(roots, func(i, j int) bool {
		return roots[i].Label < roots[j].Label
	})

	return roots
}
