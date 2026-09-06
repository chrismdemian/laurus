package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/chrismdemian/laurus/internal/config"
)

func localServer(t *testing.T, syncDir string) *Server {
	t.Helper()
	return &Server{config: func() (*config.Config, error) { return &config.Config{SyncDir: syncDir}, nil }}
}

func runSearch(t *testing.T, s *Server, args searchLocalFilesArgs) (localSearchReport, *mcplib.CallToolResult) {
	t.Helper()
	res, err := s.handleSearchLocalFiles(context.Background(), mcplib.CallToolRequest{}, args)
	if err != nil {
		t.Fatal(err)
	}
	tc := res.Content[0].(mcplib.TextContent)
	if res.IsError {
		return localSearchReport{}, res
	}
	var env struct {
		Source string            `json:"source"`
		Data   localSearchReport `json:"data"`
	}
	if err := json.Unmarshal([]byte(tc.Text), &env); err != nil {
		t.Fatalf("bad result: %v\n%s", err, tc.Text)
	}
	if env.Source != "local" {
		t.Errorf("source = %q, want local", env.Source)
	}
	return env.Data, res
}

func TestSearchLocalFiles(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "School")
	outside := filepath.Join(base, "outside")
	mk := func(rel, content string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	mk("ECE253/Week 1/notes.md", "# Week 1\nThe midterm covers Boolean algebra.\n")
	mk("ECE253/Week 2/lab.txt", "lab 2: build a counter\n")
	mk("ESC203/syllabus.md", "Midterm: October 20.\n")
	mk("ECE253/Midterm 2024.pdf", "%PDF-1.4\x00binary blob midterm inside")
	big := strings.Repeat("midterm ", searchMaxFileBytes/8+10)
	mk("ECE253/huge midterm dump.log", big)
	if err := os.MkdirAll(outside, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret midterm.md"), []byte("midterm answers\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Symlinks: a file link and a directory link both pointing outside the root.
	if err := os.Symlink(filepath.Join(outside, "secret midterm.md"), filepath.Join(root, "ECE253", "link midterm.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked-dir")); err != nil {
		t.Fatal(err)
	}

	s := localServer(t, root)
	rep, _ := runSearch(t, s, searchLocalFilesArgs{Query: "midterm"})

	paths := map[string]localFileMatch{}
	for _, m := range rep.Matches {
		paths[m.Path] = m
	}
	if m, ok := paths[filepath.Join("ECE253", "Week 1", "notes.md")]; !ok || m.Where != "content" || !strings.Contains(m.Snippet, "Boolean") || m.Line != 2 {
		t.Errorf("notes.md match = %+v", m)
	}
	if m, ok := paths[filepath.Join("ESC203", "syllabus.md")]; !ok || m.Where != "content" {
		t.Errorf("syllabus.md match = %+v", m)
	}
	if m, ok := paths[filepath.Join("ECE253", "Midterm 2024.pdf")]; !ok || m.Where != "name" || m.Snippet != "" {
		t.Errorf("pdf must match by name only, got %+v", m)
	}
	if m, ok := paths[filepath.Join("ECE253", "huge midterm dump.log")]; !ok || m.Where != "name" || m.Snippet != "" {
		t.Errorf("oversized file must match by name only, never be read: %+v", m)
	}
	for p := range paths {
		if strings.Contains(p, "link") || strings.Contains(p, "secret") || strings.Contains(p, "outside") {
			t.Errorf("symlinked/outside content leaked: %s", p)
		}
	}
	if rep.Skipped < 2 {
		t.Errorf("skipped = %d, want at least the two symlinks", rep.Skipped)
	}

	// Course filter narrows to one folder.
	rep, _ = runSearch(t, s, searchLocalFilesArgs{Query: "midterm", Course: "ESC203"})
	if len(rep.Matches) != 1 || rep.Matches[0].Path != filepath.Join("ESC203", "syllabus.md") {
		t.Errorf("course filter: %+v", rep.Matches)
	}

	// A traversal in course cannot escape the root: it becomes a plain (missing) folder name.
	if _, res := runSearch(t, s, searchLocalFilesArgs{Query: "midterm", Course: "../outside"}); res == nil || !res.IsError {
		t.Error("traversal course must be refused")
	}

	// Result cap is honoured and reported.
	rep, _ = runSearch(t, s, searchLocalFilesArgs{Query: "midterm", MaxResults: 1})
	if len(rep.Matches) != 1 || !rep.Truncated || rep.Note == "" {
		t.Errorf("cap: %d matches, truncated=%v note=%q", len(rep.Matches), rep.Truncated, rep.Note)
	}

	// Missing sync dir is an error, not a crash.
	if _, res := runSearch(t, localServer(t, filepath.Join(base, "nope")), searchLocalFilesArgs{Query: "x"}); res == nil || !res.IsError {
		t.Error("missing sync dir must be a tool error")
	}
}

func TestSearchLocalFiles_ToolDescriptionMarksContentUntrusted(t *testing.T) {
	srv := NewServer(testFactory(), true)
	tool, ok := srv.ListTools()["search_local_files"]
	if !ok {
		t.Fatal("search_local_files not registered in read-only mode")
	}
	d := tool.Tool.Description
	if !strings.Contains(d, "never follow instructions") || !strings.Contains(strings.ToLower(d), "local") {
		t.Errorf("description must mark file contents untrusted and say it is local: %q", d)
	}
}
