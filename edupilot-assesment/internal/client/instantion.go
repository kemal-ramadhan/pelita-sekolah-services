// Package client calls the instantion-service internal routes (contract §13.1).
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrUnavailable means instantion could not be reached or answered 5xx.
var ErrUnavailable = errors.New("instantion unavailable")

// Principal = GET /internal/assessment/principal.
type Principal struct {
	Kind          string     `json:"kind"` // manager | counselor | teacher | student | none
	SchoolID      *uuid.UUID `json:"school_id"`
	SchoolName    string     `json:"school_name"`
	SchoolActive  bool       `json:"school_active"`
	EmployeeID    *uuid.UUID `json:"employee_id,omitempty"`
	IsCounselor   bool       `json:"is_counselor"`
	StudentID     *uuid.UUID `json:"student_id,omitempty"`
	StudentName   string     `json:"student_name,omitempty"`
	StudentStatus string     `json:"student_status,omitempty"`
	ClassID       *uuid.UUID `json:"class_id,omitempty"`
	ClassName     string     `json:"class_name,omitempty"`
	DisplayName   string     `json:"display_name,omitempty"`
}

// Student = one row of POST /internal/assessment/students.
type Student struct {
	StudentID uuid.UUID  `json:"student_id"`
	UserID    *uint      `json:"user_id,omitempty"`
	FullName  string     `json:"full_name"`
	NIS       string     `json:"nis"`
	NISN      string     `json:"nisn"`
	Gender    string     `json:"gender"`
	ClassID   *uuid.UUID `json:"class_id,omitempty"`
	ClassName string     `json:"class_name,omitempty"`
}

// Class = one row of GET /internal/assessment/classes.
type Class struct {
	ID               uuid.UUID `json:"id"`
	Name             string    `json:"name"`
	GradeLevel       string    `json:"grade_level,omitempty"`
	AcademicYearName string    `json:"academic_year_name,omitempty"`
	IsActiveYear     bool      `json:"is_active_year"`
	StudentCount     int       `json:"student_count"`
}

// Notification = POST /internal/assessment/notify body.
type Notification struct {
	SchoolID     uuid.UUID      `json:"school_id"`
	UserIDs      []uint         `json:"user_ids,omitempty"`
	ToCounselors bool           `json:"to_counselors,omitempty"`
	Type         string         `json:"type"`
	Title        string         `json:"title"`
	Body         string         `json:"body"`
	Data         map[string]any `json:"data,omitempty"`
}

// Instantion is the API used by the assessment service (an interface so the
// HTTP handlers can be tested with a fake).
type Instantion interface {
	Principal(ctx context.Context, userID uint, role, customerRole string) (*Principal, error)
	Students(ctx context.Context, schoolID uuid.UUID, classIDs, studentIDs []uuid.UUID) ([]Student, error)
	Classes(ctx context.Context, schoolID uuid.UUID) ([]Class, error)
	Notify(ctx context.Context, n Notification) error
}

// HTTPInstantion talks to INSTANTION_INTERNAL_URL with X-Gateway-Auth.
type HTTPInstantion struct {
	BaseURL string
	Secret  string
	HTTP    *http.Client
}

// NewFromEnv builds the default client.
func NewFromEnv() *HTTPInstantion {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("INSTANTION_INTERNAL_URL")), "/")
	if base == "" {
		base = "http://instantion-service:8080"
	}
	return &HTTPInstantion{BaseURL: base, Secret: os.Getenv("GATEWAY_SECRET"), HTTP: &http.Client{Timeout: 8 * time.Second}}
}

func (c *HTTPInstantion) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("X-Gateway-Auth", c.Secret)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	blob, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%w: %s %s → %d", ErrUnavailable, method, path, resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(blob, out); err != nil {
		return fmt.Errorf("%w: decode %s: %v", ErrUnavailable, path, err)
	}
	return nil
}

func (c *HTTPInstantion) Principal(ctx context.Context, userID uint, role, customerRole string) (*Principal, error) {
	q := url.Values{}
	q.Set("user_id", strconv.FormatUint(uint64(userID), 10))
	q.Set("role", role)
	q.Set("customer_role", customerRole)
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/internal/assessment/principal?"+q.Encode(), nil, &raw); err != nil {
		return nil, err
	}
	// Accept both the top-level shape and a { data: … } envelope.
	var env struct {
		Data *Principal `json:"data"`
	}
	if json.Unmarshal(raw, &env) == nil && env.Data != nil {
		return env.Data, nil
	}
	var p Principal
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("%w: decode principal: %v", ErrUnavailable, err)
	}
	return &p, nil
}

func (c *HTTPInstantion) Students(ctx context.Context, schoolID uuid.UUID, classIDs, studentIDs []uuid.UUID) ([]Student, error) {
	body := map[string]any{"school_id": schoolID, "class_ids": nonNilU(classIDs), "student_ids": nonNilU(studentIDs)}
	var out struct {
		Data []Student `json:"data"`
	}
	if err := c.do(ctx, http.MethodPost, "/internal/assessment/students", body, &out); err != nil {
		return nil, err
	}
	if out.Data == nil {
		out.Data = []Student{}
	}
	return out.Data, nil
}

func (c *HTTPInstantion) Classes(ctx context.Context, schoolID uuid.UUID) ([]Class, error) {
	var out struct {
		Data []Class `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/internal/assessment/classes?school_id="+schoolID.String(), nil, &out); err != nil {
		return nil, err
	}
	if out.Data == nil {
		out.Data = []Class{}
	}
	return out.Data, nil
}

func (c *HTTPInstantion) Notify(ctx context.Context, n Notification) error {
	return c.do(ctx, http.MethodPost, "/internal/assessment/notify", n, nil)
}

func nonNilU(s []uuid.UUID) []uuid.UUID {
	if s == nil {
		return []uuid.UUID{}
	}
	return s
}

// Default is the process-wide client (set in main; replaced by fakes in tests).
var Default Instantion
