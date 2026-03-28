package cmdutil

import (
	"fmt"
	"math"
	"time"
)

// RelativeTime formats a time as a human-readable relative string (e.g., "in 3h", "2 days ago").
func RelativeTime(t time.Time) string {
	return RelativeTimeFrom(t, time.Now())
}

// RelativeTimeFrom formats t relative to ref. Use this variant in tests for deterministic output.
func RelativeTimeFrom(t, ref time.Time) string {
	d := t.Sub(ref)
	abs := math.Abs(d.Seconds())
	future := d > 0

	switch {
	case abs < 60:
		return "just now"
	case abs < 3600:
		m := int(abs / 60)
		if future {
			return fmt.Sprintf("in %dm", m)
		}
		return fmt.Sprintf("%dm ago", m)
	case abs < 86400:
		h := int(abs / 3600)
		if future {
			return fmt.Sprintf("in %dh", h)
		}
		return fmt.Sprintf("%dh ago", h)
	case abs < 172800:
		if future {
			return "tomorrow"
		}
		return "yesterday"
	case abs < 604800:
		d := int(abs / 86400)
		if future {
			return fmt.Sprintf("in %d days", d)
		}
		return fmt.Sprintf("%d days ago", d)
	default:
		return t.Format("Jan 2")
	}
}

// FormatDueDate returns a formatted due date with relative context.
// Returns "No due date" for nil. Converts to the given timezone for display.
func FormatDueDate(t *time.Time, tz *time.Location) string {
	if t == nil {
		return "No due date"
	}

	local := *t
	if tz != nil {
		local = local.In(tz)
	}

	rel := RelativeTimeFrom(*t, time.Now())
	abs := local.Format("Jan 2 3:04 PM")

	return fmt.Sprintf("%s  (%s)", abs, rel)
}
