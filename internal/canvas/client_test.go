package canvas

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
)

// newTestClient creates a Client pointing at a test server.
func newTestClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := NewClient(server.URL, "test-token", "test")
	return client, server
}

func TestTransport_AuthHeader(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token" {
			t.Errorf("Authorization = %q, want 'Bearer test-token'", auth)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{}`)
	})

	_, err := client.do(context.Background(), "GET", "/api/v1/test", nil)
	if err != nil {
		t.Fatalf("do() error: %v", err)
	}
}

func TestTransport_UserAgent(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		ua := r.Header.Get("User-Agent")
		if ua != "Laurus/test" {
			t.Errorf("User-Agent = %q, want 'Laurus/test'", ua)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{}`)
	})

	_, err := client.do(context.Background(), "GET", "/api/v1/test", nil)
	if err != nil {
		t.Fatalf("do() error: %v", err)
	}
}

func TestTransport_RateLimitAdjustment(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Rate-Limit-Remaining", "30")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{}`)
	})

	_, err := client.do(context.Background(), "GET", "/api/v1/test", nil)
	if err != nil {
		t.Fatalf("do() error: %v", err)
	}

	// After seeing remaining=30 (< 50), limiter should be throttled
	client.mu.Lock()
	currentLimit := client.limiter.Limit()
	client.mu.Unlock()

	if currentLimit != 2 {
		t.Errorf("rate limit = %v, want 2 (throttled)", currentLimit)
	}
}

func TestTransport_RateLimitRecovery(t *testing.T) {
	var callCount atomic.Int32
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
		if n == 1 {
			w.Header().Set("X-Rate-Limit-Remaining", "30")
		} else {
			w.Header().Set("X-Rate-Limit-Remaining", "200")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{}`)
	})

	// First call: throttle
	_, _ = client.do(context.Background(), "GET", "/api/v1/test", nil)
	// Second call: recover
	_, _ = client.do(context.Background(), "GET", "/api/v1/test", nil)

	client.mu.Lock()
	currentLimit := client.limiter.Limit()
	client.mu.Unlock()

	if currentLimit != 10 {
		t.Errorf("rate limit = %v, want 10 (recovered)", currentLimit)
	}
}

func TestGet_Success(t *testing.T) {
	type testData struct {
		Name string `json:"name"`
		ID   int    `json:"id"`
	}

	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("method = %s, want GET", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(testData{Name: "Test Course", ID: 42})
	})

	result, err := Get[testData](context.Background(), client, "/api/v1/courses/42", nil)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}

	if result.Name != "Test Course" || result.ID != 42 {
		t.Errorf("Get() = %+v, want {Name:Test Course ID:42}", result)
	}
}

func TestGet_WithParams(t *testing.T) {
	type testData struct {
		ID int `json:"id"`
	}

	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("per_page") != "50" {
			t.Errorf("per_page = %s, want 50", r.URL.Query().Get("per_page"))
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"id": 1}`)
	})

	params := url.Values{"per_page": {"50"}}
	_, err := Get[testData](context.Background(), client, "/api/v1/test", params)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
}

func TestPost_Success(t *testing.T) {
	type reqBody struct {
		Name string `json:"name"`
	}
	type respBody struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}

	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %s, want application/json", r.Header.Get("Content-Type"))
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprint(w, `{"id":1,"name":"Test"}`)
	})

	result, err := Post[respBody](context.Background(), client, "/api/v1/test", reqBody{Name: "Test"})
	if err != nil {
		t.Fatalf("Post() error: %v", err)
	}

	if result.ID != 1 || result.Name != "Test" {
		t.Errorf("Post() = %+v", result)
	}
}

func TestDelete_Success(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			t.Errorf("method = %s, want DELETE", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	err := Delete(context.Background(), client, "/api/v1/test/1")
	if err != nil {
		t.Fatalf("Delete() error: %v", err)
	}
}

func TestGet_ErrorResponse(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprint(w, `{"message":"not found"}`)
	})

	type empty struct{}
	_, err := Get[empty](context.Background(), client, "/api/v1/missing", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !isNotFound(err) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func isNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}
