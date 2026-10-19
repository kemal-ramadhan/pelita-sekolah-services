package utils

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testSecret = "test-secret"

func sign(t *testing.T, method jwt.SigningMethod, key any, claims JWTClaim) string {
	t.Helper()
	s, err := jwt.NewWithClaims(method, claims).SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func baseClaims(typ string) JWTClaim {
	now := time.Now()
	return JWTClaim{
		UserID: 7, Email: "a@b.co", Role: "institution", Typ: typ,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
			ID:        "jti-1",
		},
	}
}

func TestParseAccessToken(t *testing.T) {
	good := sign(t, jwt.SigningMethodHS256, []byte(testSecret), baseClaims("access"))
	c, err := ParseAccessToken(good, testSecret)
	if err != nil || c.UserID != 7 || c.Role != "institution" {
		t.Fatalf("expected valid token, got %v %+v", err, c)
	}

	cases := map[string]string{
		"refresh":      sign(t, jwt.SigningMethodHS256, []byte(testSecret), baseClaims("refresh")),
		"no typ":       sign(t, jwt.SigningMethodHS256, []byte(testSecret), baseClaims("")),
		"wrong secret": sign(t, jwt.SigningMethodHS256, []byte("other"), baseClaims("access")),
		"HS512":        sign(t, jwt.SigningMethodHS512, []byte(testSecret), baseClaims("access")),
		"none":         sign(t, jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, baseClaims("access")),
	}
	expired := baseClaims("access")
	expired.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Minute))
	cases["expired"] = sign(t, jwt.SigningMethodHS256, []byte(testSecret), expired)
	noExp := baseClaims("access")
	noExp.ExpiresAt = nil
	cases["no exp"] = sign(t, jwt.SigningMethodHS256, []byte(testSecret), noExp)
	noIat := baseClaims("access")
	noIat.IssuedAt = nil
	cases["no iat"] = sign(t, jwt.SigningMethodHS256, []byte(testSecret), noIat)
	noUser := baseClaims("access")
	noUser.UserID = 0
	cases["no user"] = sign(t, jwt.SigningMethodHS256, []byte(testSecret), noUser)

	for name, tok := range cases {
		if _, err := ParseAccessToken(tok, testSecret); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	if _, err := ParseAccessToken(good, ""); err == nil {
		t.Error("empty secret must fail")
	}
}
