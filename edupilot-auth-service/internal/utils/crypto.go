package utils

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"strconv"

	"golang.org/x/crypto/bcrypt"
)

// BcryptCost returns BCRYPT_COST if valid, else bcrypt.DefaultCost.
func BcryptCost() int {
	if v := os.Getenv("BCRYPT_COST"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed >= bcrypt.MinCost && parsed <= bcrypt.MaxCost {
			return parsed
		}
	}
	return bcrypt.DefaultCost
}

func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), BcryptCost())
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// dummyHash is used to equalize login timing when the user does not exist.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("edupilot-dummy-password"), bcrypt.DefaultCost)

// CheckPassword compares a bcrypt hash with a password. An empty hash always fails
// (but still spends comparable time).
func CheckPassword(hash, password string) bool {
	if hash == "" {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// RandomToken returns a hex string of n crypto-random bytes.
func RandomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// HashToken returns the hex SHA-256 of a token (for storage at rest).
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// GenerateOTP returns a crypto-random 6-digit code in [100000, 999999].
func GenerateOTP() (int, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(900000))
	if err != nil {
		return 0, err
	}
	return int(n.Int64()) + 100000, nil
}

func FormatOTP(otp int) string {
	return fmt.Sprintf("%06d", otp)
}

// SecureCompare is a constant-time string comparison.
func SecureCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
