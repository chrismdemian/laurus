package mcp

import (
	"context"
	"fmt"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/chrismdemian/laurus/internal/canvas"
)

func (s *Server) registerAnnouncementTools(srv *server.MCPServer) {
	srv.AddTool(
		mcplib.NewTool("list_announcements",
			mcplib.WithDescription("List recent announcements. Optionally filter by course."),
			mcplib.WithString("course",
				mcplib.Description("Course name, code, or ID to filter by (optional — lists all courses if omitted)"),
			),
		),
		mcplib.NewTypedToolHandler(s.handleListAnnouncements),
	)

	srv.AddTool(
		mcplib.NewTool("get_announcement",
			mcplib.WithDescription("Get the full content of a specific announcement."),
			mcplib.WithString("course",
				mcplib.Required(),
				mcplib.Description("Course name, code, or ID"),
			),
			mcplib.WithNumber("announcement_id",
				mcplib.Required(),
				mcplib.Description("Announcement ID"),
			),
		),
		mcplib.NewTypedToolHandler(s.handleGetAnnouncement),
	)
}

type listAnnouncementsArgs struct {
	Course string `json:"course"`
}

func (s *Server) handleListAnnouncements(ctx context.Context, _ mcplib.CallToolRequest, args listAnnouncementsArgs) (*mcplib.CallToolResult, error) {
	client, err := s.getClient()
	if err != nil {
		return toolError(err)
	}

	var contextCodes []string
	if args.Course != "" {
		course, err := canvas.FindCourse(ctx, client, args.Course)
		if err != nil {
			return toolError(err)
		}
		contextCodes = []string{fmt.Sprintf("course_%d", course.ID)}
	} else {
		courses, err := collectIter(canvas.ListCourses(ctx, client, canvas.CourseListOptions{
			EnrollmentState: "active",
		}))
		if err != nil {
			return toolError(err)
		}
		for _, c := range courses {
			contextCodes = append(contextCodes, fmt.Sprintf("course_%d", c.ID))
		}
	}

	if len(contextCodes) == 0 {
		return mcplib.NewToolResultText("No enrolled courses found."), nil
	}

	// Use start_date=2000-01-01 to avoid Canvas's 14-day default (known gotcha)
	announcements, err := collectIter(canvas.ListAnnouncements(ctx, client, canvas.ListAnnouncementsOptions{
		ContextCodes: contextCodes,
		StartDate:    "2000-01-01",
	}))
	if err != nil {
		return toolError(err)
	}

	type announcementSummary struct {
		ID          int64      `json:"id"`
		Title       string     `json:"title"`
		Author      string     `json:"author"`
		PostedAt    *time.Time `json:"posted_at,omitempty"`
		ContextCode string     `json:"context_code"`
		ReadState   string     `json:"read_state"`
		HTMLURL     string     `json:"html_url"`
	}

	results := make([]announcementSummary, 0, len(announcements))
	for _, a := range announcements {
		results = append(results, announcementSummary{
			ID:          a.ID,
			Title:       a.Title,
			Author:      a.Author.Name,
			PostedAt:    a.PostedAt,
			ContextCode: a.ContextCode,
			ReadState:   a.ReadState,
			HTMLURL:     a.HTMLURL,
		})
	}

	if len(results) == 0 {
		return mcplib.NewToolResultText("No announcements found."), nil
	}

	return jsonResult(results)
}

type getAnnouncementArgs struct {
	Course         string `json:"course"`
	AnnouncementID int64  `json:"announcement_id"`
}

func (s *Server) handleGetAnnouncement(ctx context.Context, _ mcplib.CallToolRequest, args getAnnouncementArgs) (*mcplib.CallToolResult, error) {
	client, err := s.getClient()
	if err != nil {
		return toolError(err)
	}

	course, err := canvas.FindCourse(ctx, client, args.Course)
	if err != nil {
		return toolError(err)
	}

	// Fetch announcements for this course and find the one with matching ID
	announcements, err := collectIter(canvas.ListAnnouncements(ctx, client, canvas.ListAnnouncementsOptions{
		ContextCodes: []string{fmt.Sprintf("course_%d", course.ID)},
		StartDate:    "2000-01-01",
	}))
	if err != nil {
		return toolError(err)
	}

	for _, a := range announcements {
		if a.ID == args.AnnouncementID {
			type announcementDetail struct {
				ID       int64      `json:"id"`
				Title    string     `json:"title"`
				Author   string     `json:"author"`
				PostedAt *time.Time `json:"posted_at,omitempty"`
				Body     string     `json:"body"`
				HTMLURL  string     `json:"html_url"`
			}
			return jsonResult(announcementDetail{
				ID:       a.ID,
				Title:    a.Title,
				Author:   a.Author.Name,
				PostedAt: a.PostedAt,
				Body:     htmlToMarkdown(a.Message),
				HTMLURL:  a.HTMLURL,
			})
		}
	}

	return mcplib.NewToolResultError(fmt.Sprintf("Announcement %d not found in %s.", args.AnnouncementID, course.CourseCode)), nil
}
