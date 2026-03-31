package mcp

import (
	"context"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/chrismdemian/laurus/internal/canvas"
)

func (s *Server) registerModuleTools(srv *server.MCPServer) {
	srv.AddTool(
		mcplib.NewTool("list_modules",
			mcplib.WithDescription("List course modules with their items (assignments, pages, files, etc)."),
			mcplib.WithString("course",
				mcplib.Required(),
				mcplib.Description("Course name, code, or ID"),
			),
		),
		mcplib.NewTypedToolHandler(s.handleListModules),
	)
}

type listModulesArgs struct {
	Course string `json:"course"`
}

func (s *Server) handleListModules(ctx context.Context, _ mcplib.CallToolRequest, args listModulesArgs) (*mcplib.CallToolResult, error) {
	client, err := s.getClient()
	if err != nil {
		return toolError(err)
	}

	course, err := canvas.FindCourse(ctx, client, args.Course)
	if err != nil {
		return toolError(err)
	}

	modules, err := collectIter(canvas.ListModules(ctx, client, course.ID, canvas.ListModulesOptions{
		IncludeItems:          true,
		IncludeContentDetails: true,
	}))
	if err != nil {
		return toolError(err)
	}

	type moduleItemOut struct {
		ID       int64      `json:"id"`
		Title    string     `json:"title"`
		Type     string     `json:"type"`
		DueAt    *time.Time `json:"due_at,omitempty"`
		HTMLURL  string     `json:"html_url"`
		Complete bool       `json:"complete,omitempty"`
	}

	type moduleOut struct {
		ID    int64           `json:"id"`
		Name  string          `json:"name"`
		Items []moduleItemOut `json:"items"`
	}

	results := make([]moduleOut, 0, len(modules))
	for _, m := range modules {
		mo := moduleOut{
			ID:   m.ID,
			Name: m.Name,
		}
		for _, item := range m.Items {
			mi := moduleItemOut{
				ID:      item.ID,
				Title:   item.Title,
				Type:    item.Type,
				HTMLURL: item.HTMLURL,
			}
			if item.ContentDetails != nil {
				mi.DueAt = item.ContentDetails.DueAt
			}
			if item.CompletionRequirement != nil {
				mi.Complete = item.CompletionRequirement.Completed
			}
			mo.Items = append(mo.Items, mi)
		}
		results = append(results, mo)
	}

	if len(results) == 0 {
		return mcplib.NewToolResultText("No modules found."), nil
	}

	return jsonResult(results)
}
