package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
)

const (
	tokenRPMPrefix         = "ts:token:rpm:"
	tokenConcurrencyPrefix = "ts:token:active:"
	tokenRPMWindow         = time.Minute
	tokenConcurrencyTTL    = 15 * time.Minute
)

type tokenRPMMemoryLimiter struct {
	mutex    sync.Mutex
	requests map[int][]time.Time
}

func (limiter *tokenRPMMemoryLimiter) allow(tokenId int, limit int, now time.Time) bool {
	limiter.mutex.Lock()
	defer limiter.mutex.Unlock()

	windowStart := now.Add(-tokenRPMWindow)
	requests := limiter.requests[tokenId]
	firstLive := 0
	for firstLive < len(requests) && !requests[firstLive].After(windowStart) {
		firstLive++
	}
	requests = requests[firstLive:]
	if len(requests) >= limit {
		limiter.requests[tokenId] = requests
		return false
	}
	limiter.requests[tokenId] = append(requests, now)
	return true
}

type tokenConcurrencyMemoryLimiter struct {
	mutex  sync.Mutex
	active map[int]int
}

func (limiter *tokenConcurrencyMemoryLimiter) acquire(tokenId int, limit int) (func(), bool) {
	limiter.mutex.Lock()
	defer limiter.mutex.Unlock()

	if limiter.active[tokenId] >= limit {
		return nil, false
	}
	limiter.active[tokenId]++
	return func() {
		limiter.mutex.Lock()
		defer limiter.mutex.Unlock()
		if limiter.active[tokenId] > 1 {
			limiter.active[tokenId]--
		} else {
			delete(limiter.active, tokenId)
		}
	}, true
}

func TokenRPMLimiter() gin.HandlerFunc {
	memoryLimiter := &tokenRPMMemoryLimiter{requests: make(map[int][]time.Time)}
	return func(c *gin.Context) {
		tokenId := c.GetInt("token_id")
		limit := c.GetInt("rpm_limit")
		if tokenId <= 0 || limit <= 0 {
			c.Next()
			return
		}

		allowed := false
		if common.RedisEnabled && common.RDB != nil {
			redisAllowed, err := allowTokenRPMRedis(c.Request.Context(), tokenId, limit)
			if err == nil {
				allowed = redisAllowed
			} else {
				allowed = memoryLimiter.allow(tokenId, limit, time.Now())
			}
		} else {
			allowed = memoryLimiter.allow(tokenId, limit, time.Now())
		}
		if !allowed {
			logRateLimitedRejection(c, fmt.Sprintf("token_rpm_limit_exceeded max=%d", limit))
			abortWithOpenAiMessage(
				c,
				http.StatusTooManyRequests,
				fmt.Sprintf("token RPM limit exceeded (max=%d)", limit),
				types.ErrorCode("token_rpm_limit_exceeded"),
			)
			return
		}
		c.Next()
	}
}

func allowTokenRPMRedis(ctx context.Context, tokenId int, limit int) (bool, error) {
	key := tokenRPMPrefix + strconv.Itoa(tokenId)
	script := `
		local count = redis.call('INCR', KEYS[1])
		if count == 1 then
			redis.call('PEXPIRE', KEYS[1], ARGV[2])
		end
		if count > tonumber(ARGV[1]) then
			return 0
		end
		return 1
	`
	result, err := common.RDB.Eval(ctx, script, []string{key}, limit, tokenRPMWindow.Milliseconds()).Int()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

func TokenConcurrencyLimiter() gin.HandlerFunc {
	memoryLimiter := &tokenConcurrencyMemoryLimiter{active: make(map[int]int)}
	return func(c *gin.Context) {
		tokenId := c.GetInt("token_id")
		limit := c.GetInt("concurrency_limit")
		if tokenId <= 0 || limit <= 0 {
			c.Next()
			return
		}

		var release func()
		var acquired bool
		if common.RedisEnabled && common.RDB != nil {
			redisRelease, redisAcquired, err := acquireTokenConcurrencyRedis(c.Request.Context(), tokenId, limit)
			if err == nil {
				release = redisRelease
				acquired = redisAcquired
			} else {
				release, acquired = memoryLimiter.acquire(tokenId, limit)
			}
		} else {
			release, acquired = memoryLimiter.acquire(tokenId, limit)
		}
		if !acquired {
			abortWithOpenAiMessage(
				c,
				http.StatusTooManyRequests,
				fmt.Sprintf("token concurrency limit exceeded (max=%d)", limit),
				types.ErrorCode("token_concurrency_limit_exceeded"),
			)
			return
		}
		defer release()
		c.Next()
	}
}

func acquireTokenConcurrencyRedis(ctx context.Context, tokenId int, limit int) (func(), bool, error) {
	key := tokenConcurrencyPrefix + strconv.Itoa(tokenId)
	acquireScript := `
		local count = redis.call('INCR', KEYS[1])
		if count > tonumber(ARGV[1]) then
			redis.call('DECR', KEYS[1])
			return 0
		end
		redis.call('EXPIRE', KEYS[1], ARGV[2])
		return 1
	`
	result, err := common.RDB.Eval(
		ctx,
		acquireScript,
		[]string{key},
		limit,
		int(tokenConcurrencyTTL.Seconds()),
	).Int()
	if err != nil {
		return nil, false, err
	}
	if result != 1 {
		return nil, false, nil
	}
	return func() {
		releaseScript := `
			local count = redis.call('DECR', KEYS[1])
			if count <= 0 then
				redis.call('DEL', KEYS[1])
			end
			return count
		`
		_, _ = common.RDB.Eval(context.Background(), releaseScript, []string{key}).Result()
	}, true, nil
}
