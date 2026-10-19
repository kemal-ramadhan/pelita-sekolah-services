package utils

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestTokenTypeEnforced(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")

	access, err := GenerateAccessToken(7, "a@b.c", "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	refresh, jti, err := GenerateRefreshToken(7, "a@b.c", "admin", "")
	if err != nil || jti == "" {
		t.Fatalf("refresh: %v jti=%q", err, jti)
	}

	c, err := ValidateToken(access, TokenTypeAccess)
	if err != nil {
		t.Fatalf("access should validate: %v", err)
	}
	if c.UserID != 7 || c.Role != "admin" || c.Typ != TokenTypeAccess || c.ID == "" {
		t.Fatalf("unexpected claims %+v", c)
	}
	if _, err := ValidateToken(refresh, TokenTypeAccess); err == nil {
		t.Fatal("refresh token must not be accepted as access token")
	}
	if _, err := ValidateToken(access, TokenTypeRefresh); err == nil {
		t.Fatal("access token must not be accepted as refresh token")
	}
}

func TestCustomerRoleClaim(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")

	access, err := GenerateAccessToken(9, "s@x.io", "customer", "students")
	if err != nil {
		t.Fatal(err)
	}
	refresh, _, err := GenerateRefreshToken(9, "s@x.io", "customer", "students")
	if err != nil {
		t.Fatal(err)
	}
	for typ, tok := range map[string]string{TokenTypeAccess: access, TokenTypeRefresh: refresh} {
		c, err := ValidateToken(tok, typ)
		if err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
		if c.CustomerRole != "students" || c.Role != "customer" {
			t.Fatalf("%s: unexpected claims %+v", typ, c)
		}
	}

	// Empty customer role is still emitted as "customer_role": "" in the payload.
	empty, err := GenerateAccessToken(1, "a@x.io", "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(empty, ".")
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(payload, &raw); err != nil {
		t.Fatal(err)
	}
	if v, ok := raw["customer_role"]; !ok || v != "" {
		t.Fatalf("customer_role claim missing or wrong: %v", raw)
	}
}

func TestRejectsOtherAlgorithmsAndSecrets(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")
	claims := JWTClaim{UserID: 1, Typ: TokenTypeAccess, RegisteredClaims: jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}}

	hs512, _ := jwt.NewWithClaims(jwt.SigningMethodHS512, claims).SignedString([]byte("test-secret"))
	if _, err := ValidateToken(hs512, TokenTypeAccess); err == nil {
		t.Fatal("HS512 must be rejected")
	}
	none, _ := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if _, err := ValidateToken(none, TokenTypeAccess); err == nil {
		t.Fatal("alg=none must be rejected")
	}
	other, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("other"))
	if _, err := ValidateToken(other, TokenTypeAccess); err == nil {
		t.Fatal("wrong secret must be rejected")
	}
}

func TestEmptySecretRefused(t *testing.T) {
	t.Setenv("JWT_SECRET", "")
	if _, err := GenerateAccessToken(1, "x", "customer", ""); err == nil {
		t.Fatal("must refuse to sign with empty secret")
	}
}

func TestIsRevoked(t *testing.T) {
	iat := time.Unix(1000, 0)
	cases := []struct {
		before int64
		want   bool
	}{{0, false}, {999, false}, {1000, false}, {1001, true}}
	for _, tc := range cases {
		if got := IsRevoked(iat, tc.before); got != tc.want {
			t.Errorf("IsRevoked(1000, %d) = %v, want %v", tc.before, got, tc.want)
		}
	}
}

func TestGenerateOTPRange(t *testing.T) {
	for i := 0; i < 1000; i++ {
		otp, err := GenerateOTP()
		if err != nil || otp < 100000 || otp > 999999 {
			t.Fatalf("bad otp %d %v", otp, err)
		}
	}
}
