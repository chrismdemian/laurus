package mcp

import (
	"errors"
	"sort"
	"testing"

	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// writeToolNames is every tool that mutates Canvas state. Keep in sync with
// registerWriteTools; TestReadOnlyModeHidesWriteTools fails if they drift.
var writeToolNames = []string{
	"submit_assignment",
	"mark_todo_done",
	"create_todo",
	"dismiss_todo",
	"mark_module_item_done",
	"book_office_hours",
	"reply_to_discussion",
	"reply_to_conversation",
	"send_message",
}

// readToolNames is a sample of read tools that must be present in both modes.
var readToolNames = []string{
	"list_courses",
	"list_assignments",
	"get_grades",
	"list_inbox",
	"read_conversation",
	"list_discussions",
	"list_office_hours",
	"get_todo",
}

func testFactory() *cmdutil.Factory {
	return &cmdutil.Factory{
		Version: "test",
		Client: func() (*canvas.Client, error) {
			return nil, errors.New("no client in tests")
		},
	}
}

func toolNames(t *testing.T, readOnly bool) map[string]bool {
	t.Helper()
	srv := NewServer(testFactory(), readOnly)
	names := map[string]bool{}
	for name := range srv.ListTools() {
		names[name] = true
	}
	if len(names) == 0 {
		t.Fatalf("readOnly=%v: server registered no tools", readOnly)
	}
	return names
}

func TestNormalModeRegistersWriteTools(t *testing.T) {
	names := toolNames(t, false)
	for _, w := range writeToolNames {
		if !names[w] {
			t.Errorf("normal mode: write tool %q not registered", w)
		}
	}
	for _, r := range readToolNames {
		if !names[r] {
			t.Errorf("normal mode: read tool %q not registered", r)
		}
	}
}

func TestReadOnlyModeHidesWriteTools(t *testing.T) {
	full := toolNames(t, false)
	ro := toolNames(t, true)

	for _, w := range writeToolNames {
		if ro[w] {
			t.Errorf("read-only mode: write tool %q is registered", w)
		}
	}
	for _, r := range readToolNames {
		if !ro[r] {
			t.Errorf("read-only mode: read tool %q missing", r)
		}
	}

	// The read-only set must be exactly the full set minus the write tools:
	// nothing else may disappear, and nothing unlisted may remain.
	expected := map[string]bool{}
	for name := range full {
		expected[name] = true
	}
	for _, w := range writeToolNames {
		delete(expected, w)
	}
	if len(ro) != len(expected) {
		t.Errorf("read-only tool count = %d, want %d", len(ro), len(expected))
	}
	for name := range ro {
		if !expected[name] {
			t.Errorf("read-only mode exposes unexpected tool %q", name)
		}
	}
	for name := range expected {
		if !ro[name] {
			t.Errorf("read-only mode dropped read tool %q", name)
		}
	}
}

// TestWriteToolListMatchesSeam guards the invariant that every tool
// registerWriteTools adds is in writeToolNames and vice versa, so a new
// mutating tool cannot be added without updating the read-only contract.
func TestWriteToolListMatchesSeam(t *testing.T) {
	full := toolNames(t, false)
	ro := toolNames(t, true)

	var seam []string
	for name := range full {
		if !ro[name] {
			seam = append(seam, name)
		}
	}
	sort.Strings(seam)
	want := append([]string(nil), writeToolNames...)
	sort.Strings(want)

	if len(seam) != len(want) {
		t.Fatalf("write seam = %v, want %v", seam, want)
	}
	for i := range seam {
		if seam[i] != want[i] {
			t.Fatalf("write seam = %v, want %v", seam, want)
		}
	}
}
