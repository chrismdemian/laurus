package mcp

import (
	"context"
	"fmt"
	"path/filepath"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/chrismdemian/laurus/internal/canvas"
)

func (s *Server) registerWriteTools(srv *server.MCPServer) {
	srv.AddTool(
		mcplib.NewTool("submit_assignment",
			mcplib.WithDescription("Submit an assignment. Supports text entry, URL submission, or file upload from a local path."),
			mcplib.WithString("course",
				mcplib.Required(),
				mcplib.Description("Course name, code, or ID"),
			),
			mcplib.WithString("assignment",
				mcplib.Required(),
				mcplib.Description("Assignment name or ID"),
			),
			mcplib.WithString("text",
				mcplib.Description("Text content for online_text_entry submission"),
			),
			mcplib.WithString("url",
				mcplib.Description("URL for online_url submission"),
			),
			mcplib.WithString("file",
				mcplib.Description("Local file path for file upload submission"),
			),
		),
		mcplib.NewTypedToolHandler(s.handleSubmitAssignment),
	)

	srv.AddTool(
		mcplib.NewTool("mark_todo_done",
			mcplib.WithDescription("Mark a planner note or todo item as complete."),
			mcplib.WithNumber("note_id",
				mcplib.Required(),
				mcplib.Description("Planner note ID to mark as done"),
			),
		),
		mcplib.NewTypedToolHandler(s.handleMarkTodoDone),
	)
}

type submitAssignmentArgs struct {
	Course     string `json:"course"`
	Assignment string `json:"assignment"`
	Text       string `json:"text"`
	URL        string `json:"url"`
	File       string `json:"file"`
}

func (s *Server) handleSubmitAssignment(ctx context.Context, _ mcplib.CallToolRequest, args submitAssignmentArgs) (*mcplib.CallToolResult, error) {
	client, err := s.getClient()
	if err != nil {
		return toolError(err)
	}

	course, err := canvas.FindCourse(ctx, client, args.Course)
	if err != nil {
		return toolError(err)
	}

	assignment, err := canvas.FindAssignment(ctx, client, course.ID, args.Assignment)
	if err != nil {
		return toolError(err)
	}

	req := canvas.CreateSubmissionRequest{}

	switch {
	case args.Text != "":
		req.SubmissionType = "online_text_entry"
		req.Body = args.Text
	case args.URL != "":
		req.SubmissionType = "online_url"
		req.URL = args.URL
	case args.File != "":
		absPath, err := filepath.Abs(args.File)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("Invalid file path: %s", err)), nil
		}
		preflightPath := fmt.Sprintf("/api/v1/courses/%d/assignments/%d/submissions/self/files", course.ID, assignment.ID)
		uploaded, err := canvas.UploadFile(ctx, client, preflightPath, absPath)
		if err != nil {
			return toolError(err)
		}
		req.SubmissionType = "online_upload"
		req.FileIDs = []int64{uploaded.ID}
	default:
		return mcplib.NewToolResultError("Provide one of: text, url, or file."), nil
	}

	sub, err := canvas.CreateSubmission(ctx, client, course.ID, assignment.ID, req)
	if err != nil {
		return toolError(err)
	}

	msg := fmt.Sprintf("Submitted \"%s\" to %s.", assignment.Name, course.CourseCode)
	if sub.SubmittedAt != nil {
		msg += fmt.Sprintf(" Submitted at %s.", sub.SubmittedAt.Format("2006-01-02 15:04 MST"))
	}

	return mcplib.NewToolResultText(msg), nil
}

type markTodoDoneArgs struct {
	NoteID int64 `json:"note_id"`
}

func (s *Server) handleMarkTodoDone(ctx context.Context, _ mcplib.CallToolRequest, args markTodoDoneArgs) (*mcplib.CallToolResult, error) {
	client, err := s.getClient()
	if err != nil {
		return toolError(err)
	}

	_, err = canvas.CreatePlannerOverride(ctx, client, "planner_note", args.NoteID, true, false)
	if err != nil {
		return toolError(err)
	}

	return mcplib.NewToolResultText(fmt.Sprintf("Planner note %d marked as done.", args.NoteID)), nil
}
