package utils

import (
	"errors"
	"os"

	"github.com/golang-jwt/jwt/v5"
)

const TokenTypeAccess = "access"

// JWTClaim mirrors auth-service claims (contract §3). Any school id claim is ignored.
type JWTClaim struct {
	UserID       uint   `json:"user_id"`
	Email        string `json:"email"`
	Role         string `json:"role"`
	CustomerRole string `json:"customer_role"`
	Typ          string `json:"typ"`
	jwt.RegisteredClaims
}

var (
	ErrInvalidTokenType = errors.New("invalid token type")
	ErrMissingClaims    = errors.New("missing required claims")
)

// JWTSecret returns JWT_SECRET (main refuses to start when it is empty).
func JWTSecret() string { return os.Getenv("JWT_SECRET") }

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
	if claims.UserID == 0 {
		return nil, ErrMissingClaims
	}
	return claims, nil
}
