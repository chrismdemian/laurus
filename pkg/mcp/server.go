// Package mcp provides the MCP (Model Context Protocol) server for Canvas LMS,
// allowing AI assistants to interact with courses, assignments, grades, and more.
package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"iter"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/internal/render"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// Server holds Canvas dependencies for MCP tool handlers.
type Server struct {
	client  func() (*canvas.Client, error)
	version string
}

// NewServer creates a configured MCP server with all Canvas tools registered.
func NewServer(f *cmdutil.Factory) *server.MCPServer {
	s := &Server{
		client:  f.Client,
		version: f.Version,
	}

	srv := server.NewMCPServer(
		"laurus",
		f.Version,
		server.WithToolCapabilities(false),
		server.WithRecovery(),
		server.WithInstructions("Canvas LMS tools for reading courses, assignments, grades, discussions, and more. Course parameters accept names, course codes, or numeric IDs (e.g. \"CSC108\", \"csc108\", or \"12345\")."),
	)

	s.registerCourseTools(srv)
	s.registerAssignmentTools(srv)
	s.registerGradeTools(srv)
	s.registerAnnouncementTools(srv)
	s.registerDiscussionTools(srv)
	s.registerInboxTools(srv)
	s.registerModuleTools(srv)
	s.registerFileTools(srv)
	s.registerPageTools(srv)
	s.registerCalendarTools(srv)
	s.registerWriteTools(srv)

	return srv
}

// getClient returns the Canvas client or an MCP error result if auth fails.
func (s *Server) getClient() (*canvas.Client, error) {
	return s.client()
}

// collectIter drains a paginated iterator into a slice.
func collectIter[T any](seq iter.Seq2[T, error]) ([]T, error) {
	var items []T
	for item, err := range seq {
		if err != nil {
			return items, err
		}
		items = append(items, item)
	}
	return items, nil
}

// toolError translates a canvas/Go error into an MCP tool error result.
func toolError(err error) (*mcplib.CallToolResult, error) {
	switch {
	case errors.Is(err, canvas.ErrTokenInvalid):
		return mcplib.NewToolResultError("Authentication failed. Run 'laurus auth login' to re-authenticate."), nil
	case errors.Is(err, canvas.ErrNotFound):
		return mcplib.NewToolResultError(fmt.Sprintf("Not found: %s", unwrapMessage(err))), nil
	case errors.Is(err, canvas.ErrForbidden), errors.Is(err, canvas.ErrPermissionDenied):
		return mcplib.NewToolResultError(fmt.Sprintf("Permission denied: %s", unwrapMessage(err))), nil
	case errors.Is(err, canvas.ErrRateLimited):
		return mcplib.NewToolResultError("Canvas rate limit reached. Try again in a moment."), nil
	default:
		var validErr *canvas.ErrValidation
		if errors.As(err, &validErr) {
			return mcplib.NewToolResultError(fmt.Sprintf("Validation error: %s", validErr.Error())), nil
		}
		return mcplib.NewToolResultError(fmt.Sprintf("Error: %s", err.Error())), nil
	}
}

// unwrapMessage extracts a human-readable message from a canvas error.
func unwrapMessage(err error) string {
	var apiErr *canvas.APIError
	if errors.As(err, &apiErr) && apiErr.Message != "" {
		return apiErr.Message
	}
	return err.Error()
}

// jsonResult marshals v to JSON and returns it as an MCP text result.
func jsonResult(v any) (*mcplib.CallToolResult, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("Failed to format result: %s", err)), nil
	}
	return mcplib.NewToolResultText(string(data)), nil
}

// htmlToMarkdown converts Canvas HTML to plain markdown for LLM consumption.
func htmlToMarkdown(html string) string {
	md, err := render.CanvasHTMLToMarkdown(html)
	if err != nil || md == "" {
		return html
	}
	return md
}
