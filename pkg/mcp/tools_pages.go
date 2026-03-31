package mcp

import (
	"context"
	"fmt"
	"strings"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/chrismdemian/laurus/internal/canvas"
)

func (s *Server) registerPageTools(srv *server.MCPServer) {
	srv.AddTool(
		mcplib.NewTool("list_pages",
			mcplib.WithDescription("List wiki pages for a course."),
			mcplib.WithString("course",
				mcplib.Required(),
				mcplib.Description("Course name, code, or ID"),
			),
		),
		mcplib.NewTypedToolHandler(s.handleListPages),
	)

	srv.AddTool(
		mcplib.NewTool("get_page",
			mcplib.WithDescription("Get the full content of a wiki page."),
			mcplib.WithString("course",
				mcplib.Required(),
				mcplib.Description("Course name, code, or ID"),
			),
			mcplib.WithString("page",
				mcplib.Required(),
				mcplib.Description("Page title, URL slug, or ID"),
			),
		),
		mcplib.NewTypedToolHandler(s.handleGetPage),
	)
}

type listPagesArgs struct {
	Course string `json:"course"`
}

func (s *Server) handleListPages(ctx context.Context, _ mcplib.CallToolRequest, args listPagesArgs) (*mcplib.CallToolResult, error) {
	client, err := s.getClient()
	if err != nil {
		return toolError(err)
	}

	course, err := canvas.FindCourse(ctx, client, args.Course)
	if err != nil {
		return toolError(err)
	}

	pages, err := collectIter(canvas.ListPages(ctx, client, course.ID, canvas.ListPagesOptions{}))
	if err != nil {
		return toolError(err)
	}

	type pageSummary struct {
		PageID  int64  `json:"page_id"`
		Title   string `json:"title"`
		URL     string `json:"url"`
		HTMLURL string `json:"html_url,omitempty"`
	}

	results := make([]pageSummary, 0, len(pages))
	for _, p := range pages {
		results = append(results, pageSummary{
			PageID:  p.PageID,
			Title:   p.Title,
			URL:     p.URL,
			HTMLURL: p.HTMLURL,
		})
	}

	if len(results) == 0 {
		return mcplib.NewToolResultText(fmt.Sprintf("No pages found in %s.", course.CourseCode)), nil
	}

	return jsonResult(results)
}

type getPageArgs struct {
	Course string `json:"course"`
	Page   string `json:"page"`
}

func (s *Server) handleGetPage(ctx context.Context, _ mcplib.CallToolRequest, args getPageArgs) (*mcplib.CallToolResult, error) {
	client, err := s.getClient()
	if err != nil {
		return toolError(err)
	}

	course, err := canvas.FindCourse(ctx, client, args.Course)
	if err != nil {
		return toolError(err)
	}

	page, err := canvas.FindPage(ctx, client, course.ID, args.Page)
	if err != nil {
		return toolError(err)
	}

	type pageDetail struct {
		PageID  int64  `json:"page_id"`
		Title   string `json:"title"`
		URL     string `json:"url"`
		HTMLURL string `json:"html_url,omitempty"`
		Body    string `json:"body"`
	}

	body := ""
	if page.Body != nil && strings.TrimSpace(*page.Body) != "" {
		body = htmlToMarkdown(*page.Body)
	}

	return jsonResult(pageDetail{
		PageID:  page.PageID,
		Title:   page.Title,
		URL:     page.URL,
		HTMLURL: page.HTMLURL,
		Body:    body,
	})
}
