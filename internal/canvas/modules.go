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

// ListModulesOptions controls filtering for ListModules.
type ListModulesOptions struct {
	// IncludeItems inlines module items in the response.
	IncludeItems bool

	// IncludeContentDetails adds due dates, points, and lock info to items.
	// Requires IncludeItems to be true.
	IncludeContentDetails bool

	// SearchTerm filters modules and items by name.
	SearchTerm string
}

// ListModules returns an iterator over modules for a course.
func ListModules(ctx context.Context, c *Client, courseID int64, opts ListModulesOptions) iter.Seq2[Module, error] {
	path := fmt.Sprintf("/api/v1/courses/%d/modules", courseID)

	params := url.Values{}
	if opts.IncludeItems {
		params.Add("include[]", "items")
	}
	if opts.IncludeContentDetails {
		params.Add("include[]", "content_details")
	}
	if opts.SearchTerm != "" {
		params.Set("search_term", opts.SearchTerm)
	}

	return Paginate[Module](ctx, c, path, params)
}

// ListModuleItems returns an iterator over items within a specific module.
func ListModuleItems(ctx context.Context, c *Client, courseID, moduleID int64, includeContentDetails bool) iter.Seq2[ModuleItem, error] {
	path := fmt.Sprintf("/api/v1/courses/%d/modules/%d/items", courseID, moduleID)

	params := url.Values{}
	if includeContentDetails {
		params.Add("include[]", "content_details")
	}

	return Paginate[ModuleItem](ctx, c, path, params)
}

// MarkModuleItemDone marks a module item as done (for must_mark_done requirement).
func MarkModuleItemDone(ctx context.Context, c *Client, courseID, moduleID, itemID int64) error {
	path := fmt.Sprintf("/api/v1/courses/%d/modules/%d/items/%d/done", courseID, moduleID, itemID)
	_, err := Put[map[string]any](ctx, c, path, struct{}{})
	return err
}

// MarkModuleItemUndone unmarks a module item (reverses MarkModuleItemDone).
func MarkModuleItemUndone(ctx context.Context, c *Client, courseID, moduleID, itemID int64) error {
	path := fmt.Sprintf("/api/v1/courses/%d/modules/%d/items/%d/done", courseID, moduleID, itemID)
	return Delete(ctx, c, path)
}

// FindModule resolves a fuzzy query to a single module within a course.
// Priority: numeric ID direct lookup > exact name match > substring name match.
func FindModule(ctx context.Context, c *Client, courseID int64, query string) (Module, error) {
	// Try numeric ID first
	if id, err := strconv.ParseInt(query, 10, 64); err == nil {
		path := fmt.Sprintf("/api/v1/courses/%d/modules/%d", courseID, id)
		mod, err := Get[Module](ctx, c, path, nil)
		if err == nil {
			return mod, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return Module{}, fmt.Errorf("looking up module %d: %w", id, err)
		}
	}

	// Collect all modules for fuzzy search
	var modules []Module
	for m, err := range ListModules(ctx, c, courseID, ListModulesOptions{}) {
		if err != nil {
			return Module{}, fmt.Errorf("listing modules for search: %w", err)
		}
		modules = append(modules, m)
	}

	q := strings.ToLower(query)

	// Exact name match
	for _, m := range modules {
		if strings.EqualFold(m.Name, query) {
			return m, nil
		}
	}

	// Substring match
	for _, m := range modules {
		if strings.Contains(strings.ToLower(m.Name), q) {
			return m, nil
		}
	}

	return Module{}, fmt.Errorf("no module matching %q: %w", query, ErrNotFound)
}
