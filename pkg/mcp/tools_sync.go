package mcp

import (
	"context"
	"fmt"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/chrismdemian/laurus/internal/cache"
	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/internal/syncer"
)

// registerSyncTools adds laurus_sync. It only reads Canvas and writes the
// local cache, so it is available in read-only mode too.
func (s *Server) registerSyncTools(srv *server.MCPServer) {
	srv.AddTool(
		mcplib.NewTool("laurus_sync",
			mcplib.WithDescription("Refresh the local cache from Canvas now (all active courses, or one course). Reads Canvas and writes only the local cache; never changes anything in Canvas. Returns status success|partial|failed, what was synced, and every error, including endpoints that were truncated (kept as the previous set, marked suspect) or refused. Use when the user says something changed, or when a read came back stale with a sync_error."),
			mcplib.WithString("course",
				mcplib.Description("Course name, code, or ID to sync; omit for every active course"),
			),
		),
		mcplib.NewTypedToolHandler(s.handleSync),
	)
}

type syncArgs struct {
	Course string `json:"course"`
}

type syncReport struct {
	Status   string      `json:"status"` // success, partial, failed
	AsOf     time.Time   `json:"as_of"`
	Elapsed  string      `json:"elapsed"`
	Courses  int         `json:"courses"`
	Items    int         `json:"items"`
	Synced   []syncEntry `json:"synced"`
	Errors   []string    `json:"errors"`
	Warnings []string    `json:"warnings,omitempty"`
}

type syncEntry struct {
	Course string `json:"course"`
	Job    string `json:"job"`
	Status string `json:"status"`
	Items  int    `json:"items"`
}

func (s *Server) handleSync(ctx context.Context, _ mcplib.CallToolRequest, args syncArgs) (*mcplib.CallToolResult, error) {
	client, err := s.getClient()
	if err != nil {
		return toolError(err)
	}
	db, err := s.getCache()
	if err != nil {
		return mcplib.NewToolResultError("cache unavailable: " + err.Error()), nil
	}
	start := time.Now()

	courses, res := syncer.SyncCourses(ctx, client, db)
	if res.Err != nil {
		return jsonResult(syncReport{
			Status: "failed", AsOf: time.Now().UTC(), Elapsed: time.Since(start).Round(100 * time.Millisecond).String(),
			Errors: []string{"courses: " + res.Err.Error()}, Synced: []syncEntry{}, Warnings: nil,
		})
	}
	report := syncReport{AsOf: time.Now().UTC(), Synced: []syncEntry{{Course: "all", Job: string(syncer.JobCourses), Status: res.Status, Items: res.Count}}, Errors: []string{}}

	courses = syncer.ActiveCourses(courses)
	if args.Course != "" {
		c, ok := matchCourse(courses, args.Course)
		if !ok {
			found, err := canvas.FindCourse(ctx, client, args.Course)
			if err != nil {
				return toolError(err)
			}
			c = found
		}
		courses = []canvas.Course{c}
	}

	sum := syncer.SyncAll(ctx, client, db, courses, syncer.Options{})
	report.Courses = len(courses)
	report.Items = sum.Items() + res.Count
	report.Elapsed = sum.Elapsed.Round(100 * time.Millisecond).String()
	var failed int
	for _, r := range sum.Results {
		report.Synced = append(report.Synced, syncEntry{Course: r.CourseCode, Job: string(r.Job), Status: r.Status, Items: r.Count})
		switch {
		case r.Status == cache.StatusFailed:
			failed++
			report.Errors = append(report.Errors, fmt.Sprintf("%s/%s: %v", r.CourseCode, r.Job, r.Err))
		case r.Err != nil: // suspect (truncated): stored, previous set kept
			report.Warnings = append(report.Warnings, fmt.Sprintf("%s/%s: %v", r.CourseCode, r.Job, r.Err))
		}
	}
	switch {
	case failed == 0 && len(report.Warnings) == 0:
		report.Status = "success"
	case failed == len(sum.Results) && failed > 0:
		report.Status = "failed"
	default:
		report.Status = "partial"
	}
	return jsonResult(report)
}
