package mcp

import (
	"context"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/chrismdemian/laurus/internal/cache"
	"github.com/chrismdemian/laurus/internal/canvas"
)

func (s *Server) registerFileTools(srv *server.MCPServer) {
	srv.AddTool(
		mcplib.NewTool("list_files",
			mcplib.WithDescription("List files in a course."),
			mcplib.WithString("course",
				mcplib.Required(),
				mcplib.Description("Course name, code, or ID"),
			),
			freshArg(),
		),
		mcplib.NewTypedToolHandler(s.handleListFiles),
	)

	srv.AddTool(
		mcplib.NewTool("get_file",
			mcplib.WithDescription("Get details and download URL for a specific file."),
			mcplib.WithString("course",
				mcplib.Required(),
				mcplib.Description("Course name, code, or ID"),
			),
			mcplib.WithString("file",
				mcplib.Required(),
				mcplib.Description("File name or ID"),
			),
		),
		mcplib.NewTypedToolHandler(s.handleGetFile),
	)
}

type listFilesArgs struct {
	Course string `json:"course"`
	Fresh  bool   `json:"fresh"`
}

// list_files is cache-first on the 1-hour tier for metadata only. Download
// URLs expire, so they are not served from cache: use get_file for one.
func (s *Server) handleListFiles(ctx context.Context, _ mcplib.CallToolRequest, args listFilesArgs) (*mcplib.CallToolResult, error) {
	course, err := s.findCourse(ctx, args.Course)
	if err != nil {
		return toolError(err)
	}

	var files []canvas.File
	env, err := s.read(ctx, readSpec{rt: cache.ResourceFiles, courseID: course.ID, ttl: tierFiles, fresh: args.Fresh}, &files, func() error {
		client, err := s.getClient()
		if err != nil {
			return err
		}
		got, err := collectIter(canvas.ListFiles(ctx, client, course.ID, canvas.ListFilesOptions{}))
		files = got
		return err
	})
	if err != nil {
		return toolError(err)
	}

	type fileSummary struct {
		ID          int64     `json:"id"`
		Name        string    `json:"name"`
		Size        int64     `json:"size"`
		ContentType string    `json:"content_type"`
		UpdatedAt   time.Time `json:"updated_at"`
	}

	results := make([]fileSummary, 0, len(files))
	for _, f := range files {
		results = append(results, fileSummary{
			ID:          f.ID,
			Name:        f.DisplayName,
			Size:        f.Size,
			ContentType: f.ContentType,
			UpdatedAt:   f.UpdatedAt,
		})
	}

	env.Note = "download URLs are not cached; call get_file for a fresh one"
	env.Data = results
	return jsonResult(env)
}

type getFileArgs struct {
	Course string `json:"course"`
	File   string `json:"file"`
}

func (s *Server) handleGetFile(ctx context.Context, _ mcplib.CallToolRequest, args getFileArgs) (*mcplib.CallToolResult, error) {
	client, err := s.getClient()
	if err != nil {
		return toolError(err)
	}

	course, err := s.findCourse(ctx, args.Course)
	if err != nil {
		return toolError(err)
	}

	file, err := canvas.FindFile(ctx, client, course.ID, args.File)
	if err != nil {
		return toolError(err)
	}

	type fileDetail struct {
		ID          int64     `json:"id"`
		Name        string    `json:"name"`
		Size        int64     `json:"size"`
		ContentType string    `json:"content_type"`
		UpdatedAt   time.Time `json:"updated_at"`
		URL         string    `json:"url"`
	}

	return liveResult(fileDetail{
		ID:          file.ID,
		Name:        file.DisplayName,
		Size:        file.Size,
		ContentType: file.ContentType,
		UpdatedAt:   file.UpdatedAt,
		URL:         file.URL,
	})
}
