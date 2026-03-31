package mcp

import (
	"context"
	"fmt"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/chrismdemian/laurus/internal/canvas"
)

func (s *Server) registerDiscussionTools(srv *server.MCPServer) {
	srv.AddTool(
		mcplib.NewTool("list_discussions",
			mcplib.WithDescription("List discussion topics for a course."),
			mcplib.WithString("course",
				mcplib.Required(),
				mcplib.Description("Course name, code, or ID"),
			),
		),
		mcplib.NewTypedToolHandler(s.handleListDiscussions),
	)

	srv.AddTool(
		mcplib.NewTool("get_discussion",
			mcplib.WithDescription("Get a full discussion thread with all replies."),
			mcplib.WithString("course",
				mcplib.Required(),
				mcplib.Description("Course name, code, or ID"),
			),
			mcplib.WithString("discussion",
				mcplib.Required(),
				mcplib.Description("Discussion topic name or ID"),
			),
		),
		mcplib.NewTypedToolHandler(s.handleGetDiscussion),
	)

	srv.AddTool(
		mcplib.NewTool("reply_to_discussion",
			mcplib.WithDescription("Post a reply to a discussion topic."),
			mcplib.WithString("course",
				mcplib.Required(),
				mcplib.Description("Course name, code, or ID"),
			),
			mcplib.WithString("discussion",
				mcplib.Required(),
				mcplib.Description("Discussion topic name or ID"),
			),
			mcplib.WithString("message",
				mcplib.Required(),
				mcplib.Description("Reply message content"),
			),
		),
		mcplib.NewTypedToolHandler(s.handleReplyToDiscussion),
	)
}

type listDiscussionsArgs struct {
	Course string `json:"course"`
}

func (s *Server) handleListDiscussions(ctx context.Context, _ mcplib.CallToolRequest, args listDiscussionsArgs) (*mcplib.CallToolResult, error) {
	client, err := s.getClient()
	if err != nil {
		return toolError(err)
	}

	course, err := canvas.FindCourse(ctx, client, args.Course)
	if err != nil {
		return toolError(err)
	}

	topics, err := collectIter(canvas.ListDiscussionTopics(ctx, client, course.ID, canvas.ListDiscussionTopicsOptions{}))
	if err != nil {
		return toolError(err)
	}

	type topicSummary struct {
		ID          int64      `json:"id"`
		Title       string     `json:"title"`
		Author      string     `json:"author"`
		PostedAt    *time.Time `json:"posted_at,omitempty"`
		LastReplyAt *time.Time `json:"last_reply_at,omitempty"`
		ReplyCount  int        `json:"reply_count"`
		UnreadCount int        `json:"unread_count"`
		Pinned      bool       `json:"pinned,omitempty"`
		Locked      bool       `json:"locked,omitempty"`
		HTMLURL     string     `json:"html_url"`
	}

	results := make([]topicSummary, 0, len(topics))
	for _, t := range topics {
		results = append(results, topicSummary{
			ID:          t.ID,
			Title:       t.Title,
			Author:      t.Author.Name,
			PostedAt:    t.PostedAt,
			LastReplyAt: t.LastReplyAt,
			ReplyCount:  t.DiscussionSubentryCount,
			UnreadCount: t.UnreadCount,
			Pinned:      t.Pinned,
			Locked:      t.Locked,
			HTMLURL:     t.HTMLURL,
		})
	}

	if len(results) == 0 {
		return mcplib.NewToolResultText("No discussion topics found."), nil
	}

	return jsonResult(results)
}

type getDiscussionArgs struct {
	Course     string `json:"course"`
	Discussion string `json:"discussion"`
}

func (s *Server) handleGetDiscussion(ctx context.Context, _ mcplib.CallToolRequest, args getDiscussionArgs) (*mcplib.CallToolResult, error) {
	client, err := s.getClient()
	if err != nil {
		return toolError(err)
	}

	course, err := canvas.FindCourse(ctx, client, args.Course)
	if err != nil {
		return toolError(err)
	}

	topic, err := canvas.FindDiscussionTopic(ctx, client, course.ID, args.Discussion)
	if err != nil {
		return toolError(err)
	}

	view, err := canvas.GetDiscussionTopicView(ctx, client, course.ID, topic.ID)
	if err != nil {
		return toolError(err)
	}

	// Build participant lookup
	participants := make(map[int64]string, len(view.Participants))
	for _, p := range view.Participants {
		participants[p.ID] = p.DisplayName
	}

	type reply struct {
		Author    string    `json:"author"`
		Message   string    `json:"message"`
		CreatedAt time.Time `json:"created_at"`
		Replies   []reply   `json:"replies,omitempty"`
	}

	var convertEntries func(entries []canvas.DiscussionEntry) []reply
	convertEntries = func(entries []canvas.DiscussionEntry) []reply {
		result := make([]reply, 0, len(entries))
		for _, e := range entries {
			author := participants[e.UserID]
			if author == "" {
				author = e.UserName
			}
			r := reply{
				Author:    author,
				Message:   htmlToMarkdown(e.Message),
				CreatedAt: e.CreatedAt,
			}
			if len(e.Replies) > 0 {
				r.Replies = convertEntries(e.Replies)
			}
			result = append(result, r)
		}
		return result
	}

	type discussionDetail struct {
		ID      int64   `json:"id"`
		Title   string  `json:"title"`
		Author  string  `json:"author"`
		Message string  `json:"message,omitempty"`
		Locked  bool    `json:"locked,omitempty"`
		Replies []reply `json:"replies"`
		HTMLURL string  `json:"html_url"`
	}

	detail := discussionDetail{
		ID:      topic.ID,
		Title:   topic.Title,
		Author:  topic.Author.Name,
		Locked:  topic.Locked,
		Replies: convertEntries(view.View),
		HTMLURL: topic.HTMLURL,
	}
	if topic.Message != nil && *topic.Message != "" {
		detail.Message = htmlToMarkdown(*topic.Message)
	}

	return jsonResult(detail)
}

type replyToDiscussionArgs struct {
	Course     string `json:"course"`
	Discussion string `json:"discussion"`
	Message    string `json:"message"`
}

func (s *Server) handleReplyToDiscussion(ctx context.Context, _ mcplib.CallToolRequest, args replyToDiscussionArgs) (*mcplib.CallToolResult, error) {
	client, err := s.getClient()
	if err != nil {
		return toolError(err)
	}

	course, err := canvas.FindCourse(ctx, client, args.Course)
	if err != nil {
		return toolError(err)
	}

	topic, err := canvas.FindDiscussionTopic(ctx, client, course.ID, args.Discussion)
	if err != nil {
		return toolError(err)
	}

	if topic.Locked {
		return mcplib.NewToolResultError(fmt.Sprintf("Discussion \"%s\" is locked and cannot receive replies.", topic.Title)), nil
	}

	entry, err := canvas.CreateDiscussionEntry(ctx, client, course.ID, topic.ID, args.Message)
	if err != nil {
		return toolError(err)
	}

	return mcplib.NewToolResultText(fmt.Sprintf("Reply posted to \"%s\" (entry ID: %d).", topic.Title, entry.ID)), nil
}
