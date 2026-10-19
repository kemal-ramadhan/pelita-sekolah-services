// Package apperr defines client-safe application errors. Services return
// *apperr.Error for expected failures; anything else is treated as an
// internal error (logged, generic message to the client).
package apperr

import (
	"errors"
	"fmt"
	"net/http"
)

type Error struct {
	Status  int
	Code    string
	Message string
	// Details holds field-level messages (field path -> message), returned to
	// the client as "details" (e.g. VALIDATION_ERROR on student forms).
	Details map[string]string
	// Data holds non-string details (e.g. OUTSIDE_GEOFENCE distance_m/radius_m),
	// returned as "details" when Details is empty.
	Data map[string]any
}

func (e *Error) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("%d %s: %s", e.Status, e.Code, e.Message)
	}
	return fmt.Sprintf("%d: %s", e.Status, e.Message)
}

func New(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

func BadRequest(message string) *Error {
	return New(http.StatusBadRequest, "VALIDATION_ERROR", message)
}

func NotFound(message string) *Error {
	return New(http.StatusNotFound, "NOT_FOUND", message)
}

func Conflict(code, message string) *Error {
	return New(http.StatusConflict, code, message)
}

// Validation returns a 400 VALIDATION_ERROR carrying field-level details.
// message is the human summary (usually the first field message).
func Validation(message string, details map[string]string) *Error {
	return &Error{Status: http.StatusBadRequest, Code: "VALIDATION_ERROR", Message: message, Details: details}
}

// WithDetails returns a copy of e with field-level details attached.
func (e *Error) WithDetails(details map[string]string) *Error {
	c := *e
	c.Details = details
	return &c
}

// InUseMessage is the generic message for a delete/update blocked by a
// foreign key (Postgres SQLSTATE 23503).
const InUseMessage = "Data tidak dapat dihapus atau diubah karena masih digunakan oleh data lain"

// InUse returns a 409 IN_USE error.
func InUse(message string) *Error {
	if message == "" {
		message = InUseMessage
	}
	return New(http.StatusConflict, "IN_USE", message)
}

// sqlStater is implemented by *pgconn.PgError.
type sqlStater interface{ SQLState() string }

// IsForeignKeyViolation reports whether err is a Postgres FK violation (23503),
// either translated by GORM (gorm.ErrForeignKeyViolated is checked by callers)
// or the raw driver error.
func IsForeignKeyViolation(err error) bool {
	var st sqlStater
	return errors.As(err, &st) && st.SQLState() == "23503"
}
