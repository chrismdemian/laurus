// Package modules implements the modules command group.
package modules

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/internal/iostreams"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// NewCmdModules returns the modules list command.
func NewCmdModules(f *cmdutil.Factory) *cobra.Command {
	var opts listOpts

	cmd := &cobra.Command{
		Use:   "modules <course>",
		Short: "List course modules",
		Long:  "Display modules with item counts, completion status, and optional tree view.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Course = args[0]
			return listRun(f, opts)
		},
	}

	cmd.Flags().BoolVar(&opts.Tree, "tree", false, "Show modules as a tree with items")
	cmd.Flags().BoolVar(&opts.Progress, "progress", false, "Show completion progress per module")

	return cmd
}

type listOpts struct {
	Course   string
	Tree     bool
	Progress bool
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

	// Always fetch items so we can show counts and completion
	var modules []canvas.Module
	for m, err := range canvas.ListModules(ctx, client, course.ID, canvas.ListModulesOptions{
		IncludeItems:          true,
		IncludeContentDetails: opts.Tree,
	}) {
		if err != nil {
			return fmt.Errorf("listing modules: %w", err)
		}
		modules = append(modules, m)
	}

	if ios.IsJSON {
		return cmdutil.RenderJSON(ios, modules)
	}

	if len(modules) == 0 {
		_, _ = fmt.Fprintln(ios.Out, "No modules found.")
		return nil
	}

	if opts.Tree {
		return renderTree(ios, modules)
	}

	return renderTable(ios, modules, opts.Progress)
}

func renderTable(ios *iostreams.IOStreams, modules []canvas.Module, showProgress bool) error {
	palette := cmdutil.NewPalette(ios)
	tbl := cmdutil.NewTable(ios)

	if showProgress {
		tbl.AddHeader("MODULE", "ITEMS", "COMPLETED", "PROGRESS")
	} else {
		tbl.AddHeader("MODULE", "ITEMS", "STATUS")
	}

	for _, m := range modules {
		style := moduleStyle(palette, m)
		completed, total := completionCounts(m)

		if showProgress {
			pct := ""
			if total > 0 {
				pct = fmt.Sprintf("%d%%", completed*100/total)
			}
			tbl.AddStyledRow(
				cmdutil.StyledCell{Value: m.Name, Style: style},
				cmdutil.StyledCell{Value: fmt.Sprintf("%d", len(m.Items)), Style: style},
				cmdutil.StyledCell{Value: fmt.Sprintf("%d/%d", completed, total), Style: style},
				cmdutil.StyledCell{Value: pct, Style: style},
			)
		} else {
			tbl.AddStyledRow(
				cmdutil.StyledCell{Value: m.Name, Style: style},
				cmdutil.StyledCell{Value: fmt.Sprintf("%d", len(m.Items)), Style: style},
				cmdutil.StyledCell{Value: moduleStatus(m), Style: style},
			)
		}
	}

	_ = ios.StartPager()
	defer ios.StopPager()
	return tbl.Render()
}

func renderTree(ios *iostreams.IOStreams, modules []canvas.Module) error {
	palette := cmdutil.NewPalette(ios)

	var roots []*cmdutil.TreeNode
	for _, m := range modules {
		mNode := &cmdutil.TreeNode{
			Label: m.Name,
			Style: moduleStyle(palette, m),
		}

		for _, item := range m.Items {
			detail := itemDetail(item)
			iNode := &cmdutil.TreeNode{
				Label:  fmt.Sprintf("[%s] %s", itemTypeShort(item.Type), item.Title),
				Style:  itemStyle(palette, item),
				Detail: detail,
			}
			mNode.Children = append(mNode.Children, iNode)
		}

		roots = append(roots, mNode)
	}

	_ = ios.StartPager()
	defer ios.StopPager()
	return cmdutil.RenderTree(ios, roots)
}

func itemDetail(item canvas.ModuleItem) string {
	var parts []string

	if item.ContentDetails != nil {
		if item.ContentDetails.DueAt != nil {
			parts = append(parts, cmdutil.RelativeTime(*item.ContentDetails.DueAt))
		}
		if item.ContentDetails.PointsPossible != nil {
			parts = append(parts, fmt.Sprintf("%.0f pts", *item.ContentDetails.PointsPossible))
		}
	}

	if item.CompletionRequirement != nil && item.CompletionRequirement.Completed {
		parts = append(parts, "completed")
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "  ")
}

func itemTypeShort(t string) string {
	switch t {
	case "Assignment":
		return "Assignment"
	case "Quiz":
		return "Quiz"
	case "File":
		return "File"
	case "Page":
		return "Page"
	case "Discussion":
		return "Discussion"
	case "ExternalUrl":
		return "Link"
	case "ExternalTool":
		return "Tool"
	case "SubHeader":
		return "Header"
	default:
		return t
	}
}

func moduleStyle(palette *cmdutil.Palette, m canvas.Module) lipgloss.Style {
	if m.State != nil {
		switch *m.State {
		case "completed":
			return palette.Submitted
		case "locked":
			return palette.Muted
		}
	}
	return palette.Neutral
}

func itemStyle(palette *cmdutil.Palette, item canvas.ModuleItem) lipgloss.Style {
	if item.CompletionRequirement != nil && item.CompletionRequirement.Completed {
		return palette.Submitted
	}
	if item.ContentDetails != nil && item.ContentDetails.LockedForUser {
		return palette.Muted
	}
	return palette.Neutral
}

func moduleStatus(m canvas.Module) string {
	if m.State != nil {
		switch *m.State {
		case "completed":
			return "Completed"
		case "locked":
			return "Locked"
		case "started":
			return "In Progress"
		case "unlocked":
			return "Available"
		}
	}
	return ""
}

func completionCounts(m canvas.Module) (completed, total int) {
	for _, item := range m.Items {
		if item.CompletionRequirement != nil {
			total++
			if item.CompletionRequirement.Completed {
				completed++
			}
		}
	}
	return
}
