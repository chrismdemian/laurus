// Package mcp provides the MCP (Model Context Protocol) server for Canvas LMS,
// allowing AI assistants to interact with courses, assignments, grades, and more.
package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"sync"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"golang.org/x/sync/singleflight"

	"github.com/chrismdemian/laurus/internal/cache"
	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/internal/config"
	"github.com/chrismdemian/laurus/internal/render"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// Server holds Canvas dependencies for MCP tool handlers.
type Server struct {
	newClient func() (*canvas.Client, error)
	newCache  func() (*cache.DB, error)
	config    func() (*config.Config, error)
	version   string

	// One client per process so a single rate limiter governs every tool
	// call; memoised on success only, so an auth failure before setup is
	// retried next call.
	clientMu sync.Mutex
	client   *canvas.Client

	cacheMu sync.Mutex
	cache   *cache.DB

	flight   singleflight.Group   // one refresh per (resource, course) at a time
	failMu   sync.Mutex           // guards failures
	failures map[string]time.Time // last failed refresh per resource:course
}

const (
	instructionsBase = "Canvas LMS tools for reading courses, assignments, grades, discussions, and more. Course parameters accept names, course codes, or numeric IDs (e.g. \"CSC108\", \"csc108\", or \"12345\"). Every read returns an envelope {as_of, stale, source, data}: source is \"cache\" (served from the local sync cache, refreshed automatically when older than the tool's freshness tier) or \"live\" (fetched from Canvas just now); as_of is when the data was fetched from Canvas; stale=true means it is older than its tier or the last refresh did not complete (sync_error says why). Pass fresh=true on cache-served tools to force a refresh. Grades, inbox, todo, calendar, search and anything time-critical are always live."

	instructionsReadOnly = instructionsBase + " This server is running in READ-ONLY mode: no tool can submit, post, send, book, or modify anything in Canvas. If asked to perform such an action, explain that it is not available here."
)

// NewServer creates a configured MCP server with Canvas tools registered.
//
// When readOnly is true, the write tools (see registerWriteTools) are never
// registered, so the connected model cannot see or call them.
func NewServer(f *cmdutil.Factory, readOnly bool) *server.MCPServer {
	s := &Server{
		newClient: f.Client,
		newCache:  f.Cache,
		config:    f.Config,
		version:   f.Version,
	}

	instructions := instructionsBase
	if readOnly {
		instructions = instructionsReadOnly
	}

	srv := server.NewMCPServer(
		"laurus",
		f.Version,
		server.WithToolCapabilities(false),
		server.WithRecovery(),
		server.WithInstructions(instructions),
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
	s.registerSyncTools(srv)
	if !readOnly {
		s.registerWriteTools(srv)
	}

	return srv
}

// getClient returns the process-wide Canvas client, creating it on first
// success.
func (s *Server) getClient() (*canvas.Client, error) {
	s.clientMu.Lock()
	defer s.clientMu.Unlock()
	if s.client != nil {
		return s.client, nil
	}
	if s.newClient == nil {
		return nil, errors.New("no Canvas client configured")
	}
	c, err := s.newClient()
	if err != nil {
		return nil, err
	}
	s.client = c
	return c, nil
}

// getCache returns the process-wide cache handle, or an error when the
// factory provides none (reads then fall back to live).
func (s *Server) getCache() (*cache.DB, error) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if s.cache != nil {
		return s.cache, nil
	}
	if s.newCache == nil {
		return nil, errors.New("cache not configured")
	}
	db, err := s.newCache()
	if err != nil {
		return nil, err
	}
	s.cache = db
	return db, nil
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
