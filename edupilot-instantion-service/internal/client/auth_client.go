// Package client contains service-to-service HTTP clients.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/edupilot-instantion-service/internal/config"
)

// AuthUser is the payload returned by POST /internal/users.
type AuthUser struct {
	ID    uint   `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

type CreateUserRequest struct {
	Email    string  `json:"email"`
	Password string  `json:"password"`
	FullName string  `json:"full_name"`
	Phone    *string `json:"phone,omitempty"`
	Role     string  `json:"role"`
}

// InviteUserRequest is the body of POST /internal/users/invite (API_CONTRACT §7.3).
type InviteUserRequest struct {
	Email      string  `json:"email"`
	FullName   string  `json:"full_name"`
	Phone      *string `json:"phone,omitempty"`
	Role       string  `json:"role"`
	SchoolName string  `json:"school_name"`
}

// InvitedUser is returned by the invite / resend-invite endpoints.
type InvitedUser struct {
	ID        uint   `json:"id"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	EmailSent bool   `json:"email_sent"`
}

// AuthError is a non-2xx response from the auth service.
type AuthError struct {
	Status  int
	Code    string
	Message string
}

func (e *AuthError) Error() string {
	return fmt.Sprintf("auth service responded %d: %s", e.Status, e.Message)
}

type AuthClient struct {
	baseURL string
	secret  string
	http    *http.Client
}

var httpClient = &http.Client{Timeout: 10 * time.Second}

// NewAuthClient builds a client from AUTH_SERVICE_INTERNAL_URL / INTERNAL_SERVICE_SECRET.
func NewAuthClient() *AuthClient {
	return &AuthClient{
		baseURL: strings.TrimRight(config.Getenv("AUTH_SERVICE_INTERNAL_URL", "http://auth-service:8080"), "/"),
		secret:  config.Getenv("INTERNAL_SERVICE_SECRET", ""),
		http:    httpClient,
	}
}

func (a *AuthClient) do(ctx context.Context, method, path string, body any, out any) error {
	if a.secret == "" {
		return fmt.Errorf("INTERNAL_SERVICE_SECRET is not configured")
	}
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Internal-Secret", a.secret)

	resp, err := a.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var eb struct {
			Error   string `json:"error"`
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &eb)
		msg := eb.Error
		if msg == "" {
			msg = eb.Message
		}
		return &AuthError{Status: resp.StatusCode, Code: eb.Code, Message: msg}
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decode auth response: %w", err)
		}
	}
	return nil
}

// CreateUser calls POST /internal/users.
func (a *AuthClient) CreateUser(ctx context.Context, req CreateUserRequest) (*AuthUser, error) {
	var resp struct {
		Data AuthUser `json:"data"`
	}
	if err := a.do(ctx, http.MethodPost, "/internal/users", req, &resp); err != nil {
		return nil, err
	}
	if resp.Data.ID == 0 {
		return nil, fmt.Errorf("auth service returned no user id")
	}
	return &resp.Data, nil
}

// DeleteUser calls DELETE /internal/users/:id (compensation).
func (a *AuthClient) DeleteUser(ctx context.Context, id uint) error {
	return a.do(ctx, http.MethodDelete, fmt.Sprintf("/internal/users/%d", id), nil, nil)
}

// InviteUser calls POST /internal/users/invite (creates a teacher account without
// password and emails an activation link).
func (a *AuthClient) InviteUser(ctx context.Context, req InviteUserRequest) (*InvitedUser, error) {
	var resp struct {
		Data InvitedUser `json:"data"`
	}
	if err := a.do(ctx, http.MethodPost, "/internal/users/invite", req, &resp); err != nil {
		return nil, err
	}
	if resp.Data.ID == 0 {
		return nil, fmt.Errorf("auth service returned no user id")
	}
	return &resp.Data, nil
}

// ResendInvite calls POST /internal/users/:id/resend-invite (new token, old one invalidated).
func (a *AuthClient) ResendInvite(ctx context.Context, id uint, schoolName string) (*InvitedUser, error) {
	var resp struct {
		Data InvitedUser `json:"data"`
	}
	body := map[string]string{"school_name": schoolName}
	if err := a.do(ctx, http.MethodPost, fmt.Sprintf("/internal/users/%d/resend-invite", id), body, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}
