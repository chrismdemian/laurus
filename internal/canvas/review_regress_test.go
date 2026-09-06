package canvas

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

// Regression tests from the 2026-09-06 re-review.

// Announcements: Canvas defaults end_date to 28 days after start_date, so
// an old start_date alone was an EMPTY window (measured live: 0 rows for
// courses holding 15, 34 and 16). ListAnnouncements must send a far-future
// end_date when the caller sets only StartDate, and pass an explicit
// EndDate through unchanged.
func TestListAnnouncements_EndDateDefaultsFarFuture(t *testing.T) {
	var gotQuery atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery.Store(r.URL.Query())
		fmt.Fprint(w, `[]`)
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "tok", "test")
	ctx := context.Background()

	for range ListAnnouncements(ctx, c, ListAnnouncementsOptions{ContextCodes: []string{"course_1"}, StartDate: "2000-01-01"}) {
	}
	q := gotQuery.Load().(url.Values)
	end := q["end_date"]
	if len(end) != 1 {
		t.Fatalf("end_date not sent with StartDate alone: %v", q)
	}
	parsed, err := time.Parse("2006-01-02", end[0])
	if err != nil || !parsed.After(time.Now().AddDate(0, 6, 0)) {
		t.Errorf("default end_date = %q, want a far-future date", end[0])
	}

	for range ListAnnouncements(ctx, c, ListAnnouncementsOptions{ContextCodes: []string{"course_1"}, StartDate: "2000-01-01", EndDate: "2001-02-03"}) {
	}
	if q := gotQuery.Load().(url.Values); len(q["end_date"]) != 1 || q["end_date"][0] != "2001-02-03" {
		t.Errorf("explicit EndDate not passed through: %v", q)
	}

	for range ListAnnouncements(ctx, c, ListAnnouncementsOptions{ContextCodes: []string{"course_1"}}) {
	}
	if q := gotQuery.Load().(url.Values); len(q["end_date"]) != 0 {
		t.Errorf("no StartDate must mean no end_date (Canvas's own 14-day default applies): %v", q)
	}
}

// Canvas throttling is a 403 (not 429) with "Rate Limit Exceeded" in the
// body. It used to map to ErrForbidden and was never retried, so a sync
// under load recorded real resources as "skipped".
func TestThrottled403_IsRateLimitedAndRetried(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) == 1 {
			w.Header().Set("X-Rate-Limit-Remaining", "0")
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, "403 Forbidden (Rate Limit Exceeded)")
			return
		}
		fmt.Fprint(w, `{"id":42,"name":"ok"}`)
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "tok", "test")
	type obj struct {
		ID int `json:"id"`
	}
	// retryablehttp waits its default minimum (1s) before the retry.
	got, err := Get[obj](context.Background(), c, "/api/v1/courses/42", nil)
	if err != nil || got.ID != 42 {
		t.Fatalf("throttled request not retried to success: got=%+v err=%v", got, err)
	}
	if n := atomic.LoadInt32(&hits); n != 2 {
		t.Errorf("hits = %d, want 2 (one throttle, one retry)", n)
	}
}

// A plain permission 403 must still be ErrForbidden and must not be retried.
func TestPlain403_StaysForbiddenNotRetried(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("X-Rate-Limit-Remaining", "699")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"errors":[{"message":"user not authorized to perform that action"}]}`)
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "tok", "test")
	type obj struct{}
	_, err := Get[obj](context.Background(), c, "/api/v1/courses/42/pages", nil)
	if !errors.Is(err, ErrForbidden) || errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Errorf("hits = %d, want 1 (no retry)", n)
	}
	// The peeked body must have been put back: the message survives.
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Message == "" {
		t.Errorf("error body lost after the retry peek: %v", err)
	}
}

func TestIsThrottled403(t *testing.T) {
	h := func(rem string) http.Header {
		hd := http.Header{}
		if rem != "" {
			hd.Set("X-Rate-Limit-Remaining", rem)
		}
		return hd
	}
	cases := []struct {
		rem, body string
		want      bool
	}{
		{"", "403 Forbidden (Rate Limit Exceeded)", true},
		{"", "rate limit exceeded", true},
		{"0", `{"errors":[{"message":"x"}]}`, true},
		{"-3.5", "", true},
		{"699", `{"errors":[{"message":"forbidden"}]}`, false},
		{"", `{"errors":[{"message":"user not authorized"}]}`, false},
	}
	for _, tc := range cases {
		if got := isThrottled403(h(tc.rem), []byte(tc.body)); got != tc.want {
			t.Errorf("rem=%q body=%q: got %v want %v", tc.rem, tc.body, got, tc.want)
		}
	}
	if !errors.Is(parseErrorResponse(403, []byte("Rate Limit Exceeded"), h("")), ErrRateLimited) {
		t.Error("parseErrorResponse must map a throttled 403 to ErrRateLimited")
	}
	if !errors.Is(parseErrorResponse(403, []byte(`{"errors":[{"message":"no"}]}`), h("500")), ErrForbidden) {
		t.Error("parseErrorResponse must keep a plain 403 as ErrForbidden")
	}
}
