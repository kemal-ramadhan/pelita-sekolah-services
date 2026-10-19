package apperr

import (
	"fmt"
	"net/http"
)

// Error is a client-safe error (contract §1: { error, code }).
type Error struct {
	Status  int
	Code    string
	Message string
	Extra   map[string]any // merged into the top-level body (e.g. missing: [...])
}

func (e *Error) Error() string { return fmt.Sprintf("%d %s: %s", e.Status, e.Code, e.Message) }

func New(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

// With adds a top-level field to the error body.
func (e *Error) With(key string, v any) *Error {
	cp := *e
	cp.Extra = map[string]any{}
	for k, x := range e.Extra {
		cp.Extra[k] = x
	}
	cp.Extra[key] = v
	return &cp
}

func BadRequest(message string) *Error {
	return New(http.StatusBadRequest, "VALIDATION_ERROR", message)
}
func Invalid(code, message string) *Error {
	return New(http.StatusBadRequest, code, message)
}
func NotFound(message string) *Error        { return New(http.StatusNotFound, "NOT_FOUND", message) }
func Conflict(code, message string) *Error  { return New(http.StatusConflict, code, message) }
func Forbidden(code, message string) *Error { return New(http.StatusForbidden, code, message) }
func Unprocessable(code, message string) *Error {
	return New(http.StatusUnprocessableEntity, code, message)
}
func Unavailable(message string) *Error {
	return New(http.StatusServiceUnavailable, "UPSTREAM_UNAVAILABLE", message)
}
