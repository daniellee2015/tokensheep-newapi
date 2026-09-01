// R21: self-service limit usage — the "actual numbers" cousin of the
// static limits already returned by GetMyTier.
//
// The tier snapshot answers "what are my ceilings?" (RPM per group,
// session concurrency for my group). This endpoint answers the noisier
// "what am I using right now?" — specifically the live in-flight session
// count driven by middleware/SessionConcurrencyLimiter through the
// Redis counter `ts:session:active:<userId>`. The frontend Limit card
// pairs the two to render "3 / 100 concurrent, RPM 1000/min".
//
// Design points worth pinning:
//
//  - concurrency_used is best-effort. If Redis is down or the key is
//    unset (the user is idle) we return 0 with concurrency_source
//    "unavailable" | "idle" instead of 500'ing the API — the card is a
//    visualization, not a permission check. The user should always be
//    able to load their own profile.
//  - We don't report the station-wide layer 3 counter (ts:system:active).
//    That's operator-facing; showing it in the user UI would leak system
//    load to every account.
//  - rpm_window_minutes echoes the *global* rate-limit window so the
//    frontend can say "past X minutes" without inspecting each group.
package controller

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/tokensheep_setting"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

// sessionActiveKeyPrefix mirrors middleware.sessionCounterPrefix. We
// duplicate the constant instead of importing it so a read-side controller
// can't accidentally reach into the middleware package's private surface.
// If the middleware ever renames the key, the compiler won't catch it —
// which is why middleware/session_concurrency_test.go asserts on the
// literal string too.
const sessionActiveKeyPrefix = "ts:session:active:"

// concurrencySourceLive means the counter came from Redis (or the value
// was legitimately absent, which we treat as 0 = idle).
const (
	concurrencySourceLive        = "live"
	concurrencySourceIdle        = "idle"
	concurrencySourceUnavailable = "unavailable"
)

type userLimitsUsage struct {
	UserGroup           string `json:"user_group"`
	ConcurrencyUsed     int    `json:"concurrency_used"`
	ConcurrencyLimit    int    `json:"concurrency_limit"`
	ConcurrencySource   string `json:"concurrency_source"`
	RPMWindowMinutes    int    `json:"rpm_window_minutes"`
	RateLimitEnabled    bool   `json:"rate_limit_enabled"`
}

// GetMyLimitsUsage returns the current user's live limit consumption.
// Attach behind UserAuth so c.GetInt("id") is populated; unauth users
// hit 401 from the middleware, not this handler.
func GetMyLimitsUsage(c *gin.Context) {
	userId := c.GetInt("id")
	if userId == 0 {
		// Defensive: the router wires UserAuth in front of us so this
		// branch shouldn't fire in production, but if a caller bypasses
		// the middleware (test harness, misconfigured route) we return
		// 401 instead of leaking a zero-user snapshot.
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "unauthorized",
		})
		return
	}

	group, err := model.GetUserGroup(userId, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	used, source := readSessionActive(c.Request.Context(), userId)

	view := userLimitsUsage{
		UserGroup:         group,
		ConcurrencyUsed:   used,
		ConcurrencyLimit:  tokensheep_setting.SessionLimit(group),
		ConcurrencySource: source,
		RPMWindowMinutes:  setting.ModelRequestRateLimitDurationMinutes,
		RateLimitEnabled:  setting.ModelRequestRateLimitEnabled,
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    view,
	})
}

// readSessionActive reads the per-user in-flight counter. Split out so
// the fallback / error path is easy to test independently.
//
// Return semantics:
//
//	source == concurrencySourceLive        → counter read succeeded and was > 0
//	source == concurrencySourceIdle        → Redis returned key-not-found (redis.Nil)
//	                                          or Redis is disabled (single-node
//	                                          deployment without Redis: we don't
//	                                          reach into the middleware's memory
//	                                          fallback map because that's a
//	                                          package-private structure).
//	source == concurrencySourceUnavailable → Redis returned an error, counter
//	                                          value is unknown. Return 0 so the
//	                                          UI stays readable and mark the
//	                                          source so the frontend can badge
//	                                          the card as "stale".
func readSessionActive(ctx context.Context, userId int) (int, string) {
	if !common.RedisEnabled || common.RDB == nil {
		// Redis-less deployments use middleware's in-memory map, which is
		// intentionally not exported. The user-facing card degrades to
		// "idle" (0) rather than misreport whatever the local counter
		// happens to hold on this pod.
		return 0, concurrencySourceIdle
	}
	key := sessionActiveKeyPrefix + strconv.Itoa(userId)
	val, err := common.RDB.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return 0, concurrencySourceIdle
		}
		return 0, concurrencySourceUnavailable
	}
	n, parseErr := strconv.Atoi(val)
	if parseErr != nil || n < 0 {
		// Corrupt counter (someone SET a string). Treat as unavailable
		// rather than surface the parse error to the user.
		return 0, concurrencySourceUnavailable
	}
	return n, concurrencySourceLive
}
