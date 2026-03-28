package cmdutil

import (
	"strings"
	"testing"
	"time"
)

func TestRelativeTimeFrom(t *testing.T) {
	ref := time.Date(2025, 10, 15, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		t    time.Time
		want string
	}{
		{"just now (0s)", ref, "just now"},
		{"30s ago", ref.Add(-30 * time.Second), "just now"},
		{"30s future", ref.Add(30 * time.Second), "just now"},
		{"5m ago", ref.Add(-5 * time.Minute), "5m ago"},
		{"5m future", ref.Add(5 * time.Minute), "in 5m"},
		{"59m ago", ref.Add(-59 * time.Minute), "59m ago"},
		{"2h ago", ref.Add(-2 * time.Hour), "2h ago"},
		{"6h future", ref.Add(6 * time.Hour), "in 6h"},
		{"23h ago", ref.Add(-23 * time.Hour), "23h ago"},
		{"yesterday", ref.Add(-30 * time.Hour), "yesterday"},
		{"tomorrow", ref.Add(30 * time.Hour), "tomorrow"},
		{"3 days ago", ref.Add(-3 * 24 * time.Hour), "3 days ago"},
		{"in 5 days", ref.Add(5 * 24 * time.Hour), "in 5 days"},
		{"2 weeks ago", ref.Add(-14 * 24 * time.Hour), "Oct 1"},
		{"months away", ref.Add(60 * 24 * time.Hour), "Dec 14"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RelativeTimeFrom(tt.t, ref)
			if got != tt.want {
				t.Errorf("RelativeTimeFrom(%v, %v) = %q, want %q", tt.t, ref, got, tt.want)
			}
		})
	}
}

func TestFormatDueDate_Nil(t *testing.T) {
	got := FormatDueDate(nil, time.UTC)
	if got != "No due date" {
		t.Errorf("FormatDueDate(nil) = %q, want %q", got, "No due date")
	}
}

func TestFormatDueDate_WithTimezone(t *testing.T) {
	// 3:59 AM UTC = 11:59 PM EDT (previous day, UTC-4 during DST)
	due := time.Date(2025, 10, 16, 3, 59, 0, 0, time.UTC)
	et, _ := time.LoadLocation("America/Toronto")

	got := FormatDueDate(&due, et)
	// Should show the ET time in the absolute part
	if got == "" {
		t.Error("FormatDueDate returned empty string")
	}
	// Verify it contains the ET-converted time (Oct 15 at 11:59 PM)
	if !strings.Contains(got, "Oct 15") {
		t.Errorf("FormatDueDate = %q, expected to contain Oct 15 (ET conversion)", got)
	}
}
