package utils

import (
	"errors"
	"os"

	"github.com/golang-jwt/jwt/v5"
)

// TokenTypeAccess is the only token type accepted by this service.
const TokenTypeAccess = "access"

// JWTClaim mirrors the claims issued by edupilot-auth-service (see API_CONTRACT.md §3).
type JWTClaim struct {
	UserID uint   `json:"user_id"`
	Email  string `json:"email"`
	Role   string `json:"role"`
	// CustomerRole is customers.role ("" if none); student principal =
	// role "customer" + customer_role "students" (API_CONTRACT §10.1).
	CustomerRole string `json:"customer_role"`
	Typ          string `json:"typ"`
	jwt.RegisteredClaims
}

var (
	ErrInvalidTokenType = errors.New("invalid token type")
	ErrMissingClaims    = errors.New("missing required claims")
)

// JWTSecret returns the configured signing secret.
func JWTSecret() string {
	return os.Getenv("JWT_SECRET")
}

// ParseAccessToken validates signature (HS256 only), expiry and token type.
// It does NOT check revocation (that requires Redis, see middleware).
func ParseAccessToken(tokenStr, secret string) (*JWTClaim, error) {
	if secret == "" {
		return nil, errors.New("jwt secret not configured")
	}
	claims := &JWTClaim{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (interface{}, error) {
		return []byte(secret), nil
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
	)
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, jwt.ErrSignatureInvalid
	}
	if claims.Typ != TokenTypeAccess {
		return nil, ErrInvalidTokenType
	}
	if claims.UserID == 0 || claims.IssuedAt == nil {
		return nil, ErrMissingClaims
	}
	return claims, nil
}
