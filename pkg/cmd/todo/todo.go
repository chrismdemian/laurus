// Package todo implements the todo command group for planner notes.
package todo

import (
	"context"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// NewCmdTodo returns the todo command with subcommands.
func NewCmdTodo(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "todo",
		Short: "Manage planner notes and todo items",
		Long:  "Create, complete, and dismiss personal planner notes on Canvas.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newCmdTodoAdd(f))
	cmd.AddCommand(newCmdTodoDone(f))
	cmd.AddCommand(newCmdTodoDismiss(f))

	return cmd
}

type addOpts struct {
	date    string
	course  string
	details string
}

func newCmdTodoAdd(f *cmdutil.Factory) *cobra.Command {
	var opts addOpts

	cmd := &cobra.Command{
		Use:   "add <title>",
		Short: "Create a planner note",
		Long: `Create a personal planner note on Canvas.

The --date flag is required and specifies when the item appears in the planner.
Use ISO 8601 format (e.g., 2026-04-01).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return addRun(f, args[0], opts)
		},
	}

	cmd.Flags().StringVar(&opts.date, "date", "", "Due date in ISO 8601 format (required)")
	cmd.Flags().StringVarP(&opts.course, "course", "c", "", "Associate with a course")
	cmd.Flags().StringVar(&opts.details, "details", "", "Note details/description")
	_ = cmd.MarkFlagRequired("date")

	return cmd
}

func addRun(f *cmdutil.Factory, title string, opts addOpts) error {
	client, err := f.Client()
	if err != nil {
		return err
	}
	ios := f.IOStreams()
	ctx := context.Background()

	req := canvas.CreatePlannerNoteRequest{
		Title:    title,
		TodoDate: opts.date,
		Details:  opts.details,
	}

	// Resolve course if provided
	if opts.course != "" {
		course, err := canvas.FindCourse(ctx, client, opts.course)
		if err != nil {
			return fmt.Errorf("finding course %q: %w", opts.course, err)
		}
		req.CourseID = &course.ID
	}

	note, err := canvas.CreatePlannerNote(ctx, client, req)
	if err != nil {
		return fmt.Errorf("creating planner note: %w", err)
	}

	_, _ = fmt.Fprintf(ios.Out, "Created todo %q (ID: %d) for %s.\n", note.Title, note.ID, opts.date)
	return nil
}

func newCmdTodoDone(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "done <note-id>",
		Short: "Mark a planner note as complete",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return overrideRun(f, args[0], true, false)
		},
	}
	return cmd
}

func newCmdTodoDismiss(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dismiss <note-id>",
		Short: "Dismiss a planner note from the planner",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return overrideRun(f, args[0], false, true)
		},
	}
	return cmd
}

func overrideRun(f *cmdutil.Factory, idStr string, markedComplete, dismissed bool) error {
	client, err := f.Client()
	if err != nil {
		return err
	}
	ios := f.IOStreams()
	ctx := context.Background()

	noteID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid note ID %q: must be a number", idStr)
	}

	_, err = canvas.CreatePlannerOverride(ctx, client, "planner_note", noteID, markedComplete, dismissed)
	if err != nil {
		return fmt.Errorf("updating planner item: %w", err)
	}

	action := "Completed"
	if dismissed {
		action = "Dismissed"
	}
	_, _ = fmt.Fprintf(ios.Out, "%s planner note %d.\n", action, noteID)
	return nil
}
