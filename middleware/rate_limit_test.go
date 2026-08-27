package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func useRateLimitMiniRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()

	previousRedisEnabled := common.RedisEnabled
	previousRedisClient := common.RDB
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	require.NoError(t, redisClient.Ping(context.Background()).Err())

	common.RedisEnabled = true
	common.RDB = redisClient
	t.Cleanup(func() {
		_ = redisClient.Close()
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRedisClient
	})

	return redisServer, redisClient
}

func performRateLimitRequest(router http.Handler, path string, remoteAddr string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.RemoteAddr = remoteAddr
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestRedisIPRateLimiterThresholdTTLAndNamespace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	redisServer, _ := useRateLimitMiniRedis(t)

	router := gin.New()
	require.NoError(t, router.SetTrustedProxies(nil))
	router.GET("/limited", rateLimitFactory(2, 37, "TEST"), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	remoteAddr := "192.0.2.10:12345"
	legacyKey := "rateLimit:TEST192.0.2.10"
	_, err := redisServer.Push(legacyKey, "legacy-list-entry")
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, performRateLimitRequest(router, "/limited", remoteAddr).Code)
	assert.Equal(t, http.StatusNoContent, performRateLimitRequest(router, "/limited", remoteAddr).Code)
	limitedResponse := performRateLimitRequest(router, "/limited", remoteAddr)
	assert.Equal(t, http.StatusTooManyRequests, limitedResponse.Code)
	assert.Equal(t, "37", limitedResponse.Header().Get("Retry-After"))

	key := redisIPRateLimitKey("TEST", "192.0.2.10")
	count, err := redisServer.Get(key)
	require.NoError(t, err)
	assert.Equal(t, "3", count)
	assert.Equal(t, 37*time.Second, redisServer.TTL(key))
	assert.True(t, redisServer.Exists(legacyKey), "the v2 counter must not touch an old list key")
}

func TestRedisUserRateLimiterUsesSharedFixedWindow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	redisServer, _ := useRateLimitMiniRedis(t)

	router := gin.New()
	router.GET(
		"/limited",
		func(c *gin.Context) { c.Set("id", 42) },
		userRateLimitFactory(1, 23, "USER"),
		func(c *gin.Context) { c.Status(http.StatusNoContent) },
	)

	assert.Equal(t, http.StatusNoContent, performRateLimitRequest(router, "/limited", "192.0.2.20:12345").Code)
	assert.Equal(t, http.StatusTooManyRequests, performRateLimitRequest(router, "/limited", "198.51.100.20:12345").Code)

	key := redisUserRateLimitKey("USER", 42)
	assert.True(t, redisServer.Exists(key))
	assert.Equal(t, 23*time.Second, redisServer.TTL(key))
}

func TestRedisEmailVerificationRateLimiterPreservesResponseAndTTL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	redisServer, _ := useRateLimitMiniRedis(t)

	router := gin.New()
	require.NoError(t, router.SetTrustedProxies(nil))
	router.GET("/verify", EmailVerificationRateLimit(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	remoteAddr := "192.0.2.30:12345"
	assert.Equal(t, http.StatusNoContent, performRateLimitRequest(router, "/verify", remoteAddr).Code)
	assert.Equal(t, http.StatusNoContent, performRateLimitRequest(router, "/verify", remoteAddr).Code)
	response := performRateLimitRequest(router, "/verify", remoteAddr)
	assert.Equal(t, http.StatusTooManyRequests, response.Code)
	assert.JSONEq(t, `{"success":false,"message":"发送过于频繁，请等待 30 秒后再试"}`, response.Body.String())

	key := redisIPRateLimitKey(EmailVerificationRateLimitMark, "192.0.2.30")
	assert.True(t, redisServer.Exists(key))
	assert.Equal(t, time.Duration(EmailVerificationDuration)*time.Second, redisServer.TTL(key))
}

func TestRedisFixedWindowIsAtomicUnderConcurrency(t *testing.T) {
	redisServer, _ := useRateLimitMiniRedis(t)
	const (
		requestCount = 20
		maximumCount = 7
		duration     = int64(41)
	)
	key := redisIPRateLimitKey("CONCURRENT", "192.0.2.40")

	var allowedCount atomic.Int64
	errorsFound := make(chan error, requestCount)
	var waitGroup sync.WaitGroup
	waitGroup.Add(requestCount)
	for range requestCount {
		go func() {
			defer waitGroup.Done()
			allowed, _, _, err := redisFixedWindowTake(context.Background(), key, maximumCount, duration)
			if err != nil {
				errorsFound <- err
				return
			}
			if allowed {
				allowedCount.Add(1)
			}
		}()
	}
	waitGroup.Wait()
	close(errorsFound)
	for err := range errorsFound {
		require.NoError(t, err)
	}

	assert.Equal(t, int64(maximumCount), allowedCount.Load())
	count, err := redisServer.Get(key)
	require.NoError(t, err)
	assert.Equal(t, "20", count)
	assert.Equal(t, time.Duration(duration)*time.Second, redisServer.TTL(key))
}

func TestRedisFixedWindowResetsAtBoundary(t *testing.T) {
	redisServer, _ := useRateLimitMiniRedis(t)
	const duration = int64(10)
	key := redisIPRateLimitKey("BOUNDARY", "192.0.2.50")

	for range 2 {
		allowed, _, _, err := redisFixedWindowTake(context.Background(), key, 2, duration)
		require.NoError(t, err)
		assert.True(t, allowed)
	}
	allowed, _, _, err := redisFixedWindowTake(context.Background(), key, 2, duration)
	require.NoError(t, err)
	assert.False(t, allowed)

	// This reset is intentional fixed-window behavior. A client can consume one
	// full allowance immediately before and another immediately after a boundary.
	redisServer.FastForward(time.Duration(duration) * time.Second)
	for range 2 {
		allowed, _, _, err = redisFixedWindowTake(context.Background(), key, 2, duration)
		require.NoError(t, err)
		assert.True(t, allowed)
	}
}

func TestRedisFixedWindowRepairsCounterWithoutTTL(t *testing.T) {
	redisServer, _ := useRateLimitMiniRedis(t)
	const duration = int64(29)
	key := redisIPRateLimitKey("MISSING-TTL", "192.0.2.51")
	redisServer.Set(key, "5")

	allowed, count, ttl, err := redisFixedWindowTake(context.Background(), key, 3, duration)
	require.NoError(t, err)
	assert.False(t, allowed)
	assert.Equal(t, int64(6), count)
	assert.Equal(t, duration, ttl)
	assert.Equal(t, time.Duration(duration)*time.Second, redisServer.TTL(key))

	redisServer.FastForward(time.Duration(duration) * time.Second)
	assert.False(t, redisServer.Exists(key), "a recovered counter must not remain permanently rate-limited")
}

func TestRefreshRateLimitBucketsBySID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	redisServer, _ := useRateLimitMiniRedis(t)

	prevEnable := common.RefreshRateLimitEnable
	prevNum := common.RefreshRateLimitNum
	prevDur := common.RefreshRateLimitDuration
	common.RefreshRateLimitEnable = true
	common.RefreshRateLimitNum = 2
	common.RefreshRateLimitDuration = 30
	t.Cleanup(func() {
		common.RefreshRateLimitEnable = prevEnable
		common.RefreshRateLimitNum = prevNum
		common.RefreshRateLimitDuration = prevDur
	})

	router := gin.New()
	require.NoError(t, router.SetTrustedProxies(nil))
	router.POST("/auth/refresh", RefreshRateLimit(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	do := func(sid, remoteAddr string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
		req.RemoteAddr = remoteAddr
		if sid != "" {
			req.Header.Set("X-Auth-Session", sid)
		}
		router.ServeHTTP(rec, req)
		return rec
	}

	// sid=A 的两次通过, 第三次 429
	assert.Equal(t, http.StatusNoContent, do("sid-A", "192.0.2.100:1").Code)
	assert.Equal(t, http.StatusNoContent, do("sid-A", "192.0.2.100:1").Code)
	assert.Equal(t, http.StatusTooManyRequests, do("sid-A", "192.0.2.100:1").Code)

	// sid=B 独立计数, 即使同一 IP 也不受 sid-A 的桶影响
	assert.Equal(t, http.StatusNoContent, do("sid-B", "192.0.2.100:1").Code)

	// 无 sid 走 IP 回落桶, 跟 sid 桶隔离
	assert.Equal(t, http.StatusNoContent, do("", "192.0.2.101:1").Code)
	assert.Equal(t, http.StatusNoContent, do("", "192.0.2.101:1").Code)
	assert.Equal(t, http.StatusTooManyRequests, do("", "192.0.2.101:1").Code)

	assert.True(t, redisServer.Exists("rateLimit:v2:sid:RF:sid-A"))
	assert.True(t, redisServer.Exists("rateLimit:v2:sid:RF:sid-B"))
	assert.True(t, redisServer.Exists("rateLimit:v2:ip:RF:ip:192.0.2.101"))
}

func TestRefreshRateLimitDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)

	prevEnable := common.RefreshRateLimitEnable
	common.RefreshRateLimitEnable = false
	t.Cleanup(func() { common.RefreshRateLimitEnable = prevEnable })

	router := gin.New()
	require.NoError(t, router.SetTrustedProxies(nil))
	router.POST("/auth/refresh", RefreshRateLimit(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	for i := 0; i < 100; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
		req.Header.Set("X-Auth-Session", "sid-any")
		req.RemoteAddr = "192.0.2.200:1"
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusNoContent, rec.Code)
	}
}

func TestGlobalAPIRateLimitBucketsByUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	redisServer, _ := useRateLimitMiniRedis(t)

	prevEnable := common.GlobalApiRateLimitEnable
	prevNum := common.GlobalApiRateLimitNum
	prevDur := common.GlobalApiRateLimitDuration
	common.GlobalApiRateLimitEnable = true
	common.GlobalApiRateLimitNum = 2
	common.GlobalApiRateLimitDuration = 30
	t.Cleanup(func() {
		common.GlobalApiRateLimitEnable = prevEnable
		common.GlobalApiRateLimitNum = prevNum
		common.GlobalApiRateLimitDuration = prevDur
	})

	router := gin.New()
	require.NoError(t, router.SetTrustedProxies(nil))
	router.GET("/api/x", GlobalAPIRateLimit(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	issueToken := func(t *testing.T, userID int, sid string) string {
		t.Helper()
		token, _, err := service.IssueAccessToken(service.AuthIdentity{
			UserID:          userID,
			SessionID:       sid,
			UserAuthVersion: 1,
			SessionVersion:  1,
		})
		require.NoError(t, err)
		return token
	}
	do := func(bearer, ip string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
		req.RemoteAddr = ip + ":1"
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		router.ServeHTTP(rec, req)
		return rec
	}

	tokenA := issueToken(t, 42, "sid-a")
	tokenB := issueToken(t, 43, "sid-b")

	// 用户 42 消耗 2 次后触发 429, 换 IP 也不影响 (证明按 user 分桶)
	assert.Equal(t, http.StatusNoContent, do(tokenA, "192.0.2.10").Code)
	assert.Equal(t, http.StatusNoContent, do(tokenA, "192.0.2.11").Code)
	assert.Equal(t, http.StatusTooManyRequests, do(tokenA, "192.0.2.12").Code)

	// 用户 43 独立桶, 仍能通过
	assert.Equal(t, http.StatusNoContent, do(tokenB, "192.0.2.10").Code)

	// 未带 Authorization 走 IP 桶, 跟用户桶隔离
	assert.Equal(t, http.StatusNoContent, do("", "192.0.2.20").Code)
	assert.Equal(t, http.StatusNoContent, do("", "192.0.2.20").Code)
	assert.Equal(t, http.StatusTooManyRequests, do("", "192.0.2.20").Code)

	// Bearer 但不是 dashboard token 时回落 IP (非 dashboard issuer 的 JWT
	// 或不透明 PAT 都不能污染用户桶)
	assert.True(t, redisServer.Exists(redisUserRateLimitKey("GA", 42)))
	assert.True(t, redisServer.Exists(redisUserRateLimitKey("GA", 43)))
	assert.True(t, redisServer.Exists(redisIPRateLimitKey("GA", "192.0.2.20")))
}

func TestGlobalAPIRateLimitIgnoresNonDashboardToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	redisServer, _ := useRateLimitMiniRedis(t)

	prevEnable := common.GlobalApiRateLimitEnable
	prevNum := common.GlobalApiRateLimitNum
	prevDur := common.GlobalApiRateLimitDuration
	common.GlobalApiRateLimitEnable = true
	common.GlobalApiRateLimitNum = 2
	common.GlobalApiRateLimitDuration = 30
	t.Cleanup(func() {
		common.GlobalApiRateLimitEnable = prevEnable
		common.GlobalApiRateLimitNum = prevNum
		common.GlobalApiRateLimitDuration = prevDur
	})

	router := gin.New()
	require.NoError(t, router.SetTrustedProxies(nil))
	router.GET("/api/x", GlobalAPIRateLimit(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	// 不透明 PAT 字符串, 完全不是 JWT
	do := func(bearer, ip string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
		req.RemoteAddr = ip + ":1"
		req.Header.Set("Authorization", "Bearer "+bearer)
		router.ServeHTTP(rec, req)
		return rec
	}
	assert.Equal(t, http.StatusNoContent, do("sk-not-a-jwt-just-opaque", "192.0.2.30").Code)
	assert.Equal(t, http.StatusNoContent, do("sk-not-a-jwt-just-opaque", "192.0.2.30").Code)
	assert.Equal(t, http.StatusTooManyRequests, do("sk-not-a-jwt-just-opaque", "192.0.2.30").Code)

	// 全部计到 IP 桶, 没有创建任何 user 桶
	assert.True(t, redisServer.Exists(redisIPRateLimitKey("GA", "192.0.2.30")))
}

func TestRedisFailurePolicies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, redisClient := useRateLimitMiniRedis(t)
	require.NoError(t, redisClient.Close())

	router := gin.New()
	require.NoError(t, router.SetTrustedProxies(nil))
	router.GET("/ip", rateLimitFactory(1, 30, "FAIL-IP"), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	router.GET(
		"/user",
		func(c *gin.Context) { c.Set("id", 7) },
		userRateLimitFactory(1, 30, "FAIL-USER"),
		func(c *gin.Context) { c.Status(http.StatusNoContent) },
	)
	router.GET("/email", EmailVerificationRateLimit(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	ipResponse := performRateLimitRequest(router, "/ip", "192.0.2.60:12345")
	assert.Equal(t, http.StatusInternalServerError, ipResponse.Code)
	assert.Empty(t, ipResponse.Body.String())
	userResponse := performRateLimitRequest(router, "/user", "192.0.2.61:12345")
	assert.Equal(t, http.StatusInternalServerError, userResponse.Code)
	assert.Empty(t, userResponse.Body.String())
	assert.Equal(t, http.StatusNoContent, performRateLimitRequest(router, "/email", "192.0.2.62:12345").Code)
}
