package mcp

import (
	"context"
	"fmt"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/chrismdemian/laurus/internal/canvas"
)

func (s *Server) registerCalendarTools(srv *server.MCPServer) {
	srv.AddTool(
		mcplib.NewTool("list_calendar",
			mcplib.WithDescription("List calendar events and assignment deadlines. Defaults to upcoming events; use start_date/end_date for a custom range."),
			mcplib.WithString("start_date",
				mcplib.Description("Start date (YYYY-MM-DD). If omitted, shows upcoming events."),
			),
			mcplib.WithString("end_date",
				mcplib.Description("End date (YYYY-MM-DD). If omitted, shows upcoming events."),
			),
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

type listCalendarArgs struct {
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
}

func (s *Server) handleListCalendar(ctx context.Context, _ mcplib.CallToolRequest, args listCalendarArgs) (*mcplib.CallToolResult, error) {
	client, err := s.getClient()
	if err != nil {
		return toolError(err)
	}

	// If no date range specified, use upcoming events (fast path).
	if args.StartDate == "" && args.EndDate == "" {
		events, err := canvas.ListUpcomingEvents(ctx, client)
		if err != nil {
			return toolError(err)
		}

		if len(events) == 0 {
			return mcplib.NewToolResultText("No upcoming events or deadlines."), nil
		}

		return jsonResult(events)
	}

	// Date-range query using /calendar_events.
	var contextCodes []string
	courses, err := collectIter(canvas.ListCourses(ctx, client, canvas.CourseListOptions{
		EnrollmentState: "active",
	}))
	if err != nil {
		return toolError(err)
	}
	for _, c := range courses {
		contextCodes = append(contextCodes, fmt.Sprintf("course_%d", c.ID))
	}

	var events []canvas.CalendarEvent
	for _, eventType := range []string{"event", "assignment"} {
		for ev, err := range canvas.ListCalendarEvents(ctx, client, canvas.ListCalendarEventsOptions{
			Type:         eventType,
			StartDate:    args.StartDate,
			EndDate:      args.EndDate,
			ContextCodes: contextCodes,
		}) {
			if err != nil {
				return toolError(err)
			}
			events = append(events, ev)
		}
	}

	if len(events) == 0 {
		return mcplib.NewToolResultText("No events in the specified date range."), nil
	}

	return jsonResult(events)
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

	results, err := canvas.SearchCourseWithSmartFallback(ctx, client, course.ID, args.Query)
	if err != nil {
		return toolError(err)
	}

	if len(results) == 0 {
		return mcplib.NewToolResultText("No results found."), nil
	}

	return jsonResult(results)
}
