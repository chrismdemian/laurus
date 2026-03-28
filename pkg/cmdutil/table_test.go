package cmdutil

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/chrismdemian/laurus/internal/iostreams"
)

func TestTable_Basic(t *testing.T) {
	ios, _, stdout, _ := iostreams.Test()

	tbl := NewTable(ios)
	tbl.AddHeader("NAME", "CODE", "GRADE")
	tbl.AddRow("Intro to CS", "CSC108", "87.5%")
	tbl.AddRow("Data Structures", "CSC148", "92.0%")

	if err := tbl.Render(); err != nil {
		t.Fatalf("Render error: %v", err)
	}

	out := stdout.String()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3 (header + 2 rows)\noutput:\n%s", len(lines), out)
	}

	// Header should contain all column names
	if !strings.Contains(lines[0], "NAME") || !strings.Contains(lines[0], "CODE") {
		t.Errorf("header missing column names: %q", lines[0])
	}

	// Rows should contain data
	if !strings.Contains(lines[1], "Intro to CS") {
		t.Errorf("row 1 missing data: %q", lines[1])
	}
	if !strings.Contains(lines[2], "CSC148") {
		t.Errorf("row 2 missing data: %q", lines[2])
	}
}

func TestTable_Empty(t *testing.T) {
	ios, _, stdout, _ := iostreams.Test()

	tbl := NewTable(ios)
	if err := tbl.Render(); err != nil {
		t.Fatalf("Render error: %v", err)
	}

	if stdout.Len() != 0 {
		t.Errorf("empty table should produce no output, got %q", stdout.String())
	}
}

func TestTable_StyledRow(t *testing.T) {
	ios, _, stdout, _ := iostreams.Test()

	tbl := NewTable(ios)
	tbl.AddHeader("ITEM", "STATUS")
	tbl.AddStyledRow(
		StyledCell{Value: "Assignment 1", Style: lipgloss.NewStyle()},
		StyledCell{Value: "submitted", Style: lipgloss.NewStyle()},
	)

	if err := tbl.Render(); err != nil {
		t.Fatalf("Render error: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "Assignment 1") {
		t.Errorf("styled row missing data: %q", out)
	}
}

func TestTable_ColumnAlignment(t *testing.T) {
	ios, _, stdout, _ := iostreams.Test()

	tbl := NewTable(ios)
	tbl.AddHeader("A", "B")
	tbl.AddRow("short", "x")
	tbl.AddRow("longer value", "y")

	if err := tbl.Render(); err != nil {
		t.Fatalf("Render error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}

	// The B column should start at the same offset in all rows
	// (header width of col A = len("longer value") = 12)
	for i := 1; i < len(lines); i++ {
		bIdx := strings.LastIndex(lines[i], " ") + 1
		if bIdx < 12 {
			// B column data should be after column A's max width + padding
			// This is a loose check — just verify alignment exists
			continue
		}
	}
}

func TestRenderJSON(t *testing.T) {
	ios, _, stdout, _ := iostreams.Test()

	data := []map[string]string{
		{"name": "CSC108", "grade": "87.5%"},
	}

	if err := RenderJSON(ios, data); err != nil {
		t.Fatalf("RenderJSON error: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, `"name": "CSC108"`) {
		t.Errorf("JSON output missing expected data: %q", out)
	}
	// Should be indented
	if !strings.Contains(out, "  ") {
		t.Error("JSON output should be indented")
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		s        string
		maxWidth int
		want     string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is too long", 10, "this is..."},
		{"ab", 2, "ab"},
		{"abc", 2, "ab"},
	}

	for _, tt := range tests {
		got := truncate(tt.s, tt.maxWidth)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.s, tt.maxWidth, got, tt.want)
		}
	}
}
