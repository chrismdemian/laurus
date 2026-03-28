package canvas

import (
	"context"
	"fmt"
	"iter"
	"net/url"
	"strconv"
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
