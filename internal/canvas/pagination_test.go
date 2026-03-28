package canvas

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

type testItem struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func TestPaginate_SinglePage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `[{"id":1,"name":"a"},{"id":2,"name":"b"}]`)
	}))
	defer server.Close()
	client := NewClient(server.URL, "token", "test")

	var items []testItem
	for item, err := range Paginate[testItem](context.Background(), client, "/api/v1/items", nil) {
		if err != nil {
			t.Fatalf("Paginate() error: %v", err)
		}
		items = append(items, item)
	}

	if len(items) != 2 {
		t.Errorf("got %d items, want 2", len(items))
	}
}

func TestPaginate_MultiPage(t *testing.T) {
	var callCount int
	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		switch callCount {
		case 1:
			nextURL := fmt.Sprintf("http://%s/api/v1/items?page=2&per_page=100", r.Host)
			w.Header().Set("Link", fmt.Sprintf(`<%s>; rel="next"`, nextURL))
			_, _ = fmt.Fprint(w, `[{"id":1,"name":"a"}]`)
		case 2:
			nextURL := fmt.Sprintf("http://%s/api/v1/items?page=3&per_page=100", r.Host)
			w.Header().Set("Link", fmt.Sprintf(`<%s>; rel="next"`, nextURL))
			_, _ = fmt.Fprint(w, `[{"id":2,"name":"b"}]`)
		case 3:
			_, _ = fmt.Fprint(w, `[{"id":3,"name":"c"}]`)
		}
	}))
	defer server2.Close()
	client2 := NewClient(server2.URL, "token", "test")

	var items []testItem
	for item, err := range Paginate[testItem](context.Background(), client2, "/api/v1/items", nil) {
		if err != nil {
			t.Fatalf("Paginate() error: %v", err)
		}
		items = append(items, item)
	}

	if len(items) != 3 {
		t.Errorf("got %d items, want 3", len(items))
	}
	if callCount != 3 {
		t.Errorf("made %d requests, want 3", callCount)
	}
}

func TestPaginate_EmptyNextURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Canvas bug: Link header with empty URI
		w.Header().Set("Link", `<>; rel="next"`)
		_, _ = fmt.Fprint(w, `[{"id":1,"name":"a"}]`)
	}))
	defer server.Close()
	client := NewClient(server.URL, "token", "test")

	var items []testItem
	for item, err := range Paginate[testItem](context.Background(), client, "/api/v1/items", nil) {
		if err != nil {
			t.Fatalf("Paginate() error: %v", err)
		}
		items = append(items, item)
	}

	// Should stop after first page (empty URL treated as no next page)
	if len(items) != 1 {
		t.Errorf("got %d items, want 1 (should stop on empty next URL)", len(items))
	}
}

func TestPaginate_ErrorOnPage2(t *testing.T) {
	var callCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			nextURL := fmt.Sprintf("http://%s/api/v1/items?page=2", r.Host)
			w.Header().Set("Link", fmt.Sprintf(`<%s>; rel="next"`, nextURL))
			_, _ = fmt.Fprint(w, `[{"id":1,"name":"a"}]`)
		} else {
			w.WriteHeader(http.StatusForbidden)
			_, _ = fmt.Fprint(w, `{"message":"forbidden"}`)
		}
	}))
	defer server.Close()
	client := NewClient(server.URL, "token", "test")

	var items []testItem
	var gotErr bool
	for item, err := range Paginate[testItem](context.Background(), client, "/api/v1/items", nil) {
		if err != nil {
			gotErr = true
			break
		}
		items = append(items, item)
	}

	if len(items) != 1 {
		t.Errorf("got %d items from page 1, want 1", len(items))
	}
	if !gotErr {
		t.Error("expected error on page 2")
	}
}

func TestPaginate_EarlyTermination(t *testing.T) {
	var callCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		nextURL := fmt.Sprintf("http://%s/api/v1/items?page=%d", r.Host, callCount+1)
		w.Header().Set("Link", fmt.Sprintf(`<%s>; rel="next"`, nextURL))
		_, _ = fmt.Fprint(w, `[{"id":1,"name":"a"},{"id":2,"name":"b"}]`)
	}))
	defer server.Close()
	client := NewClient(server.URL, "token", "test")

	// Only take the first item then stop
	for _, err := range Paginate[testItem](context.Background(), client, "/api/v1/items", nil) {
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		break // stop after first item
	}

	if callCount != 1 {
		t.Errorf("made %d requests, want 1 (should stop early)", callCount)
	}
}

func TestPaginate_PerPage100(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if pp := r.URL.Query().Get("per_page"); pp != "100" {
			t.Errorf("per_page = %s, want 100", pp)
		}
		_, _ = fmt.Fprint(w, `[]`)
	}))
	defer server.Close()
	client := NewClient(server.URL, "token", "test")

	for _, err := range Paginate[testItem](context.Background(), client, "/api/v1/items", nil) {
		if err != nil {
			t.Fatalf("error: %v", err)
		}
	}
}

func TestPaginate_PreservesParams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if state := r.URL.Query().Get("enrollment_state"); state != "active" {
			t.Errorf("enrollment_state = %s, want active", state)
		}
		if pp := r.URL.Query().Get("per_page"); pp != "100" {
			t.Errorf("per_page = %s, want 100", pp)
		}
		_, _ = fmt.Fprint(w, `[]`)
	}))
	defer server.Close()
	client := NewClient(server.URL, "token", "test")

	params := url.Values{"enrollment_state": {"active"}}
	for _, err := range Paginate[testItem](context.Background(), client, "/api/v1/courses", params) {
		if err != nil {
			t.Fatalf("error: %v", err)
		}
	}
}
