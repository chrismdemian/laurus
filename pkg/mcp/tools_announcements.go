package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/chrismdemian/laurus/internal/cache"
	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/internal/syncer"
)

func (s *Server) registerAnnouncementTools(srv *server.MCPServer) {
	srv.AddTool(
		mcplib.NewTool("list_announcements",
			mcplib.WithDescription("List recent announcements. Optionally filter by course."),
			mcplib.WithString("course",
				mcplib.Description("Course name, code, or ID to filter by (optional — lists all courses if omitted)"),
			),
			freshArg(),
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
			freshArg(),
		),
		mcplib.NewTypedToolHandler(s.handleGetAnnouncement),
	)
}

type listAnnouncementsArgs struct {
	Course string `json:"course"`
	Fresh  bool   `json:"fresh"`
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

// readAnnouncements is the cache-first fetch for one course (30-minute tier).
func (s *Server) readAnnouncements(ctx context.Context, course canvas.Course, fresh bool) ([]canvas.Announcement, envelope, error) {
	var items []canvas.Announcement
	env, err := s.read(ctx, readSpec{rt: cache.ResourceAnnouncements, courseID: course.ID, ttl: tierActivity, fresh: fresh}, &items, func() error {
		client, err := s.getClient()
		if err != nil {
			return err
		}
		got, err := collectIter(canvas.ListAnnouncements(ctx, client, canvas.ListAnnouncementsOptions{
			ContextCodes: []string{fmt.Sprintf("course_%d", course.ID)},
			StartDate:    "2000-01-01", // avoid Canvas's 14-day default
		}))
		items = got
		return err
	})
	return items, env, err
}

func (s *Server) handleListAnnouncements(ctx context.Context, _ mcplib.CallToolRequest, args listAnnouncementsArgs) (*mcplib.CallToolResult, error) {
	var courses []canvas.Course
	if args.Course != "" {
		course, err := s.findCourse(ctx, args.Course)
		if err != nil {
			return toolError(err)
		}
		courses = []canvas.Course{course}
	} else {
		all, _, err := s.cachedCourses(ctx, tierGrade, args.Fresh)
		if err != nil {
			return toolError(err)
		}
		courses = syncer.ActiveCourses(all)
	}
	if len(courses) == 0 {
		return liveEmpty("No enrolled courses found.")
	}

	env := envelope{Source: sourceCache}
	var oldest time.Time
	var notes []string
	var results []announcementSummary
	for _, c := range courses {
		items, cenv, err := s.readAnnouncements(ctx, c, args.Fresh)
		if err != nil {
			if len(courses) == 1 {
				return toolError(err)
			}
			notes = append(notes, fmt.Sprintf("%s: %v", c.CourseCode, err))
			continue
		}
		if cenv.Stale {
			env.Stale = true
		}
		if cenv.SyncError != "" {
			notes = append(notes, fmt.Sprintf("%s: %s", c.CourseCode, cenv.SyncError))
		}
		if cenv.Source == sourceLive {
			env.Source = sourceLive
		}
		if oldest.IsZero() || cenv.AsOf.Before(oldest) {
			oldest = cenv.AsOf
		}
		for _, a := range items {
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
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].PostedAt == nil {
			return false
		}
		if results[j].PostedAt == nil {
			return true
		}
		return results[i].PostedAt.After(*results[j].PostedAt)
	})

	env.AsOf = oldest
	if oldest.IsZero() {
		env.AsOf = time.Now().UTC()
	}
	if len(notes) > 0 {
		env.Stale = true // a course is missing or its refresh failed
		env.SyncError = strings.Join(notes, "; ")
	}
	if results == nil {
		results = []announcementSummary{}
	}
	env.Data = results
	return jsonResult(env)
}

type getAnnouncementArgs struct {
	Course         string `json:"course"`
	AnnouncementID int64  `json:"announcement_id"`
	Fresh          bool   `json:"fresh"`
}

func (s *Server) handleGetAnnouncement(ctx context.Context, _ mcplib.CallToolRequest, args getAnnouncementArgs) (*mcplib.CallToolResult, error) {
	course, err := s.findCourse(ctx, args.Course)
	if err != nil {
		return toolError(err)
	}

	announcements, env, err := s.readAnnouncements(ctx, course, args.Fresh)
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
			env.Data = announcementDetail{
				ID:       a.ID,
				Title:    a.Title,
				Author:   a.Author.Name,
				PostedAt: a.PostedAt,
				Body:     htmlToMarkdown(a.Message),
				HTMLURL:  a.HTMLURL,
			}
			return jsonResult(env)
		}
	}

	return mcplib.NewToolResultError(fmt.Sprintf("Announcement %d not found in %s (cache as of %s; retry with fresh=true if it was just posted).", args.AnnouncementID, course.CourseCode, env.AsOf.Format(time.RFC3339))), nil
}
