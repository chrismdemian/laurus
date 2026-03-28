package cmdutil

import (
	"testing"

	"github.com/chrismdemian/laurus/internal/iostreams"
)

func TestNewPalette_ColorEnabled(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	ios.ColorEnabled = true

	p := NewPalette(ios)

	// Verify palette is constructed with non-zero styles (no panics)
	// Note: lipgloss may not emit ANSI codes without a real terminal,
	// so we just verify the palette is functional.
	if p.Overdue.Render("test") == "" {
		t.Error("Overdue.Render returned empty string")
	}
	if p.Header.Render("test") == "" {
		t.Error("Header.Render returned empty string")
	}
}

func TestNewPalette_ColorDisabled(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	ios.ColorEnabled = false

	p := NewPalette(ios)

	// All styles should be no-ops — output equals input
	if got := p.Overdue.Render("test"); got != "test" {
		t.Errorf("Overdue with color disabled = %q, want %q", got, "test")
	}
	if got := p.Header.Render("test"); got != "test" {
		t.Errorf("Header with color disabled = %q, want %q", got, "test")
	}
	if got := p.Submitted.Render("test"); got != "test" {
		t.Errorf("Submitted with color disabled = %q, want %q", got, "test")
	}
}

func TestPalette_CourseStateStyle(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	ios.ColorEnabled = false

	p := NewPalette(ios)

	// With color disabled, all render to the same plain text
	tests := []string{"available", "completed", "unpublished", "deleted"}
	for _, state := range tests {
		t.Run(state, func(t *testing.T) {
			s := p.CourseStateStyle(state)
			got := s.Render("X")
			if got != "X" {
				t.Errorf("CourseStateStyle(%q).Render(X) = %q, want X (color disabled)", state, got)
			}
		})
	}
}
