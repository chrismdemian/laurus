package cmdutil

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/chrismdemian/laurus/internal/iostreams"
)

// Palette holds resolved color styles for the current terminal.
type Palette struct {
	Overdue   lipgloss.Style
	DueToday  lipgloss.Style
	Submitted lipgloss.Style
	Graded    lipgloss.Style
	Concluded lipgloss.Style
	Neutral   lipgloss.Style
	Header    lipgloss.Style
	Muted     lipgloss.Style
}

// NewPalette creates a Palette appropriate for the given IOStreams.
// If color is disabled, all styles are no-ops.
func NewPalette(ios *iostreams.IOStreams) *Palette {
	if !ios.ColorEnabled {
		s := lipgloss.NewStyle()
		return &Palette{
			Overdue:   s,
			DueToday:  s,
			Submitted: s,
			Graded:    s,
			Concluded: s,
			Neutral:   s,
			Header:    s,
			Muted:     s,
		}
	}

	return &Palette{
		Overdue:   lipgloss.NewStyle().Foreground(lipgloss.Color("1")), // red
		DueToday:  lipgloss.NewStyle().Foreground(lipgloss.Color("3")), // yellow
		Submitted: lipgloss.NewStyle().Foreground(lipgloss.Color("2")), // green
		Graded:    lipgloss.NewStyle().Foreground(lipgloss.Color("6")), // cyan
		Concluded: lipgloss.NewStyle().Faint(true),
		Neutral:   lipgloss.NewStyle(),
		Header:    lipgloss.NewStyle().Bold(true),
		Muted:     lipgloss.NewStyle().Faint(true),
	}
}

// CourseStateStyle returns the appropriate style for a course workflow state.
func (p *Palette) CourseStateStyle(workflowState string) lipgloss.Style {
	switch workflowState {
	case "completed":
		return p.Concluded
	case "available":
		return p.Neutral
	default:
		return p.Muted
	}
}
