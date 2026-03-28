package modules

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// NewCmdMarkDone returns the mark-done command.
func NewCmdMarkDone(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mark-done <course> <item>",
		Short: "Mark a module item as complete",
		Long:  "Mark a module item as done (for items with a must_mark_done completion requirement).",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return markDoneRun(f, args[0], args[1])
		},
	}
	return cmd
}

func markDoneRun(f *cmdutil.Factory, courseQuery, itemQuery string) error {
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

	// Fetch all modules with items to search for the item
	moduleID, item, err := findModuleItem(ctx, client, course.ID, itemQuery)
	if err != nil {
		return err
	}

	if err := canvas.MarkModuleItemDone(ctx, client, course.ID, moduleID, item.ID); err != nil {
		return fmt.Errorf("marking item done: %w", err)
	}

	_, _ = fmt.Fprintf(ios.Out, "Marked %q as done in %s.\n", item.Title, course.CourseCode)
	return nil
}

// findModuleItem searches all modules for an item matching the query.
// Returns the module ID and the matched item.
func findModuleItem(ctx context.Context, c *canvas.Client, courseID int64, query string) (int64, canvas.ModuleItem, error) {
	var allItems []struct {
		moduleID int64
		item     canvas.ModuleItem
	}

	for m, err := range canvas.ListModules(ctx, c, courseID, canvas.ListModulesOptions{
		IncludeItems: true,
	}) {
		if err != nil {
			return 0, canvas.ModuleItem{}, fmt.Errorf("listing modules: %w", err)
		}
		for _, item := range m.Items {
			allItems = append(allItems, struct {
				moduleID int64
				item     canvas.ModuleItem
			}{m.ID, item})
		}
	}

	q := strings.ToLower(query)

	// Exact title match
	for _, entry := range allItems {
		if strings.EqualFold(entry.item.Title, query) {
			return entry.moduleID, entry.item, nil
		}
	}

	// Substring match
	for _, entry := range allItems {
		if strings.Contains(strings.ToLower(entry.item.Title), q) {
			return entry.moduleID, entry.item, nil
		}
	}

	return 0, canvas.ModuleItem{}, fmt.Errorf("no module item matching %q: %w", query, canvas.ErrNotFound)
}
