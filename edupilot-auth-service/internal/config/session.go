package config

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// Session / token state stored in Redis (see API_CONTRACT.md §3).
//
//   auth:revoked_before:user:<id>     unix seconds; tokens with iat < value are rejected
//   refresh_token:<user_id>:<jti>     one key per live refresh token (rotated on use)

func RevokedBeforeKey(userID uint) string {
	return fmt.Sprintf("auth:revoked_before:user:%d", userID)
}

func refreshKey(userID uint, jti string) string {
	return fmt.Sprintf("refresh_token:%d:%s", userID, jti)
}

// SaveRefreshToken stores a refresh token's jti with the token TTL.
func SaveRefreshToken(userID uint, jti string, ttl time.Duration) error {
	return RDB.Set(ctx, refreshKey(userID, jti), "1", ttl).Err()
}

// ConsumeRefreshToken atomically deletes the jti key. It returns true only if
// the key existed, so a refresh token can be used exactly once.
func ConsumeRefreshToken(userID uint, jti string) (bool, error) {
	n, err := RDB.Del(ctx, refreshKey(userID, jti)).Result()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// DeleteAllRefreshTokens removes every refresh token of a user.
func DeleteAllRefreshTokens(userID uint) error {
	pattern := fmt.Sprintf("refresh_token:%d:*", userID)
	iter := RDB.Scan(ctx, 0, pattern, 200).Iterator()
	var keys []string
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
		if len(keys) >= 200 {
			if err := RDB.Del(ctx, keys...).Err(); err != nil {
				return err
			}
			keys = keys[:0]
		}
	}
	if err := iter.Err(); err != nil {
		return err
	}
	if len(keys) > 0 {
		return RDB.Del(ctx, keys...).Err()
	}
	return nil
}

// RevokeUserSessions invalidates all tokens issued before now and deletes all refresh tokens.
// keyTTL should be >= the longest token lifetime.
func RevokeUserSessions(userID uint, keyTTL time.Duration) error {
	if err := RDB.Set(ctx, RevokedBeforeKey(userID), time.Now().Unix(), keyTTL).Err(); err != nil {
		return err
	}
	return DeleteAllRefreshTokens(userID)
}

// GetRevokedBefore returns the revocation timestamp (0 if none).
func GetRevokedBefore(userID uint) (int64, error) {
	val, err := RDB.Get(ctx, RevokedBeforeKey(userID)).Result()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(val, 10, 64)
}
