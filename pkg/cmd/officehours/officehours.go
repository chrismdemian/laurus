// Package officehours implements the office-hours command group.
package officehours

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// NewCmdOfficeHours returns the office-hours command with subcommands.
func NewCmdOfficeHours(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "office-hours [course]",
		Aliases: []string{"oh"},
		Short:   "List available office hours and book appointment slots",
		Long:    "View reservable appointment slots from your courses and book them.",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			course := ""
			if len(args) > 0 {
				course = args[0]
			}
			return listRun(f, course)
		},
	}

	cmd.AddCommand(newCmdBook(f))

	return cmd
}

func listRun(f *cmdutil.Factory, courseQuery string) error {
	client, err := f.Client()
	if err != nil {
		return err
	}
	ios := f.IOStreams()
	ctx := context.Background()

	groups, err := canvas.ListAppointmentGroups(ctx, client)
	if err != nil {
		return fmt.Errorf("listing appointment groups: %w", err)
	}

	// Filter by course if specified.
	if courseQuery != "" {
		course, err := canvas.FindCourse(ctx, client, courseQuery)
		if err != nil {
			return fmt.Errorf("finding course %q: %w", courseQuery, err)
		}
		contextCode := fmt.Sprintf("course_%d", course.ID)
		var filtered []canvas.AppointmentGroup
		for _, g := range groups {
			for _, cc := range g.ContextCodes {
				if cc == contextCode {
					filtered = append(filtered, g)
					break
				}
			}
		}
		groups = filtered
	}

	if ios.IsJSON {
		return cmdutil.RenderJSON(ios, groups)
	}

	if len(groups) == 0 {
		_, _ = fmt.Fprintln(ios.Out, "No available office hours found.")
		return nil
	}

	palette := cmdutil.NewPalette(ios)
	tbl := cmdutil.NewTable(ios)
	tbl.AddHeader("GROUP", "SLOT ID", "TIME", "SPOTS")

	for _, g := range groups {
		for _, slot := range g.Appointments {
			timeStr := ""
			if slot.StartAt != nil {
				if slot.EndAt != nil {
					timeStr = fmt.Sprintf("%s — %s",
						slot.StartAt.Local().Format("Mon Jan 2 3:04 PM"),
						slot.EndAt.Local().Format("3:04 PM"))
				} else {
					timeStr = slot.StartAt.Local().Format("Mon Jan 2 3:04 PM")
				}
			}

			spots := "available"
			style := palette.Neutral
			if slot.WorkflowState == "locked" {
				style = palette.Muted
				spots = "full"
			} else if g.ParticipantCount > 0 {
				spots = fmt.Sprintf("%d booked", g.ParticipantCount)
			}

			tbl.AddStyledRow(
				cmdutil.StyledCell{Value: g.Title, Style: style},
				cmdutil.StyledCell{Value: strconv.FormatInt(slot.ID, 10), Style: style},
				cmdutil.StyledCell{Value: timeStr, Style: style},
				cmdutil.StyledCell{Value: spots, Style: style},
			)
		}
	}

	_ = ios.StartPager()
	defer ios.StopPager()
	return tbl.Render()
}

func newCmdBook(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "book <slot-id>",
		Short: "Reserve an appointment slot",
		Long:  "Book an available office hours time slot by its ID.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slotID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid slot ID %q: %w", args[0], err)
			}
			return bookRun(f, slotID)
		},
	}
	return cmd
}

func bookRun(f *cmdutil.Factory, slotID int64) error {
	client, err := f.Client()
	if err != nil {
		return err
	}
	ios := f.IOStreams()
	ctx := context.Background()

	reservation, err := canvas.ReserveAppointmentSlot(ctx, client, slotID)
	if err != nil {
		if strings.Contains(err.Error(), "already reserved") || strings.Contains(err.Error(), "full") {
			return fmt.Errorf("slot %d is not available: %w", slotID, err)
		}
		return fmt.Errorf("booking slot %d: %w", slotID, err)
	}

	if ios.IsJSON {
		return cmdutil.RenderJSON(ios, reservation)
	}

	timeStr := "unknown time"
	if reservation.StartAt != nil {
		timeStr = reservation.StartAt.Local().Format("Mon Jan 2, 3:04 PM")
	}

	_, _ = fmt.Fprintf(ios.Out, "Booked: %s at %s\n", reservation.Title, timeStr)
	return nil
}
