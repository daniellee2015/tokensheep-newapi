package middleware

// Regression coverage for the three-layer concurrency gate added in R16-4.
//
// The station-wide layer is the one that actually protects the upstream
// pool from a fat-fingered group cap, so the invariants worth pinning are:
//
//  1. A zero/absent SystemConcurrency must not gate anything. Every
//     existing deployment has no option row for it; if the middleware
//     treated that as "limit 0" it would 503 all traffic on upgrade.
//  2. Once set, the station-wide counter must refuse request N+1 even when
//     the requests come from *different* users — that's the whole point,
//     and it's what a per-user counter can't do.
//  3. The station-wide gate must release on request completion, otherwise
//     the station wedges shut after the first burst.
//  4. The station-wide refusal must be distinguishable from a per-user
//     refusal (503 system_busy vs 429 session_limit_exceeded) so clients
//     and dashboards can tell "you're over quota" from "we're full".

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/tokensheep_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// useSessionMiniRedis points common.RDB at an in-process miniredis and
// restores the previous globals on cleanup.
func useSessionMiniRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()

	prevEnabled := common.RedisEnabled
	prevClient := common.RDB
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	require.NoError(t, client.Ping(context.Background()).Err())

	common.RedisEnabled = true
	common.RDB = client
	t.Cleanup(func() {
		_ = client.Close()
		common.RedisEnabled = prevEnabled
		common.RDB = prevClient
	})

	return server
}

// setEconomy applies a partial tokensheep_economy JSON and restores the
// prior state on cleanup. UpdateEconomySettingByJSONString merges onto the
// current struct, so omitted fields keep their existing values.
func setEconomy(t *testing.T, jsonStr string) {
	t.Helper()
	before := tokensheep_setting.EconomySetting2JSONString()
	require.NoError(t, tokensheep_setting.UpdateEconomySettingByJSONString(jsonStr))
	t.Cleanup(func() {
		_ = tokensheep_setting.UpdateEconomySettingByJSONString(before)
	})
}

// sessionTestRouter builds a router whose handler blocks on `hold` until
// the test lets it finish, so several requests can be in flight at once.
// Passing a nil channel makes the handler return immediately.
func sessionTestRouter(userID int, group string, hold <-chan struct{}) *gin.Engine {
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("id", userID)
		c.Set("group", group)
		c.Next()
	})
	router.Use(SessionConcurrencyLimiter())
	router.GET("/relay", func(c *gin.Context) {
		if hold != nil {
			<-hold
		}
		c.Status(http.StatusOK)
	})
	return router
}

func doSessionRequest(router http.Handler) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/relay", nil))
	return recorder
}

// TestSystemConcurrencyDisabledByDefault pins invariant 1: an unset
// SystemConcurrency must let traffic through untouched. Without this,
// shipping R16-4 to a station that never configures the option would take
// the whole station down.
func TestSystemConcurrencyDisabledByDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useSessionMiniRedis(t)
	// system_concurrency 0 == disabled; give the user's group a generous
	// per-user cap so only the station-wide gate is under test.
	setEconomy(t, `{"system_concurrency":0,"session_limits":{"fan":100}}`)

	require.Equal(t, 0, tokensheep_setting.GetSystemConcurrency(),
		"zero must be reported as disabled, not as a limit of zero")

	router := sessionTestRouter(1, "fan", nil)
	for i := 0; i < 5; i++ {
		res := doSessionRequest(router)
		assert.Equal(t, http.StatusOK, res.Code,
			"request %d should pass when the station-wide gate is disabled", i+1)
	}
}

// TestSystemConcurrencyCapsAcrossDifferentUsers pins invariant 2 and 4: the
// station-wide counter is shared, so two *different* accounts contend for
// it, and the refusal is a 503 system_busy rather than a per-user 429.
//
// This is the exact scenario the layer exists for: SessionLimits is set
// absurdly high (simulating a fat-fingered group cap) yet the station-wide
// ceiling still holds the line.
func TestSystemConcurrencyCapsAcrossDifferentUsers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useSessionMiniRedis(t)
	setEconomy(t, `{"system_concurrency":1,"session_limits":{"fan":100000}}`)

	hold := make(chan struct{})
	// Two routers, two distinct user IDs — a per-user counter would let
	// both through. Only a shared station-wide counter blocks the second.
	routerA := sessionTestRouter(101, "fan", hold)
	routerB := sessionTestRouter(202, "fan", nil)

	firstDone := make(chan *httptest.ResponseRecorder, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		firstDone <- doSessionRequest(routerA)
	}()

	// Wait until user 101's request is actually holding the slot.
	require.Eventually(t, func() bool {
		val, err := common.RDB.Get(context.Background(), systemCounterKey).Result()
		return err == nil && val == "1"
	}, 2*time.Second, 5*time.Millisecond,
		"first request should have taken the single station-wide slot")

	// Different user, station is full.
	second := doSessionRequest(routerB)
	assert.Equal(t, http.StatusServiceUnavailable, second.Code,
		"station-wide saturation must be 503, not 429")
	assert.Contains(t, second.Body.String(), "system_concurrency_limit_exceeded",
		"error code should identify the station-wide gate")

	close(hold)
	wg.Wait()
	assert.Equal(t, http.StatusOK, (<-firstDone).Code)
}

// TestSystemConcurrencyReleasesAfterCompletion pins invariant 3: the
// station-wide slot is handed back when the request finishes, so the
// station doesn't wedge shut after the first burst fills the counter.
func TestSystemConcurrencyReleasesAfterCompletion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useSessionMiniRedis(t)
	setEconomy(t, `{"system_concurrency":1,"session_limits":{"fan":100}}`)

	router := sessionTestRouter(303, "fan", nil)

	// Sequential requests each take and release the single slot.
	for i := 0; i < 3; i++ {
		res := doSessionRequest(router)
		require.Equal(t, http.StatusOK, res.Code,
			"sequential request %d should reuse the released slot", i+1)
	}

	// Counter is back to zero (or gone) once nothing is in flight.
	val, err := common.RDB.Get(context.Background(), systemCounterKey).Result()
	if err == nil {
		assert.Equal(t, "0", val, "counter should be back to zero when idle")
	}
}

// TestPerUserGateStillReturns429 guards the layer-2 behaviour against
// regression from the layer-3 insertion: a user over their own group cap
// must still get 429 session_limit_exceeded, not the new 503.
func TestPerUserGateStillReturns429(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useSessionMiniRedis(t)
	// Station-wide gate generous, per-user gate at 1.
	setEconomy(t, `{"system_concurrency":1000,"session_limits":{"fan":1}}`)

	hold := make(chan struct{})
	// Same user ID on both routers so the per-user counter is what bites.
	routerFirst := sessionTestRouter(404, "fan", hold)
	routerSecond := sessionTestRouter(404, "fan", nil)

	firstDone := make(chan *httptest.ResponseRecorder, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		firstDone <- doSessionRequest(routerFirst)
	}()

	require.Eventually(t, func() bool {
		val, err := common.RDB.Get(context.Background(), sessionCounterPrefix+"404").Result()
		return err == nil && val == "1"
	}, 2*time.Second, 5*time.Millisecond,
		"first request should hold the per-user slot")

	second := doSessionRequest(routerSecond)
	assert.Equal(t, http.StatusTooManyRequests, second.Code,
		"per-user saturation must stay a 429")
	assert.Contains(t, second.Body.String(), "session_limit_exceeded")

	close(hold)
	wg.Wait()
	assert.Equal(t, http.StatusOK, (<-firstDone).Code)
}

// TestSystemGateReleaseAfterPerUserRejection pins that a per-user 429 does
// NOT leak the station-wide slot. Layer 3 already acquired the slot for
// this request; the deferred release must still fire even though Layer 2
// short-circuits with c.Abort()+return before ever reaching c.Next().
//
// Why this case matters, and why the other four cases don't already cover
// it:
//   - Go defer runs when the surrounding function returns *for any reason*,
//     so the current code (defer releaseSystem() at the end of Layer 3's
//     success path, above Layer 2's checks) IS correct.
//   - But invariants 1–4 never actually read systemCounterKey after a 429.
//     Move `defer releaseSystem()` into the `if !systemOK` block, or push
//     it below Layer 2's `if !ok { return }`, and every existing case still
//     passes — while the station counter climbs forever on 429s and the
//     station eventually wedges shut. That is exactly the R16-4 risk the
//     verification pass flagged.
//
// The invariant pinned here: after a per-user 429 while another request is
// still holding the station slot, systemCounterKey == "1", never "2".
func TestSystemGateReleaseAfterPerUserRejection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useSessionMiniRedis(t)
	// Station cap generous, per-user cap at 1: Layer 3 lets the second
	// request in, Layer 2 turns it away with 429.
	setEconomy(t, `{"system_concurrency":10,"session_limits":{"fan":1}}`)

	const userID = 505
	userKey := sessionCounterPrefix + "505"

	hold := make(chan struct{})
	routerHolding := sessionTestRouter(userID, "fan", hold)
	routerRejected := sessionTestRouter(userID, "fan", nil)
	routerAfter := sessionTestRouter(userID, "fan", nil)

	firstDone := make(chan *httptest.ResponseRecorder, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		firstDone <- doSessionRequest(routerHolding)
	}()

	// Wait until request 1 is actually holding both counters. Reading both
	// here — not just the station one — makes the failure mode obvious if
	// the ordering of Layer 3 vs Layer 2 acquisition ever changes.
	require.Eventually(t, func() bool {
		sysVal, sysErr := common.RDB.Get(context.Background(), systemCounterKey).Result()
		userVal, userErr := common.RDB.Get(context.Background(), userKey).Result()
		return sysErr == nil && sysVal == "1" && userErr == nil && userVal == "1"
	}, 2*time.Second, 5*time.Millisecond,
		"first request should hold both station and per-user slots")

	// Request 2, same user: Layer 3 acquires (system 1→2), Layer 2 sees
	// per-user already at 1, rolls its own INCR back (per-user stays 1)
	// and aborts 429. Layer 3's deferred release must fire on the way out,
	// taking system back to 1.
	second := doSessionRequest(routerRejected)
	require.Equal(t, http.StatusTooManyRequests, second.Code,
		"per-user saturation must stay a 429 (Layer 2 semantics)")
	require.Contains(t, second.Body.String(), "session_limit_exceeded")

	sysAfter, err := common.RDB.Get(context.Background(), systemCounterKey).Result()
	require.NoError(t, err, "station counter should still exist while req 1 holds")
	// The load-bearing assertion. If this reads "2", `defer releaseSystem()`
	// didn't run on the 429 return path — station counter leaks on every
	// future 429 and eventually wedges the station shut.
	assert.Equalf(t, "1", sysAfter,
		"per-user 429 must NOT leak the station slot; got %q, want \"1\"", sysAfter)

	// Per-user counter also rolled back to 1 (Layer 2's own Lua DECR path).
	userAfter, err := common.RDB.Get(context.Background(), userKey).Result()
	require.NoError(t, err)
	assert.Equal(t, "1", userAfter,
		"per-user counter should still reflect only the holding request")

	// Release request 1; its own defers should return both counters to 0.
	close(hold)
	wg.Wait()
	require.Equal(t, http.StatusOK, (<-firstDone).Code)

	require.Eventually(t, func() bool {
		v, err := common.RDB.Get(context.Background(), systemCounterKey).Result()
		if err != nil {
			// Key gone (miniredis TTL / evict) — treat as zero.
			return true
		}
		return v == "0"
	}, 2*time.Second, 5*time.Millisecond,
		"station counter should reach zero once nothing is in flight")

	// Fresh request after drain: proves the middleware isn't wedged and
	// that releaseSystem() actually handed the slot back cleanly, not just
	// stopped incrementing.
	third := doSessionRequest(routerAfter)
	require.Equal(t, http.StatusOK, third.Code,
		"once the holding request releases, a subsequent request must succeed")

	if sysFinal, err := common.RDB.Get(context.Background(), systemCounterKey).Result(); err == nil {
		assert.Equal(t, "0", sysFinal, "station counter should be idle after all requests")
	}
	if userFinal, err := common.RDB.Get(context.Background(), userKey).Result(); err == nil {
		assert.Equal(t, "0", userFinal, "per-user counter should be idle after all requests")
	}
}
