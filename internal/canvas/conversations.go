package canvas

import (
	"context"
	"fmt"
	"iter"
	"net/url"
	"strconv"
	"strings"
)

// ListConversationsOptions controls filtering for ListConversations.
type ListConversationsOptions struct {
	// Scope filters by: "inbox" (default), "unread", "starred", "archived", "sent".
	Scope string

	// Filter limits to specific course context codes (e.g., ["course_123"]).
	Filter []string
}

// ListConversations returns an iterator over the current user's conversations.
func ListConversations(ctx context.Context, c *Client, opts ListConversationsOptions) iter.Seq2[Conversation, error] {
	path := "/api/v1/conversations"

	params := url.Values{}
	if opts.Scope != "" {
		params.Set("scope", opts.Scope)
	}
	for _, f := range opts.Filter {
		params.Add("filter[]", f)
	}

	return Paginate[Conversation](ctx, c, path, params)
}

// GetConversation retrieves a single conversation with full message history.
// Canvas auto-marks the conversation as read by default.
func GetConversation(ctx context.Context, c *Client, conversationID int64) (Conversation, error) {
	path := fmt.Sprintf("/api/v1/conversations/%d", conversationID)
	return Get[Conversation](ctx, c, path, nil)
}

// unreadCountResponse handles the Canvas unread_count endpoint which returns a string value.
type unreadCountResponse struct {
	UnreadCount string `json:"unread_count"`
}

// GetUnreadCount returns the number of unread conversations for the current user.
func GetUnreadCount(ctx context.Context, c *Client) (int, error) {
	resp, err := Get[unreadCountResponse](ctx, c, "/api/v1/conversations/unread_count", nil)
	if err != nil {
		return 0, fmt.Errorf("fetching unread count: %w", err)
	}
	n, err := strconv.Atoi(resp.UnreadCount)
	if err != nil {
		return 0, fmt.Errorf("parsing unread count %q: %w", resp.UnreadCount, err)
	}
	return n, nil
}

// SearchRecipients searches for message recipients within an optional context.
// The contextCode (e.g., "course_123") limits results to that course.
func SearchRecipients(ctx context.Context, c *Client, search string, contextCode string) ([]Recipient, error) {
	params := url.Values{}
	params.Set("search", search)
	if contextCode != "" {
		params.Set("context", contextCode)
	}
	params.Set("per_page", "10")

	return Get[[]Recipient](ctx, c, "/api/v1/search/recipients", params)
}

// conversationCreateRequest wraps the fields Canvas expects for POST /conversations.
// Canvas expects form-style parameters, but also accepts JSON with these field names.
type conversationCreateRequest struct {
	Recipients  []string `json:"recipients"`
	Subject     string   `json:"subject"`
	Body        string   `json:"body"`
	ContextCode string   `json:"context_code,omitempty"`
}

// CreateConversation sends a new inbox message.
// Canvas returns an array of conversations (one per recipient group).
func CreateConversation(ctx context.Context, c *Client, req CreateConversationRequest) ([]Conversation, error) {
	return Post[[]Conversation](ctx, c, "/api/v1/conversations", conversationCreateRequest{
		Recipients:  req.Recipients,
		Subject:     req.Subject,
		Body:        req.Body,
		ContextCode: req.ContextCode,
	})
}

// addMessageRequest is the JSON body for adding a message to an existing conversation.
type addMessageRequest struct {
	Body string `json:"body"`
}

// AddConversationMessage adds a reply to an existing conversation.
func AddConversationMessage(ctx context.Context, c *Client, conversationID int64, body string) (Conversation, error) {
	path := fmt.Sprintf("/api/v1/conversations/%d/add_message", conversationID)
	return Post[Conversation](ctx, c, path, addMessageRequest{Body: body})
}

// FindRecipient resolves a fuzzy query to a single recipient.
// Priority: exact name match > substring name match.
func FindRecipient(ctx context.Context, c *Client, query string, contextCode string) (Recipient, error) {
	recipients, err := SearchRecipients(ctx, c, query, contextCode)
	if err != nil {
		return Recipient{}, fmt.Errorf("searching recipients: %w", err)
	}

	if len(recipients) == 0 {
		return Recipient{}, fmt.Errorf("no recipient matching %q: %w", query, ErrNotFound)
	}

	q := strings.ToLower(query)

	// Exact name match
	for _, r := range recipients {
		if strings.EqualFold(r.Name, query) || strings.EqualFold(r.FullName, query) {
			return r, nil
		}
	}

	// Substring match
	for _, r := range recipients {
		if strings.Contains(strings.ToLower(r.Name), q) || strings.Contains(strings.ToLower(r.FullName), q) {
			return r, nil
		}
	}

	// Return first result from server search
	return recipients[0], nil
}
