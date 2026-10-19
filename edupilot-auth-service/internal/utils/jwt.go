package utils

import (
	"errors"
	"os"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	TokenTypeAccess  = "access"
	TokenTypeRefresh = "refresh"
)

var (
	ErrWrongTokenType = errors.New("wrong token type")
	ErrTokenRevoked   = errors.New("token revoked")
)

type JWTClaim struct {
	UserID uint   `json:"user_id"`
	Email  string `json:"email"`
	Role   string `json:"role"`
	// CustomerRole is customers.role ("" if the user has no customer row).
	// A student principal = role "customer" + customer_role "students" (API_CONTRACT §10.1).
	CustomerRole string `json:"customer_role"`
	Typ          string `json:"typ"`
	jwt.RegisteredClaims
}

func jwtSecret() []byte {
	return []byte(os.Getenv("JWT_SECRET"))
}

// durationFromEnv reads a number of seconds from env.
func durationFromEnv(envKey string, fallback time.Duration) time.Duration {
	val := os.Getenv(envKey)
	if val == "" {
		return fallback
	}
	secs, err := strconv.ParseInt(val, 10, 64)
	if err != nil || secs <= 0 {
		return fallback
	}
	return time.Duration(secs) * time.Second
}

func AccessTTL() time.Duration {
	return durationFromEnv("JWT_ACCESS_EXPIRES", 15*time.Minute)
}

func RefreshTTL() time.Duration {
	return durationFromEnv("JWT_REFRESH_EXPIRES", 7*24*time.Hour)
}

// SessionRevocationTTL is how long a revoked_before marker must live: the longest token lifetime.
func SessionRevocationTTL() time.Duration {
	if a, r := AccessTTL(), RefreshTTL(); a > r {
		return a
	}
	return RefreshTTL()
}

func generateToken(userID uint, email, role, customerRole, typ string, ttl time.Duration) (string, string, error) {
	if len(jwtSecret()) == 0 {
		return "", "", errors.New("JWT_SECRET not configured")
	}
	now := time.Now()
	jti := uuid.NewString()
	claims := JWTClaim{
		UserID:       userID,
		Email:        email,
		Role:         role,
		CustomerRole: customerRole,
		Typ:          typ,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(jwtSecret())
	return signed, jti, err
}

func GenerateAccessToken(userID uint, email, role, customerRole string) (string, error) {
	tok, _, err := generateToken(userID, email, role, customerRole, TokenTypeAccess, AccessTTL())
	return tok, err
}

// GenerateRefreshToken returns the signed token and its jti.
func GenerateRefreshToken(userID uint, email, role, customerRole string) (string, string, error) {
	return generateToken(userID, email, role, customerRole, TokenTypeRefresh, RefreshTTL())
}

// ValidateToken verifies signature (HS256 only), expiry and expected typ.
func ValidateToken(tokenStr, expectedTyp string) (*JWTClaim, error) {
	if len(jwtSecret()) == 0 {
		return nil, errors.New("JWT_SECRET not configured")
	}
	claims := &JWTClaim{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (interface{}, error) {
		return jwtSecret(), nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuedAt(), jwt.WithExpirationRequired())
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, jwt.ErrSignatureInvalid
	}
	if claims.Typ != expectedTyp {
		return nil, ErrWrongTokenType
	}
	if claims.UserID == 0 || claims.IssuedAt == nil {
		return nil, jwt.ErrTokenInvalidClaims
	}
	return claims, nil
}

// IsRevoked reports whether a token issued at iat is revoked by a revoked_before marker.
func IsRevoked(iat time.Time, revokedBefore int64) bool {
	return revokedBefore > 0 && iat.Unix() < revokedBefore
}
