package mcp

import (
	"context"
	"fmt"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/chrismdemian/laurus/internal/canvas"
)

func (s *Server) registerInboxTools(srv *server.MCPServer) {
	srv.AddTool(
		mcplib.NewTool("list_inbox",
			mcplib.WithDescription("List Canvas inbox conversations."),
			mcplib.WithString("scope",
				mcplib.Description("Filter by scope"),
				mcplib.Enum("inbox", "unread", "starred", "sent", "archived"),
			),
		),
		mcplib.NewTypedToolHandler(s.handleListInbox),
	)

	srv.AddTool(
		mcplib.NewTool("get_unread_count",
			mcplib.WithDescription("Get the number of unread Canvas inbox messages."),
		),
		mcplib.NewTypedToolHandler(s.handleGetUnreadCount),
	)

	srv.AddTool(
		mcplib.NewTool("send_message",
			mcplib.WithDescription("Send a new Canvas inbox message to a recipient."),
			mcplib.WithString("recipient",
				mcplib.Required(),
				mcplib.Description("Recipient name to search for"),
			),
			mcplib.WithString("subject",
				mcplib.Required(),
				mcplib.Description("Message subject line"),
			),
			mcplib.WithString("body",
				mcplib.Required(),
				mcplib.Description("Message body"),
			),
			mcplib.WithString("course",
				mcplib.Description("Course context for recipient search (optional, improves match accuracy)"),
			),
		),
		mcplib.NewTypedToolHandler(s.handleSendMessage),
	)
}

type listInboxArgs struct {
	Scope string `json:"scope"`
}

func (s *Server) handleListInbox(ctx context.Context, _ mcplib.CallToolRequest, args listInboxArgs) (*mcplib.CallToolResult, error) {
	client, err := s.getClient()
	if err != nil {
		return toolError(err)
	}

	opts := canvas.ListConversationsOptions{
		Scope: args.Scope,
	}
	conversations, err := collectIter(canvas.ListConversations(ctx, client, opts))
	if err != nil {
		return toolError(err)
	}

	type conversationSummary struct {
		ID           int64     `json:"id"`
		Subject      string    `json:"subject"`
		LastMessage  string    `json:"last_message"`
		LastAt       time.Time `json:"last_message_at"`
		MessageCount int       `json:"message_count"`
		Participants []string  `json:"participants"`
		State        string    `json:"state"`
		Starred      bool      `json:"starred,omitempty"`
	}

	results := make([]conversationSummary, 0, len(conversations))
	for _, c := range conversations {
		cs := conversationSummary{
			ID:           c.ID,
			Subject:      c.Subject,
			LastMessage:  c.LastMessage,
			LastAt:       c.LastMessageAt,
			MessageCount: c.MessageCount,
			State:        c.WorkflowState,
			Starred:      c.Starred,
		}
		for _, p := range c.Participants {
			cs.Participants = append(cs.Participants, p.Name)
		}
		results = append(results, cs)
	}

	if len(results) == 0 {
		return mcplib.NewToolResultText("No conversations found."), nil
	}

	return jsonResult(results)
}

type getUnreadCountArgs struct{}

func (s *Server) handleGetUnreadCount(ctx context.Context, _ mcplib.CallToolRequest, _ getUnreadCountArgs) (*mcplib.CallToolResult, error) {
	client, err := s.getClient()
	if err != nil {
		return toolError(err)
	}

	count, err := canvas.GetUnreadCount(ctx, client)
	if err != nil {
		return toolError(err)
	}

	return mcplib.NewToolResultText(fmt.Sprintf("You have %d unread message(s).", count)), nil
}

type sendMessageArgs struct {
	Recipient string `json:"recipient"`
	Subject   string `json:"subject"`
	Body      string `json:"body"`
	Course    string `json:"course"`
}

func (s *Server) handleSendMessage(ctx context.Context, _ mcplib.CallToolRequest, args sendMessageArgs) (*mcplib.CallToolResult, error) {
	client, err := s.getClient()
	if err != nil {
		return toolError(err)
	}

	var contextCode string
	if args.Course != "" {
		course, err := canvas.FindCourse(ctx, client, args.Course)
		if err != nil {
			return toolError(err)
		}
		contextCode = fmt.Sprintf("course_%d", course.ID)
	}

	recipient, err := canvas.FindRecipient(ctx, client, args.Recipient, contextCode)
	if err != nil {
		return toolError(err)
	}

	req := canvas.CreateConversationRequest{
		Recipients:  []string{recipient.ID},
		Subject:     args.Subject,
		Body:        args.Body,
		ContextCode: contextCode,
	}

	conversations, err := canvas.CreateConversation(ctx, client, req)
	if err != nil {
		return toolError(err)
	}

	if len(conversations) > 0 {
		return mcplib.NewToolResultText(fmt.Sprintf("Message sent to %s (conversation ID: %d).", recipient.Name, conversations[0].ID)), nil
	}

	return mcplib.NewToolResultText(fmt.Sprintf("Message sent to %s.", recipient.Name)), nil
}
