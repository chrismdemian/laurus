package pathsafe

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestName(t *testing.T) {
	cases := map[string]string{
		"CSC108":                "CSC108",
		"../../.zshrc":          "..-..-.zshrc", // flat name, no separators left
		"..":                    "_",
		".":                     "_",
		"":                      "_",
		"  ":                    "_",
		"a/b":                   "a-b",
		"a\\b":                  "a-b",
		"Week 1: Intro?":        "Week 1 - Intro",
		"evil\x00name":          "evilname",
		"/etc/passwd":           "-etc-passwd",
		"Lecture   notes  .pdf": "Lecture notes .pdf",
	}
	for in, want := range cases {
		if got := Name(in); got != want {
			t.Errorf("Name(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestComponents(t *testing.T) {
	got := Components("course files/Week 1/../../../etc/./secret")
	want := []string{"course files", "Week 1", "etc", "secret"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("Components = %v, want %v", got, want)
	}
	if len(Components("")) != 0 || len(Components("/../")) != 0 {
		t.Error("empty or dot-only paths must yield no components")
	}
}

func TestJoin_NeverEscapes(t *testing.T) {
	root := filepath.Join(t.TempDir(), "School")
	attacks := [][]string{
		{"../Documents"},
		{"CSC108", "..", "..", ".zshrc"},
		{"CSC108", "../../.zshrc"},
		{"/etc", "passwd"},
		{"..\\..\\win"},
	}
	for _, parts := range attacks {
		// Names are sanitised first, as the callers do.
		safe := make([]string, len(parts))
		for i, p := range parts {
			safe[i] = Name(p)
		}
		got, err := Join(root, safe...)
		if err != nil {
			t.Errorf("Join(%v) after Name = error %v; sanitised parts must join", parts, err)
			continue
		}
		if Within(root, got) != nil {
			t.Errorf("Join(%v) = %q escapes %q", parts, got, root)
		}
	}

	// Raw (unsanitised) traversal is refused by the containment check.
	for _, parts := range attacks {
		if _, err := Join(root, parts...); err != nil && !errors.Is(err, ErrEscapesRoot) {
			t.Errorf("Join(raw %v): unexpected error type %v", parts, err)
		}
	}
	if _, err := Join(root, "..", "x"); !errors.Is(err, ErrEscapesRoot) {
		t.Errorf("raw .. must be refused, got %v", err)
	}
	if _, err := Join(root, "CSC108", "Week 1", "notes.pdf"); err != nil {
		t.Errorf("benign join refused: %v", err)
	}
	if err := Within(root, root); err != nil {
		t.Errorf("root is within itself: %v", err)
	}
	if err := Within(root, root+"-other"); err == nil {
		t.Error("sibling with the same prefix must not count as within")
	}
}
