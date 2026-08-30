// Session concurrency middleware — TokenSheep tier-driven simultaneous
// in-flight request cap.
//
// Purpose:
//   RPM is a rate over a rolling window; it can't stop a single user from
//   spawning 50 concurrent long-running streams. Session-concurrency is the
//   orthogonal knob: how many requests may be *simultaneously in progress*
//   for a single user_id, across all their API keys.
//
// Design:
//   - Redis INCR + expiring key holds the current live-request count per user.
//   - We bump on entry, defer a decrement so the count *always* returns to
//     zero even when the handler panics, times out, or the client drops the
//     stream mid-flight.
//   - If the pre-bump count would exceed the tier limit, we immediately
//     rollback the INCR and return 429 with a meaningful message.
//   - Redis-less deployments fall back to an in-memory map with a mutex —
//     single-node only, but at least keeps the concept working during tests.
//
// See docs/spec/economy-model.md and setting/tokensheep_setting/economy.go
// for the tier -> limit mapping (which is admin-editable at runtime).
package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/tokensheep_setting"

	"github.com/gin-gonic/gin"
)

const (
	sessionCounterPrefix = "ts:session:active:" // ts:session:active:<user_id>
	// systemCounterKey is the single station-wide in-flight counter
	// (concurrency layer 3). No user suffix — every authenticated request
	// increments the same key.
	systemCounterKey = "ts:system:active"
	// TTL protects against orphaned counters if a Go panic slips past the
	// deferred decrement (shouldn't happen, but Redis-side belt-and-braces).
	sessionCounterTTL = 15 * time.Minute
)

// SessionConcurrencyLimiter returns a gin middleware that enforces the
// TokenSheep per-user session-concurrency ceiling. Attach after Auth so
// c.GetInt("id") is populated.
//
// The middleware is a no-op when:
//   - user has no user id (unauthenticated),
//   - user's tier isn't listed in SessionLimits,
//   - or the configured limit is zero (== no ceiling).
func SessionConcurrencyLimiter() gin.HandlerFunc {
	// In-memory fallback for deployments without Redis. Keys are user_id
	// strings (plus the systemCounterKey sentinel); values are the current
	// live count.
	var (
		fallbackMu sync.Mutex
		fallback   = make(map[string]int)
	)

	return func(c *gin.Context) {
		userId := c.GetInt("id")
		if userId == 0 {
			c.Next()
			return
		}

		ctx := c.Request.Context()

		// ── Layer 3: station-wide ceiling ───────────────────────────────
		// Checked first and independently of the user's group so it still
		// applies to accounts whose tier has no SessionLimits entry (which
		// short-circuits the per-user gate below). This is the backstop for
		// a misconfigured group cap: if SessionLimits["wholesale-plus"] is
		// fat-fingered from 100 to 100000, this is what stops one account
		// from saturating the upstream pool.
		//
		// A zero setting disables the gate, so deployments that never set
		// the option are unaffected.
		if systemLimit := tokensheep_setting.GetSystemConcurrency(); systemLimit > 0 {
			var (
				releaseSystem func()
				systemOK      bool
			)
			if common.RedisEnabled && common.RDB != nil {
				releaseSystem, systemOK = acquireRedis(ctx, systemCounterKey, systemLimit)
			} else {
				releaseSystem, systemOK = acquireMemory(&fallbackMu, fallback, systemCounterKey, systemLimit)
			}
			if !systemOK {
				// 503 rather than 429: this is station saturation, not a
				// per-account quota problem, and the distinction matters to
				// clients deciding whether an immediate retry is sensible.
				c.JSON(http.StatusServiceUnavailable, gin.H{
					"error": gin.H{
						"message": fmt.Sprintf(
							"station is at capacity (%d concurrent requests in flight). "+
								"Retry shortly.",
							systemLimit,
						),
						"type": "system_busy",
						"code": "system_concurrency_limit_exceeded",
					},
				})
				c.Abort()
				return
			}
			defer releaseSystem()
		}

		// ── Layer 2: per-user-group ceiling ─────────────────────────────
		group, _ := c.Get("group")
		groupStr, _ := group.(string)
		if groupStr == "" {
			c.Next()
			return
		}

		limit := tokensheep_setting.SessionLimit(groupStr)
		if limit <= 0 {
			c.Next()
			return
		}

		key := sessionCounterPrefix + strconv.Itoa(userId)

		var release func()
		var ok bool
		if common.RedisEnabled && common.RDB != nil {
			release, ok = acquireRedis(ctx, key, limit)
		} else {
			release, ok = acquireMemory(&fallbackMu, fallback, key, limit)
		}

		if !ok {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": gin.H{
					"message": fmt.Sprintf(
						"session concurrency limit reached (tier=%s, max=%d). "+
							"Close some open streams or upgrade your tier.",
						groupStr, limit,
					),
					"type": "session_limit_exceeded",
					"code": "session_limit_exceeded",
				},
			})
			c.Abort()
			return
		}
		defer release()

		c.Next()
	}
}

// acquireRedis atomically bumps the per-user counter with a Lua script:
// return true if the new value is <= limit, otherwise rollback and refuse.
// The returned release() decrements the counter (min 0).
func acquireRedis(ctx context.Context, key string, limit int) (func(), bool) {
	// Lua so INCR/GET/rollback happen atomically. Also refreshes the TTL on
	// every successful acquire so a live user's counter can't be reaped
	// mid-flight.
	acquireScript := `
		local n = redis.call('INCR', KEYS[1])
		if n > tonumber(ARGV[1]) then
			redis.call('DECR', KEYS[1])
			return 0
		end
		redis.call('EXPIRE', KEYS[1], ARGV[2])
		return 1
	`
	res, err := common.RDB.Eval(ctx, acquireScript, []string{key},
		limit, int(sessionCounterTTL.Seconds())).Result()
	if err != nil {
		// Fail-open on Redis errors: better to let the request through than
		// take down the whole site because Redis blipped. Matches upstream
		// rate-limit behavior in model-rate-limit.go.
		return func() {}, true
	}
	if n, ok := res.(int64); !ok || n != 1 {
		return nil, false
	}
	return func() {
		// Use a small script for the release too, so we never dip below 0
		// even if some race allowed a spurious extra decrement.
		releaseScript := `
			local n = redis.call('DECR', KEYS[1])
			if n < 0 then redis.call('SET', KEYS[1], 0) end
			return n
		`
		_, _ = common.RDB.Eval(context.Background(), releaseScript,
			[]string{key}).Result()
	}, true
}

// acquireMemory is the process-local fallback. Single-node only; if you run
// >1 replicas without Redis, users can bypass the limit by racing across
// pods. Redis is the intended deployment.
func acquireMemory(mu *sync.Mutex, m map[string]int, key string, limit int) (func(), bool) {
	mu.Lock()
	defer mu.Unlock()
	if m[key] >= limit {
		return nil, false
	}
	m[key]++
	return func() {
		mu.Lock()
		defer mu.Unlock()
		if m[key] > 0 {
			m[key]--
		}
	}, true
}
