package mcp

import (
	"context"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/chrismdemian/laurus/internal/canvas"
)

func (s *Server) registerCalendarTools(srv *server.MCPServer) {
	srv.AddTool(
		mcplib.NewTool("list_calendar",
			mcplib.WithDescription("List upcoming calendar events and assignment deadlines."),
		),
		mcplib.NewTypedToolHandler(s.handleListCalendar),
	)

	srv.AddTool(
		mcplib.NewTool("get_todo",
			mcplib.WithDescription("List items on the Canvas planner/todo list."),
		),
		mcplib.NewTypedToolHandler(s.handleGetTodo),
	)

	srv.AddTool(
		mcplib.NewTool("search_course",
			mcplib.WithDescription("Search for content within a course by keyword. Searches assignments, pages, discussions, and announcements."),
			mcplib.WithString("course",
				mcplib.Required(),
				mcplib.Description("Course name, code, or ID"),
			),
			mcplib.WithString("query",
				mcplib.Required(),
				mcplib.Description("Search keyword"),
			),
		),
		mcplib.NewTypedToolHandler(s.handleSearchCourse),
	)
}

type listCalendarArgs struct{}

func (s *Server) handleListCalendar(ctx context.Context, _ mcplib.CallToolRequest, _ listCalendarArgs) (*mcplib.CallToolResult, error) {
	client, err := s.getClient()
	if err != nil {
		return toolError(err)
	}

	events, err := canvas.ListUpcomingEvents(ctx, client)
	if err != nil {
		return toolError(err)
	}

	type calendarEvent struct {
		Title   string     `json:"title"`
		Type    string     `json:"type"`
		StartAt *time.Time `json:"start_at,omitempty"`
		EndAt   *time.Time `json:"end_at,omitempty"`
		HTMLURL string     `json:"html_url"`
	}

	results := make([]calendarEvent, 0, len(events))
	for _, e := range events {
		results = append(results, calendarEvent{
			Title:   e.Title,
			Type:    e.Type,
			StartAt: e.StartAt,
			EndAt:   e.EndAt,
			HTMLURL: e.HTMLURL,
		})
	}

	if len(results) == 0 {
		return mcplib.NewToolResultText("No upcoming events or deadlines."), nil
	}

	return jsonResult(results)
}

type getTodoArgs struct{}

func (s *Server) handleGetTodo(ctx context.Context, _ mcplib.CallToolRequest, _ getTodoArgs) (*mcplib.CallToolResult, error) {
	client, err := s.getClient()
	if err != nil {
		return toolError(err)
	}

	items, err := canvas.ListTodoItems(ctx, client)
	if err != nil {
		return toolError(err)
	}

	type todoItem struct {
		Type       string     `json:"type"`
		CourseName string     `json:"course_name,omitempty"`
		Name       string     `json:"name"`
		DueAt      *time.Time `json:"due_at,omitempty"`
		HTMLURL    string     `json:"html_url"`
	}

	results := make([]todoItem, 0, len(items))
	for _, t := range items {
		ti := todoItem{
			Type:       t.Type,
			CourseName: t.ContextName,
			HTMLURL:    t.HTMLURL,
		}
		if t.Assignment != nil {
			ti.Name = t.Assignment.Name
			ti.DueAt = t.Assignment.DueAt
		}
		results = append(results, ti)
	}

	if len(results) == 0 {
		return mcplib.NewToolResultText("No todo items."), nil
	}

	return jsonResult(results)
}

type searchCourseArgs struct {
	Course string `json:"course"`
	Query  string `json:"query"`
}

func (s *Server) handleSearchCourse(ctx context.Context, _ mcplib.CallToolRequest, args searchCourseArgs) (*mcplib.CallToolResult, error) {
	client, err := s.getClient()
	if err != nil {
		return toolError(err)
	}

	course, err := canvas.FindCourse(ctx, client, args.Course)
	if err != nil {
		return toolError(err)
	}

	type searchResult struct {
		Type    string `json:"type"`
		ID      int64  `json:"id,omitempty"`
		Title   string `json:"title"`
		HTMLURL string `json:"html_url,omitempty"`
	}

	var results []searchResult
	var lastErr error

	// Search assignments
	assignments, err := collectIter(canvas.ListAssignments(ctx, client, course.ID, canvas.ListAssignmentsOptions{
		SearchTerm: args.Query,
	}))
	if err == nil {
		for _, a := range assignments {
			results = append(results, searchResult{
				Type:    "assignment",
				ID:      a.ID,
				Title:   a.Name,
				HTMLURL: a.HTMLURL,
			})
		}
	} else {
		lastErr = err
	}

	// Search pages (may 404 if Pages tab is disabled — known gotcha)
	pages, err := collectIter(canvas.ListPages(ctx, client, course.ID, canvas.ListPagesOptions{
		SearchTerm: args.Query,
	}))
	if err == nil {
		for _, p := range pages {
			results = append(results, searchResult{
				Type:  "page",
				ID:    p.PageID,
				Title: p.Title,
			})
		}
	} else {
		lastErr = err
	}

	// Search discussions
	discussions, err := collectIter(canvas.ListDiscussionTopics(ctx, client, course.ID, canvas.ListDiscussionTopicsOptions{
		SearchTerm: args.Query,
	}))
	if err == nil {
		for _, d := range discussions {
			results = append(results, searchResult{
				Type:    "discussion",
				ID:      d.ID,
				Title:   d.Title,
				HTMLURL: d.HTMLURL,
			})
		}
	} else {
		lastErr = err
	}

	if len(results) == 0 {
		if lastErr != nil {
			return toolError(lastErr)
		}
		return mcplib.NewToolResultText("No results found."), nil
	}

	return jsonResult(results)
}
