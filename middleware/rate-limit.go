package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

const redisRateLimitNamespace = "rateLimit:v2"

// Redis rate limiting intentionally uses a fixed window. The single Lua script
// makes increment, expiry, and the limit decision atomic, while retaining the
// simple fixed-window behavior: traffic at a window boundary can burst up to
// twice the configured limit. Do not replace this with a sliding-window ZSET
// unless that externally visible behavior is intentionally changed.
const redisFixedWindowScript = `
local count = redis.call('INCR', KEYS[1])
if count == 1 then
  redis.call('EXPIRE', KEYS[1], ARGV[2])
end
local ttl = redis.call('TTL', KEYS[1])
if ttl < 0 then
  redis.call('EXPIRE', KEYS[1], ARGV[2])
  ttl = redis.call('TTL', KEYS[1])
end
if count > tonumber(ARGV[1]) then
  return {0, count, ttl}
end
return {1, count, ttl}
`

var inMemoryRateLimiter common.InMemoryRateLimiter

var defNext = func(c *gin.Context) {
	c.Next()
}

func redisIPRateLimitKey(mark string, clientIP string) string {
	return fmt.Sprintf("%s:ip:%s:%s", redisRateLimitNamespace, mark, clientIP)
}

func redisUserRateLimitKey(mark string, userID int) string {
	return fmt.Sprintf("%s:user:%s:%d", redisRateLimitNamespace, mark, userID)
}

func redisReplyInteger(value interface{}) (int64, error) {
	switch typed := value.(type) {
	case int64:
		return typed, nil
	case string:
		return strconv.ParseInt(typed, 10, 64)
	case []byte:
		return strconv.ParseInt(string(typed), 10, 64)
	default:
		return 0, fmt.Errorf("unexpected Redis integer reply type %T", value)
	}
}

func redisFixedWindowTake(ctx context.Context, key string, maxRequestNum int, duration int64) (bool, int64, int64, error) {
	if common.RDB == nil {
		return false, 0, 0, errors.New("Redis client is not initialized")
	}
	if key == "" {
		return false, 0, 0, errors.New("rate limit key is empty")
	}
	if maxRequestNum <= 0 {
		return false, 0, 0, errors.New("rate limit maximum must be positive")
	}
	if duration <= 0 {
		return false, 0, 0, errors.New("rate limit duration must be positive")
	}

	values, err := common.RDB.Eval(
		ctx,
		redisFixedWindowScript,
		[]string{key},
		maxRequestNum,
		duration,
	).Slice()
	if err != nil {
		return false, 0, 0, err
	}
	if len(values) != 3 {
		return false, 0, 0, fmt.Errorf("unexpected Redis rate limit reply length %d", len(values))
	}

	allowedValue, err := redisReplyInteger(values[0])
	if err != nil {
		return false, 0, 0, err
	}
	count, err := redisReplyInteger(values[1])
	if err != nil {
		return false, 0, 0, err
	}
	ttlSeconds, err := redisReplyInteger(values[2])
	if err != nil {
		return false, 0, 0, err
	}

	return allowedValue == 1, count, ttlSeconds, nil
}

func redisRateLimiter(c *gin.Context, maxRequestNum int, duration int64, mark string) {
	allowed, _, ttlSeconds, err := redisFixedWindowTake(
		c.Request.Context(),
		redisIPRateLimitKey(mark, c.ClientIP()),
		maxRequestNum,
		duration,
	)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("rate limit check failed (mark=%s): %v", mark, err))
		c.Status(http.StatusInternalServerError)
		c.Abort()
		return
	}
	if !allowed {
		writeRateLimited(c, ttlSeconds)
	}
}

func memoryRateLimiter(c *gin.Context, maxRequestNum int, duration int64, mark string) {
	key := mark + c.ClientIP()
	if !inMemoryRateLimiter.Request(key, maxRequestNum, duration) {
		writeRateLimited(c, duration)
		return
	}
}

// writeRateLimited rejects the request with 429 and a Retry-After hint so
// clients can back off instead of treating the rejection as a fatal error.
// The in-memory limiter cannot report the remaining window, so callers
// without a TTL pass the full window duration as a conservative upper bound.
func writeRateLimited(c *gin.Context, retryAfterSeconds int64) {
	if retryAfterSeconds > 0 {
		c.Header("Retry-After", strconv.FormatInt(retryAfterSeconds, 10))
	}
	c.Status(http.StatusTooManyRequests)
	c.Abort()
}

func rateLimitFactory(maxRequestNum int, duration int64, mark string) func(c *gin.Context) {
	if common.RedisEnabled {
		return func(c *gin.Context) {
			redisRateLimiter(c, maxRequestNum, duration, mark)
		}
	}
	// It's safe to call multi times.
	inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
	return func(c *gin.Context) {
		memoryRateLimiter(c, maxRequestNum, duration, mark)
	}
}

func GlobalWebRateLimit() func(c *gin.Context) {
	if common.GlobalWebRateLimitEnable {
		return rateLimitFactory(common.GlobalWebRateLimitNum, common.GlobalWebRateLimitDuration, "GW")
	}
	return defNext
}

func GlobalAPIRateLimit() func(c *gin.Context) {
	if !common.GlobalApiRateLimitEnable {
		return defNext
	}
	return hybridGlobalAPIRateLimit
}

// hybridGlobalAPIRateLimit 是 GlobalAPIRateLimit 的实际实现。
// 优先按已登录用户分桶: 从 Authorization Bearer 里 unverified 地解出 userID
// (只用来 keying, 不承担安全语义), 让同一账号在多 tab / 多设备 / IPv4↔IPv6
// 切换时不再互相挤压额度。解不出就回落到 IP-based, 阈值和窗口保持一致。
//
// 这一层在 UserAuth 之前跑, 所以不能依赖 c.GetInt("id"); 真正的鉴权由
// 后续中间件完成, 这里的 userID 仅作为限流 bucket key。
func hybridGlobalAPIRateLimit(c *gin.Context) {
	num := common.GlobalApiRateLimitNum
	duration := common.GlobalApiRateLimitDuration

	if userID, ok := peekBearerUserID(c); ok {
		if common.RedisEnabled {
			userRedisRateLimiter(c, num, duration, redisUserRateLimitKey("GA", userID))
			return
		}
		inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
		key := fmt.Sprintf("GA:user:%d", userID)
		if !inMemoryRateLimiter.Request(key, num, duration) {
			writeRateLimited(c, duration)
		}
		return
	}

	// 未登录或非 dashboard token: 沿用原来的 IP 桶, 阈值不变。
	if common.RedisEnabled {
		redisRateLimiter(c, num, duration, "GA")
		return
	}
	inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
	memoryRateLimiter(c, num, duration, "GA")
}

// peekBearerUserID 从请求头里嗅出 dashboard 用户 ID, 只用于限流 key。
// 严格拒绝非 dashboard JWT (relay token / PAT), 避免误把大流量的
// API key 请求全部塞进同一个用户桶。
func peekBearerUserID(c *gin.Context) (int, bool) {
	raw := c.GetHeader("Authorization")
	if raw == "" {
		return 0, false
	}
	parts := strings.Fields(raw)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return 0, false
	}
	return service.PeekAccessTokenUserID(parts[1])
}

func CriticalRateLimit() func(c *gin.Context) {
	if common.CriticalRateLimitEnable {
		return rateLimitFactory(common.CriticalRateLimitNum, common.CriticalRateLimitDuration, "CT")
	}
	return defNext
}

// RefreshRateLimit 专用于 /api/user/auth/refresh。
// 以 sid 为限流 key: 同一浏览器 session 无论 IP 怎么切都是同一个 sid,
// 多 tab / IPv4↔IPv6 切换不再误触发 429。拿不到 sid (cookie 缺失/损坏)
// 回落到 IP-based 保底 bucket, 防止恶意刷 refresh 端点。
//
// 依赖: service.RefreshTokenSID (纯字符串拆分, 不校验签名, 因此安全前置于
// RefreshLoginSession 的哈希校验)。使用前提是 SessionCookieOriginGuard
// 已在同一路由链上, 阻止 cross-site 的 sid 冒用。
func RefreshRateLimit() func(c *gin.Context) {
	if !common.RefreshRateLimitEnable {
		return defNext
	}
	return refreshRateLimiter
}

func refreshRateLimiter(c *gin.Context) {
	sid := extractRefreshSID(c)
	mark := "RF"
	num := common.RefreshRateLimitNum
	duration := common.RefreshRateLimitDuration

	if sid == "" {
		// 无 sid 走 IP 保底桶, 阈值不变但 mark 独立, 不再污染 CT
		if common.RedisEnabled {
			redisRateLimiter(c, num, duration, mark+":ip")
			return
		}
		inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
		memoryRateLimiter(c, num, duration, mark+":ip")
		return
	}

	if common.RedisEnabled {
		key := fmt.Sprintf("%s:sid:%s:%s", redisRateLimitNamespace, mark, sid)
		allowed, _, ttlSeconds, err := redisFixedWindowTake(c.Request.Context(), key, num, duration)
		if err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("refresh rate limit check failed: %v", err))
			c.Status(http.StatusInternalServerError)
			c.Abort()
			return
		}
		if !allowed {
			writeRateLimited(c, ttlSeconds)
		}
		return
	}

	inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
	key := fmt.Sprintf("%s:sid:%s", mark, sid)
	if !inMemoryRateLimiter.Request(key, num, duration) {
		writeRateLimited(c, duration)
	}
}

// extractRefreshSID 从 refresh cookie 或 X-Auth-Session header 里推 sid,
// 都拿不到返回空串。写法上不做 hex/base64 校验, 因为限流 key 只需要稳定,
// 不需要"合法"; 后续 RefreshLoginSession 会做真正的校验。
func extractRefreshSID(c *gin.Context) string {
	if raw, err := c.Cookie(service.RefreshCookieName); err == nil && raw != "" {
		if sid, ok := service.RefreshTokenSID(raw); ok {
			return sid
		}
	}
	if h := c.GetHeader("X-Auth-Session"); h != "" {
		return h
	}
	return ""
}

func UserCriticalRateLimit(scope string) func(c *gin.Context) {
	if !common.CriticalRateLimitEnable {
		return defNext
	}
	return userRateLimitFactory(
		common.CriticalRateLimitNum,
		common.CriticalRateLimitDuration,
		"UC:"+scope,
	)
}

func DownloadRateLimit() func(c *gin.Context) {
	return rateLimitFactory(common.DownloadRateLimitNum, common.DownloadRateLimitDuration, "DW")
}

func UploadRateLimit() func(c *gin.Context) {
	return rateLimitFactory(common.UploadRateLimitNum, common.UploadRateLimitDuration, "UP")
}

// userRateLimitFactory creates a rate limiter keyed by authenticated user ID
// instead of client IP, making it resistant to proxy rotation attacks.
// Must be used AFTER authentication middleware (UserAuth).
func userRateLimitFactory(maxRequestNum int, duration int64, mark string) func(c *gin.Context) {
	if common.RedisEnabled {
		return func(c *gin.Context) {
			userID := c.GetInt("id")
			if userID == 0 {
				c.Status(http.StatusUnauthorized)
				c.Abort()
				return
			}
			userRedisRateLimiter(c, maxRequestNum, duration, redisUserRateLimitKey(mark, userID))
		}
	}
	// It's safe to call multi times.
	inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
	return func(c *gin.Context) {
		userID := c.GetInt("id")
		if userID == 0 {
			c.Status(http.StatusUnauthorized)
			c.Abort()
			return
		}
		key := fmt.Sprintf("%s:user:%d", mark, userID)
		if !inMemoryRateLimiter.Request(key, maxRequestNum, duration) {
			writeRateLimited(c, duration)
			return
		}
	}
}

// userRedisRateLimiter is like redisRateLimiter but accepts a pre-built key
// (to support user-ID-based keys).
func userRedisRateLimiter(c *gin.Context, maxRequestNum int, duration int64, key string) {
	allowed, _, ttlSeconds, err := redisFixedWindowTake(c.Request.Context(), key, maxRequestNum, duration)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("rate limit check failed (key=%s): %v", key, err))
		c.Status(http.StatusInternalServerError)
		c.Abort()
		return
	}
	if !allowed {
		writeRateLimited(c, ttlSeconds)
	}
}

// SearchRateLimit returns a per-user rate limiter for search endpoints.
// Configurable via SEARCH_RATE_LIMIT_ENABLE / SEARCH_RATE_LIMIT / SEARCH_RATE_LIMIT_DURATION.
func SearchRateLimit() func(c *gin.Context) {
	if !common.SearchRateLimitEnable {
		return defNext
	}
	return userRateLimitFactory(common.SearchRateLimitNum, common.SearchRateLimitDuration, "SR")
}
