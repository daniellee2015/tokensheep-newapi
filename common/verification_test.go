package common

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupVerificationRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client, *redis.Client) {
	t.Helper()
	server := miniredis.RunT(t)
	first := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	second := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	previousClient, previousEnabled := RDB, RedisEnabled
	previousMap, previousMinutes := verificationMap, VerificationValidMinutes
	RDB, RedisEnabled = first, true
	verificationMap = make(map[string]verificationValue)
	VerificationValidMinutes = 10
	t.Cleanup(func() {
		require.NoError(t, first.Close())
		require.NoError(t, second.Close())
		RDB, RedisEnabled = previousClient, previousEnabled
		verificationMap, VerificationValidMinutes = previousMap, previousMinutes
	})
	return server, first, second
}

func TestVerificationCodeSurvivesDifferentInstance(t *testing.T) {
	_, _, second := setupVerificationRedis(t)
	require.NoError(t, RegisterVerificationCodeWithKey("person@example.com", "abc123", EmailVerificationPurpose))
	// Another instance has its own Redis connection and no process-local codes.
	RDB = second
	verificationMap = make(map[string]verificationValue)
	assert.True(t, VerifyCodeWithKey("person@example.com", "abc123", EmailVerificationPurpose))
	assert.False(t, VerifyCodeWithKey("person@example.com", "wrong", EmailVerificationPurpose))
	assert.False(t, VerifyCodeWithKey("other@example.com", "abc123", EmailVerificationPurpose))
	assert.False(t, VerifyCodeWithKey("person@example.com", "abc123", PasswordResetPurpose))
}

func TestVerificationCodeReplacementExpirationAndDeletion(t *testing.T) {
	server, first, second := setupVerificationRedis(t)
	require.NoError(t, RegisterVerificationCodeWithKey("person@example.com", "old", EmailVerificationPurpose))
	require.NoError(t, RegisterVerificationCodeWithKey("person@example.com", "reset", PasswordResetPurpose))
	RDB = second
	require.NoError(t, RegisterVerificationCodeWithKey("person@example.com", "new", EmailVerificationPurpose))
	RDB = first
	assert.False(t, VerifyCodeWithKey("person@example.com", "old", EmailVerificationPurpose))
	assert.True(t, VerifyCodeWithKey("person@example.com", "new", EmailVerificationPurpose))
	assert.True(t, VerifyCodeWithKey("person@example.com", "reset", PasswordResetPurpose))

	RDB = second
	DeleteKey("person@example.com", PasswordResetPurpose)
	RDB = first
	assert.False(t, VerifyCodeWithKey("person@example.com", "reset", PasswordResetPurpose))
	assert.True(t, VerifyCodeWithKey("person@example.com", "new", EmailVerificationPurpose))
	server.FastForward(10 * time.Minute)
	assert.False(t, VerifyCodeWithKey("person@example.com", "new", EmailVerificationPurpose))
}

func TestVerificationRedisFailureDoesNotUseLocalCodes(t *testing.T) {
	server, _, _ := setupVerificationRedis(t)
	RedisEnabled = false
	require.NoError(t, RegisterVerificationCodeWithKey("person@example.com", "local", EmailVerificationPurpose))
	RedisEnabled = true
	server.SetError("ERR unavailable")
	require.Error(t, RegisterVerificationCodeWithKey("person@example.com", "new", EmailVerificationPurpose))
	assert.False(t, VerifyCodeWithKey("person@example.com", "local", EmailVerificationPurpose))
	server.SetError("")
	assert.False(t, VerifyCodeWithKey("person@example.com", "new", EmailVerificationPurpose))

	RDB = nil
	require.Error(t, RegisterVerificationCodeWithKey("person@example.com", "new", EmailVerificationPurpose))
	assert.False(t, VerifyCodeWithKey("person@example.com", "local", EmailVerificationPurpose))
}

func TestVerificationCodeWithoutRedis(t *testing.T) {
	setupVerificationRedis(t)
	RedisEnabled, RDB = false, nil
	require.NoError(t, RegisterVerificationCodeWithKey("person@example.com", "local", EmailVerificationPurpose))
	assert.True(t, VerifyCodeWithKey("person@example.com", "local", EmailVerificationPurpose))
	assert.False(t, VerifyCodeWithKey("person@example.com", "wrong", EmailVerificationPurpose))
	DeleteKey("person@example.com", EmailVerificationPurpose)
	assert.False(t, VerifyCodeWithKey("person@example.com", "local", EmailVerificationPurpose))
}
