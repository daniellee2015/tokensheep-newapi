package controller

// R21: coverage for GetMyLimitsUsage. This handler backs the
// profile Limit card so it *has* to stay quiet under adverse conditions —
// missing Redis, missing counter key, wrong-shape value — because the
// alternative is a red banner on every profile page every time Redis
// blips. The invariants we pin here:
//
//  1. Happy path returns the live counter with source=live.
//  2. Missing key (idle user) returns 0 with source=idle, not an error.
//  3. Redis error path degrades to 0 + source=unavailable, never 5xx.
//  4. Groups not in SessionLimits report limit=0 (unlimited) — this
//     matches the middleware's fail-open semantics for the same map.
//  5. Unauthenticated request → 401. UserAuth normally guards this, but a
//     misconfigured route must still fail closed instead of leaking a
//     zero-user snapshot.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/tokensheep_setting"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupLimitsUsageTestDB wires an in-memory SQLite and a seeded user so
// GetMyLimitsUsage can resolve the caller's group. The Redis client stays
// disabled until the individual test opts in via useMiniRedis; that lets
// us cover both branches from the same fixture.
func setupLimitsUsageTestDB(t *testing.T, userID int, group string) *gorm.DB {
	t.Helper()

	prevDB, prevLog := model.DB, model.LOG_DB
	prevRedis := common.RedisEnabled
	prevRDB := common.RDB
	prevMain, prevLogType := common.MainDatabaseType(), common.LogDatabaseType()

	common.RedisEnabled = false
	common.RDB = nil
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB, model.LOG_DB = db, db
	require.NoError(t, db.AutoMigrate(&model.User{}))

	user := &model.User{
		Id:       userID,
		Username: fmt.Sprintf("limits-usage-%d", userID),
		Password: "password",
		Group:    group,
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, db.Create(user).Error)

	t.Cleanup(func() {
		model.DB, model.LOG_DB = prevDB, prevLog
		common.RedisEnabled = prevRedis
		common.RDB = prevRDB
		common.SetDatabaseTypes(prevMain, prevLogType)
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

// useMiniRedis attaches an in-process miniredis to common.RDB and returns
// it. Restores prior globals on cleanup so subsequent tests aren't infected.
func useMiniRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()

	prevEnabled := common.RedisEnabled
	prevRDB := common.RDB

	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	require.NoError(t, client.Ping(context.Background()).Err())
	common.RedisEnabled = true
	common.RDB = client

	t.Cleanup(func() {
		_ = client.Close()
		common.RedisEnabled = prevEnabled
		common.RDB = prevRDB
	})
	return server
}

// withSessionLimitsFixture pins tokensheep_economy.session_limits and
// restores prior state on cleanup. UpdateEconomySettingByJSONString does
// a merge onto the current shape rather than a full replace, so we can
// supply only the field(s) we care about.
func withSessionLimitsFixture(t *testing.T, jsonBody string) {
	t.Helper()
	prev := tokensheep_setting.EconomySetting2JSONString()
	require.NoError(t, tokensheep_setting.UpdateEconomySettingByJSONString(jsonBody))
	t.Cleanup(func() {
		_ = tokensheep_setting.UpdateEconomySettingByJSONString(prev)
	})
}

// callGetMyLimitsUsage invokes the handler with the given user id set on
// the context. userID=0 exercises the unauthenticated defensive branch.
func callGetMyLimitsUsage(t *testing.T, userID int) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/user/self/limits/usage", nil)
	if userID != 0 {
		ctx.Set("id", userID)
	}
	GetMyLimitsUsage(ctx)
	return recorder
}

type limitsUsageEnvelope struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    userLimitsUsage `json:"data"`
}

func decodeLimitsUsage(t *testing.T, recorder *httptest.ResponseRecorder) limitsUsageEnvelope {
	t.Helper()
	var body limitsUsageEnvelope
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	return body
}

func TestGetMyLimitsUsage_LiveCounterFromRedis(t *testing.T) {
	setupLimitsUsageTestDB(t, 101, "wholesale-plus")
	server := useMiniRedis(t)
	withSessionLimitsFixture(t, `{"session_limits":{"wholesale-plus":100}}`)

	// Prime the counter the same way SessionConcurrencyLimiter would:
	// bump it to 3 in-flight requests for user 101.
	require.NoError(t, server.Set("ts:session:active:101", "3"))

	setting.ModelRequestRateLimitEnabled = true
	setting.ModelRequestRateLimitDurationMinutes = 1
	t.Cleanup(func() {
		setting.ModelRequestRateLimitEnabled = false
		setting.ModelRequestRateLimitDurationMinutes = 1
	})

	rec := callGetMyLimitsUsage(t, 101)
	require.Equal(t, http.StatusOK, rec.Code)

	body := decodeLimitsUsage(t, rec)
	require.True(t, body.Success)
	assert.Equal(t, "wholesale-plus", body.Data.UserGroup)
	assert.Equal(t, 3, body.Data.ConcurrencyUsed)
	assert.Equal(t, 100, body.Data.ConcurrencyLimit)
	assert.Equal(t, concurrencySourceLive, body.Data.ConcurrencySource)
	assert.Equal(t, 1, body.Data.RPMWindowMinutes)
	assert.True(t, body.Data.RateLimitEnabled)
}

func TestGetMyLimitsUsage_IdleUserReportsZero(t *testing.T) {
	setupLimitsUsageTestDB(t, 202, "free")
	useMiniRedis(t) // key intentionally absent

	rec := callGetMyLimitsUsage(t, 202)
	require.Equal(t, http.StatusOK, rec.Code)

	body := decodeLimitsUsage(t, rec)
	require.True(t, body.Success)
	assert.Equal(t, 0, body.Data.ConcurrencyUsed,
		"idle user should read 0, not error out")
	assert.Equal(t, concurrencySourceIdle, body.Data.ConcurrencySource)
}

func TestGetMyLimitsUsage_RedisUnavailableFailsOpen(t *testing.T) {
	setupLimitsUsageTestDB(t, 303, "free")
	server := useMiniRedis(t)
	// Poison the counter: SET a non-numeric value. GET succeeds but the
	// atoi fails, exercising the "counter is corrupt" path. This is a
	// stand-in for "Redis is behaving weirdly" — the miniredis test
	// harness doesn't expose an easy way to force a raw connection error
	// mid-request, and both branches feed the same behaviour: report 0
	// with source=unavailable.
	require.NoError(t, server.Set("ts:session:active:303", "not-a-number"))

	rec := callGetMyLimitsUsage(t, 303)
	require.Equal(t, http.StatusOK, rec.Code,
		"Redis blip must not 5xx the profile page")

	body := decodeLimitsUsage(t, rec)
	require.True(t, body.Success)
	assert.Equal(t, 0, body.Data.ConcurrencyUsed)
	assert.Equal(t, concurrencySourceUnavailable, body.Data.ConcurrencySource)
}

func TestGetMyLimitsUsage_UnknownGroupHasZeroLimit(t *testing.T) {
	// A user whose group isn't in SessionLimits ("unknown-tier" here) is
	// intentionally unlimited by the middleware. This test pins that the
	// user-facing card mirrors that fact instead of pretending the ceiling
	// is 1 or falling back to a global default.
	setupLimitsUsageTestDB(t, 404, "unknown-tier")
	server := useMiniRedis(t)
	require.NoError(t, server.Set("ts:session:active:404", "2"))
	// Explicit empty session_limits so we're testing "group missing from
	// map", not "map is stale from another test".
	withSessionLimitsFixture(t, `{"session_limits":{}}`)

	rec := callGetMyLimitsUsage(t, 404)
	require.Equal(t, http.StatusOK, rec.Code)

	body := decodeLimitsUsage(t, rec)
	require.True(t, body.Success)
	assert.Equal(t, "unknown-tier", body.Data.UserGroup)
	assert.Equal(t, 0, body.Data.ConcurrencyLimit,
		"missing SessionLimits entry must surface as 0 (=unlimited)")
	assert.Equal(t, 2, body.Data.ConcurrencyUsed)
}

func TestGetMyLimitsUsage_UnauthenticatedReturns401(t *testing.T) {
	setupLimitsUsageTestDB(t, 505, "free")
	useMiniRedis(t)

	rec := callGetMyLimitsUsage(t, 0) // no id set
	assert.Equal(t, http.StatusUnauthorized, rec.Code,
		"missing userId must fail closed rather than return a zero-user snapshot")
}

func TestReadSessionActive_RedisDisabledDegradesToIdle(t *testing.T) {
	// Redis-less deployments (single-node dev boxes, tests) shouldn't
	// try to peek into the middleware's in-memory fallback map — that's
	// a package-private structure and reaching into it via reflection
	// would be a landmine. readSessionActive degrades to source=idle so
	// the frontend can badge "not tracking" without complaining.
	//
	// Testing the helper directly avoids the middleware's user-lookup
	// path (which itself needs Redis or initCol()) — that's already
	// exercised in the handler-level tests above.
	prevEnabled := common.RedisEnabled
	prevRDB := common.RDB
	common.RedisEnabled = false
	common.RDB = nil
	t.Cleanup(func() {
		common.RedisEnabled = prevEnabled
		common.RDB = prevRDB
	})

	used, source := readSessionActive(context.Background(), 42)
	assert.Equal(t, 0, used)
	assert.Equal(t, concurrencySourceIdle, source)
}
