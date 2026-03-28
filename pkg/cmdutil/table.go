package cmdutil

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/chrismdemian/laurus/internal/iostreams"
)

// StyledCell pairs a string value with a lipgloss style.
type StyledCell struct {
	Value string
	Style lipgloss.Style
}

// Table renders aligned columnar output to an IOStreams writer.
type Table struct {
	ios     *iostreams.IOStreams
	palette *Palette
	headers []string
	rows    [][]StyledCell
}

// NewTable creates a new table renderer.
func NewTable(ios *iostreams.IOStreams) *Table {
	return &Table{
		ios:     ios,
		palette: NewPalette(ios),
	}
}

// AddHeader sets column headers.
func (t *Table) AddHeader(headers ...string) *Table {
	t.headers = headers
	return t
}

// AddRow adds a row of plain (unstyled) string values.
func (t *Table) AddRow(values ...string) *Table {
	cells := make([]StyledCell, len(values))
	for i, v := range values {
		cells[i] = StyledCell{Value: v, Style: t.palette.Neutral}
	}
	t.rows = append(t.rows, cells)
	return t
}

// AddStyledRow adds a row where each cell has an associated style.
func (t *Table) AddStyledRow(cells ...StyledCell) *Table {
	t.rows = append(t.rows, cells)
	return t
}

// Render writes the table to ios.Out.
func (t *Table) Render() error {
	if len(t.headers) == 0 && len(t.rows) == 0 {
		return nil
	}

	numCols := len(t.headers)
	if numCols == 0 && len(t.rows) > 0 {
		numCols = len(t.rows[0])
	}

	// Calculate column widths
	widths := make([]int, numCols)
	for i, h := range t.headers {
		if len(h) > widths[i] {
			widths[i] = len(h)
		}
	}
	for _, row := range t.rows {
		for i, cell := range row {
			if i < numCols && len(cell.Value) > widths[i] {
				widths[i] = len(cell.Value)
			}
		}
	}

	// Truncate if total width exceeds terminal
	maxWidth := t.ios.TerminalWidth()
	padding := 2 // spaces between columns
	totalWidth := 0
	for _, w := range widths {
		totalWidth += w
	}
	totalWidth += padding * (numCols - 1)

	if maxWidth > 0 && totalWidth > maxWidth {
		// Find the widest column and truncate it
		maxIdx := 0
		for i, w := range widths {
			if w > widths[maxIdx] {
				maxIdx = i
			}
		}
		excess := totalWidth - maxWidth
		if widths[maxIdx] > excess+3 {
			widths[maxIdx] -= excess
		}
	}

	// Render header
	if len(t.headers) > 0 {
		var parts []string
		for i, h := range t.headers {
			padded := padRight(h, widths[i])
			parts = append(parts, t.palette.Header.Render(padded))
		}
		if _, err := fmt.Fprintln(t.ios.Out, strings.Join(parts, "  ")); err != nil {
			return err
		}
	}

	// Render rows
	for _, row := range t.rows {
		var parts []string
		for i, cell := range row {
			if i >= numCols {
				break
			}
			val := truncate(cell.Value, widths[i])
			padded := padRight(val, widths[i])
			parts = append(parts, cell.Style.Render(padded))
		}
		if _, err := fmt.Fprintln(t.ios.Out, strings.Join(parts, "  ")); err != nil {
			return err
		}
	}

	return nil
}

// RenderJSON marshals data as indented JSON to ios.Out.
func RenderJSON(ios *iostreams.IOStreams, data any) error {
	enc := json.NewEncoder(ios.Out)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

func truncate(s string, maxWidth int) string {
	if len(s) <= maxWidth {
		return s
	}
	if maxWidth <= 3 {
		return s[:maxWidth]
	}
	return s[:maxWidth-3] + "..."
}
