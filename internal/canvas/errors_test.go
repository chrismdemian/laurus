package canvas

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestParseErrorResponse(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		headers    http.Header
		wantErr    error
		wantMsg    string
	}{
		{
			name:       "shape 1: array of objects",
			statusCode: 400,
			body:       `{"errors":[{"message":"invalid parameter"}]}`,
			headers:    http.Header{},
			wantMsg:    "invalid parameter",
		},
		{
			name:       "shape 2: field map (422)",
			statusCode: 422,
			body:       `{"errors":{"name":[{"type":"required","message":"is required"}]}}`,
			headers:    http.Header{},
		},
		{
			name:       "shape 3: error string",
			statusCode: 400,
			body:       `{"error":"bad request"}`,
			headers:    http.Header{},
			wantMsg:    "bad request",
		},
		{
			name:       "shape 4: message string",
			statusCode: 500,
			body:       `{"message":"something went wrong"}`,
			headers:    http.Header{},
			wantMsg:    "something went wrong",
		},
		{
			name:       "401 with WWW-Authenticate",
			statusCode: 401,
			body:       `{"error":"unauthorized"}`,
			headers:    http.Header{"Www-Authenticate": {"Bearer"}},
			wantErr:    ErrTokenInvalid,
		},
		{
			name:       "401 without WWW-Authenticate",
			statusCode: 401,
			body:       `{"error":"unauthorized"}`,
			headers:    http.Header{},
			wantErr:    ErrPermissionDenied,
		},
		{
			name:       "404",
			statusCode: 404,
			body:       `{"message":"The specified resource does not exist"}`,
			headers:    http.Header{},
			wantErr:    ErrNotFound,
		},
		{
			name:       "429",
			statusCode: 429,
			body:       `{"errors":[{"message":"Rate limit exceeded"}]}`,
			headers:    http.Header{},
			wantErr:    ErrRateLimited,
		},
		{
			name:       "empty body",
			statusCode: 500,
			body:       "",
			headers:    http.Header{},
			wantMsg:    "Internal Server Error",
		},
		{
			name:       "invalid JSON",
			statusCode: 502,
			body:       "<html>Bad Gateway</html>",
			headers:    http.Header{},
			wantMsg:    "<html>Bad Gateway</html>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parseErrorResponse(tt.statusCode, []byte(tt.body), tt.headers)
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("errors.Is(%v, %v) = false", err, tt.wantErr)
				}
			}

			if tt.wantMsg != "" {
				var apiErr *APIError
				if errors.As(err, &apiErr) {
					if apiErr.Message != tt.wantMsg {
						t.Errorf("message = %q, want %q", apiErr.Message, tt.wantMsg)
					}
				} else {
					t.Errorf("expected *APIError, got %T", err)
				}
			}
		})
	}
}

func TestParseErrorResponse_422Validation(t *testing.T) {
	body := `{"errors":{"name":[{"type":"required","message":"is required"}],"email":[{"type":"invalid","message":"is not valid"}]}}`
	err := parseErrorResponse(422, []byte(body), http.Header{})

	var ve *ErrValidation
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ErrValidation, got %T: %v", err, err)
	}

	if len(ve.Fields) != 2 {
		t.Errorf("expected 2 fields, got %d", len(ve.Fields))
	}

	nameErrs := ve.Fields["name"]
	if len(nameErrs) != 1 || nameErrs[0] != "is required" {
		t.Errorf("name errors = %v", nameErrs)
	}

	emailErrs := ve.Fields["email"]
	if len(emailErrs) != 1 || emailErrs[0] != "is not valid" {
		t.Errorf("email errors = %v", emailErrs)
	}
}

func TestAPIError_Unwrap(t *testing.T) {
	err := &APIError{StatusCode: 404, Message: "not found", Err: ErrNotFound}

	if !errors.Is(err, ErrNotFound) {
		t.Error("expected errors.Is to match ErrNotFound")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Error("expected errors.As to match *APIError")
	}
}

func TestErrValidation_Error(t *testing.T) {
	ve := &ErrValidation{Fields: map[string][]string{
		"name": {"is required"},
	}}
	msg := ve.Error()
	if msg == "" {
		t.Error("expected non-empty error message")
	}
	if !strings.Contains(msg, "name") || !strings.Contains(msg, "is required") {
		t.Errorf("unexpected message: %s", msg)
	}
}
