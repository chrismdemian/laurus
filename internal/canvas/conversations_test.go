package canvas

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

// TestGetConversation_DoesNotMarkRead guards the invariant that fetching a
// conversation never flips it to read: Canvas defaults auto_mark_as_read to
// true, so the request must carry auto_mark_as_read=false explicitly.
func TestGetConversation_DoesNotMarkRead(t *testing.T) {
	conv := Conversation{ID: 42, Subject: "Office hours"}
	data, _ := json.Marshal(conv)

	var gotPath, gotAutoMark string
	var hadParam bool
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAutoMark = r.URL.Query().Get("auto_mark_as_read")
		hadParam = r.URL.Query().Has("auto_mark_as_read")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	})

	got, err := GetConversation(context.Background(), client, 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/v1/conversations/42" {
		t.Errorf("path = %q, want /api/v1/conversations/42", gotPath)
	}
	if !hadParam {
		t.Fatalf("request did not include auto_mark_as_read; Canvas would mark the conversation read")
	}
	if gotAutoMark != "false" {
		t.Errorf("auto_mark_as_read = %q, want \"false\"", gotAutoMark)
	}
	if got.ID != 42 || got.Subject != "Office hours" {
		t.Errorf("conversation = %+v, want ID 42 / Office hours", got)
	}
}
