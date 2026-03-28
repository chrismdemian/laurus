package canvas

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Sentinel errors for Canvas API responses.
var (
	ErrTokenInvalid     = errors.New("canvas: invalid or expired token")
	ErrPermissionDenied = errors.New("canvas: permission denied")
	ErrNotFound         = errors.New("canvas: resource not found")
	ErrForbidden        = errors.New("canvas: forbidden")
	ErrRateLimited      = errors.New("canvas: rate limit exceeded after retries")
)

// APIError wraps a Canvas API error with status code and message.
type APIError struct {
	StatusCode int
	Message    string
	Err        error
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("canvas: %s (HTTP %d)", e.Message, e.StatusCode)
	}
	return fmt.Sprintf("canvas: HTTP %d", e.StatusCode)
}

func (e *APIError) Unwrap() error {
	return e.Err
}

// ErrValidation represents a 422 error with field-level details.
type ErrValidation struct {
	Fields map[string][]string
}

func (e *ErrValidation) Error() string {
	var parts []string
	for field, msgs := range e.Fields {
		parts = append(parts, fmt.Sprintf("%s (%s)", field, strings.Join(msgs, ", ")))
	}
	return fmt.Sprintf("canvas: validation failed: %s", strings.Join(parts, "; "))
}

// parseErrorResponse interprets a non-2xx Canvas API response into a typed error.
func parseErrorResponse(statusCode int, body []byte, headers http.Header) error {
	// Determine sentinel based on status + headers
	var sentinel error
	switch statusCode {
	case http.StatusUnauthorized:
		if headers.Get("WWW-Authenticate") != "" {
			sentinel = ErrTokenInvalid
		} else {
			sentinel = ErrPermissionDenied
		}
	case http.StatusForbidden:
		sentinel = ErrForbidden
	case http.StatusNotFound:
		sentinel = ErrNotFound
	case http.StatusTooManyRequests:
		sentinel = ErrRateLimited
	}

	// Try to extract a human-readable message from the body
	msg := extractMessage(statusCode, body)

	// For 422, try to build a validation error
	if statusCode == http.StatusUnprocessableEntity {
		if ve := parseValidationError(body); ve != nil {
			return ve
		}
	}

	if sentinel != nil {
		return &APIError{StatusCode: statusCode, Message: msg, Err: sentinel}
	}

	return &APIError{StatusCode: statusCode, Message: msg}
}

// rawEnvelope captures all possible Canvas error JSON shapes.
type rawEnvelope struct {
	Errors  json.RawMessage `json:"errors"`
	Error   string          `json:"error"`
	Message string          `json:"message"`
}

func extractMessage(statusCode int, body []byte) string {
	if len(body) == 0 {
		return http.StatusText(statusCode)
	}

	var env rawEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return string(body)
	}

	// Shape 3: {"error": "string"}
	if env.Error != "" {
		return env.Error
	}

	// Shape 4: {"message": "string"}
	if env.Message != "" {
		return env.Message
	}

	// Shapes 1 and 2 both use the "errors" key
	if len(env.Errors) > 0 {
		return parseErrorsField(env.Errors)
	}

	return http.StatusText(statusCode)
}

func parseErrorsField(raw json.RawMessage) string {
	// Shape 1: [{"message": "..."}]
	if len(raw) > 0 && raw[0] == '[' {
		var arr []struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(raw, &arr) == nil && len(arr) > 0 {
			var msgs []string
			for _, e := range arr {
				if e.Message != "" {
					msgs = append(msgs, e.Message)
				}
			}
			if len(msgs) > 0 {
				return strings.Join(msgs, "; ")
			}
		}
	}

	// Shape 2: {"field": [{"type": "...", "message": "..."}]}
	if len(raw) > 0 && raw[0] == '{' {
		var fields map[string][]struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		}
		if json.Unmarshal(raw, &fields) == nil {
			var parts []string
			for field, errs := range fields {
				for _, e := range errs {
					parts = append(parts, fmt.Sprintf("%s: %s", field, e.Message))
				}
			}
			if len(parts) > 0 {
				return strings.Join(parts, "; ")
			}
		}
	}

	return string(raw)
}

func parseValidationError(body []byte) *ErrValidation {
	var env struct {
		Errors json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(body, &env) != nil || len(env.Errors) == 0 || env.Errors[0] != '{' {
		return nil
	}

	var fields map[string][]struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	}
	if json.Unmarshal(env.Errors, &fields) != nil || len(fields) == 0 {
		return nil
	}

	result := &ErrValidation{Fields: make(map[string][]string)}
	for field, errs := range fields {
		for _, e := range errs {
			result.Fields[field] = append(result.Fields[field], e.Message)
		}
	}
	return result
}
