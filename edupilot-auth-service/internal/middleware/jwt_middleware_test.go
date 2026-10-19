package middleware

import (
	"errors"
	"testing"
	"time"

	"github.com/edupilot-auth-service/internal/utils"
)

func withLookup(t *testing.T, f func(uint) (int64, error)) {
	orig := revokedBeforeLookup
	revokedBeforeLookup = f
	t.Cleanup(func() { revokedBeforeLookup = orig })
}

func TestAuthenticateRevocation(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")
	access, _ := utils.GenerateAccessToken(5, "u@x.io", "customer", "students")
	refresh, _, _ := utils.GenerateRefreshToken(5, "u@x.io", "customer", "students")

	withLookup(t, func(uint) (int64, error) { return 0, nil })
	if c, err := authenticate(access); err != nil || c.CustomerRole != "students" {
		t.Fatalf("valid access token rejected: %v %+v", err, c)
	}
	if _, err := authenticate(refresh); err == nil {
		t.Fatal("refresh token accepted by auth middleware")
	}

	withLookup(t, func(uint) (int64, error) { return time.Now().Add(time.Minute).Unix(), nil })
	if _, err := authenticate(access); !errors.Is(err, utils.ErrTokenRevoked) {
		t.Fatalf("revoked token accepted: %v", err)
	}

	// Redis error: documented fail-open for the revocation check only.
	withLookup(t, func(uint) (int64, error) { return 0, errors.New("redis down") })
	if _, err := authenticate(access); err != nil {
		t.Fatalf("expected fail-open on redis error, got %v", err)
	}
	if _, err := authenticate("garbage"); err == nil {
		t.Fatal("invalid token accepted when redis down")
	}
}
