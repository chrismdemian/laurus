package canvas

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"net/url"
	"strconv"
	"strings"
)

// ListDiscussionTopicsOptions controls filtering for ListDiscussionTopics.
type ListDiscussionTopicsOptions struct {
	// Scope filters by lock state: "locked" or "unlocked".
	Scope string

	// OrderBy controls sort: "position", "recent_activity", "title".
	OrderBy string

	// SearchTerm filters topics by title (server-side).
	SearchTerm string

	// FilterBy filters by read state: "all" or "unread".
	FilterBy string
}

// ListDiscussionTopics returns an iterator over discussion topics for a course.
func ListDiscussionTopics(ctx context.Context, c *Client, courseID int64, opts ListDiscussionTopicsOptions) iter.Seq2[DiscussionTopic, error] {
	path := fmt.Sprintf("/api/v1/courses/%d/discussion_topics", courseID)

	params := url.Values{}
	if opts.Scope != "" {
		params.Set("scope", opts.Scope)
	}
	if opts.OrderBy != "" {
		params.Set("order_by", opts.OrderBy)
	}
	if opts.SearchTerm != "" {
		params.Set("search_term", opts.SearchTerm)
	}
	if opts.FilterBy != "" {
		params.Set("filter_by", opts.FilterBy)
	}

	return Paginate[DiscussionTopic](ctx, c, path, params)
}

// GetDiscussionTopicView retrieves the full thread view for a discussion topic.
// Returns participants, unread entry IDs, and the full tree of entries.
// This endpoint is NOT paginated — Canvas returns the entire thread.
func GetDiscussionTopicView(ctx context.Context, c *Client, courseID, topicID int64) (DiscussionTopicView, error) {
	path := fmt.Sprintf("/api/v1/courses/%d/discussion_topics/%d/view", courseID, topicID)
	return Get[DiscussionTopicView](ctx, c, path, nil)
}

// FindDiscussionTopic resolves a fuzzy query to a single discussion topic within a course.
// Priority: numeric ID direct lookup > exact title match > substring title match.
func FindDiscussionTopic(ctx context.Context, c *Client, courseID int64, query string) (DiscussionTopic, error) {
	// Try numeric ID first
	if id, err := strconv.ParseInt(query, 10, 64); err == nil {
		path := fmt.Sprintf("/api/v1/courses/%d/discussion_topics/%d", courseID, id)
		topic, err := Get[DiscussionTopic](ctx, c, path, nil)
		if err == nil {
			return topic, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return DiscussionTopic{}, fmt.Errorf("looking up topic %d: %w", id, err)
		}
	}

	// Collect all topics for fuzzy search
	var topics []DiscussionTopic
	for t, err := range ListDiscussionTopics(ctx, c, courseID, ListDiscussionTopicsOptions{}) {
		if err != nil {
			return DiscussionTopic{}, fmt.Errorf("listing topics for search: %w", err)
		}
		topics = append(topics, t)
	}

	q := strings.ToLower(query)

	// Exact title match
	for _, t := range topics {
		if strings.EqualFold(t.Title, query) {
			return t, nil
		}
	}

	// Substring match
	for _, t := range topics {
		if strings.Contains(strings.ToLower(t.Title), q) {
			return t, nil
		}
	}

	return DiscussionTopic{}, fmt.Errorf("no discussion topic matching %q: %w", query, ErrNotFound)
}
