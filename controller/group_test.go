package controller

// R21: contract test for GetUserGroups.
//
// The frontend API-key picker expects each group's payload to now carry
// two extra fields — rpm (the per-group RPM ceiling) and
// rpm_window_minutes (the rolling window that ceiling applies to).
// GetUserGroups is public (no auth middleware) so we can't rely on a
// UserAuth-populated context; the handler falls back to userId=0 with a
// silent group lookup, which for our purposes is fine — we're testing the
// per-group RPM columns, not the user-tier logic.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupGroupTestDB gives GetUserGroups a real (in-memory) DB so
// model.GetUserGroup doesn't panic on the cache miss → SQL fallthrough.
// The handler does a group lookup even when the caller isn't
// authenticated (userId=0), and swallows the resulting error, but the
// call still hits the DB — so we must wire one up.
func setupGroupTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	prevDB, prevLog := model.DB, model.LOG_DB
	prevRedis := common.RedisEnabled
	prevMain, prevLogType := common.MainDatabaseType(), common.LogDatabaseType()

	common.RedisEnabled = false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB, model.LOG_DB = db, db
	require.NoError(t, db.AutoMigrate(&model.User{}))

	t.Cleanup(func() {
		model.DB, model.LOG_DB = prevDB, prevLog
		common.RedisEnabled = prevRedis
		common.SetDatabaseTypes(prevMain, prevLogType)
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

// withRateLimitFixture pins the four rate-limit knobs used by GetUserGroups
// / GetMyLimitsUsage to a known state and restores the prior globals on
// cleanup. All four are process-global so parallel tests can't share the
// package.
func withRateLimitFixture(t *testing.T, enabled bool, durationMinutes int, groupJSON string) {
	t.Helper()

	prevEnabled := setting.ModelRequestRateLimitEnabled
	prevDuration := setting.ModelRequestRateLimitDurationMinutes
	prevGroups := setting.ModelRequestRateLimitGroup2JSONString()

	setting.ModelRequestRateLimitEnabled = enabled
	setting.ModelRequestRateLimitDurationMinutes = durationMinutes
	if groupJSON != "" {
		require.NoError(t, setting.UpdateModelRequestRateLimitGroupByJSONString(groupJSON))
	} else {
		require.NoError(t, setting.UpdateModelRequestRateLimitGroupByJSONString("{}"))
	}

	t.Cleanup(func() {
		setting.ModelRequestRateLimitEnabled = prevEnabled
		setting.ModelRequestRateLimitDurationMinutes = prevDuration
		_ = setting.UpdateModelRequestRateLimitGroupByJSONString(prevGroups)
	})
}

// withGroupCatalogFixture pins GroupRatio + UserUsableGroups so
// GetUserGroups produces a deterministic set of groups regardless of what
// other tests left in the globals.
func withGroupCatalogFixture(t *testing.T, ratioJSON, usableJSON string) {
	t.Helper()
	prevRatio := ratio_setting.GroupRatio2JSONString()
	prevUsable := setting.UserUsableGroups2JSONString()

	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(ratioJSON))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(usableJSON))

	t.Cleanup(func() {
		_ = ratio_setting.UpdateGroupRatioByJSONString(prevRatio)
		_ = setting.UpdateUserUsableGroupsByJSONString(prevUsable)
	})
}

// callGetUserGroups builds a bare gin context (userId=0 → group="") and
// invokes GetUserGroups. Returns the decoded per-group payload.
func callGetUserGroups(t *testing.T) map[string]map[string]interface{} {
	t.Helper()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/user/groups", nil)
	GetUserGroups(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)

	var body struct {
		Success bool                              `json:"success"`
		Data    map[string]map[string]interface{} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.True(t, body.Success)
	return body.Data
}

func TestGetUserGroups_IncludesRPMWhenRateLimitEnabled(t *testing.T) {
	setupGroupTestDB(t)
	withGroupCatalogFixture(t,
		`{"default":1,"vip":1}`,
		`{"default":"Default","vip":"VIP"}`,
	)
	// default: [total=2000, success=1000]; vip has no entry so falls
	// through to "unlimited" (0/0).
	withRateLimitFixture(t, true, 1, `{"default":[2000,1000]}`)

	data := callGetUserGroups(t)

	defaultGroup, ok := data["default"]
	require.True(t, ok, "default group missing from response")
	require.EqualValues(t, 1000, defaultGroup["rpm"], "default rpm should reflect limits[1]")
	require.EqualValues(t, 1, defaultGroup["rpm_window_minutes"])
	// Existing 'kind' contract must still hold — this is a regression guard
	// against the two fields getting out of sync when someone edits the
	// map literal.
	require.Equal(t, "channel", defaultGroup["kind"])

	vipGroup, ok := data["vip"]
	require.True(t, ok, "vip group missing from response")
	require.EqualValues(t, 0, vipGroup["rpm"], "unlisted group should report 0 rpm")
	require.EqualValues(t, 0, vipGroup["rpm_window_minutes"])
}

func TestGetUserGroups_RateLimitDisabledReportsZero(t *testing.T) {
	setupGroupTestDB(t)
	withGroupCatalogFixture(t,
		`{"default":1}`,
		`{"default":"Default"}`,
	)
	// Even with a fully-populated map, disabling the global switch must
	// zero out the exposed RPM — otherwise the frontend would render a
	// number that isn't actually being enforced.
	withRateLimitFixture(t, false, 5, `{"default":[9999,9999]}`)

	data := callGetUserGroups(t)
	defaultGroup, ok := data["default"]
	require.True(t, ok)
	require.EqualValues(t, 0, defaultGroup["rpm"],
		"rpm must be 0 when ModelRequestRateLimitEnabled=false")
	require.EqualValues(t, 0, defaultGroup["rpm_window_minutes"],
		"window must be 0 when rate limiting is disabled")
}

func TestGetUserGroups_WindowMinutesEchoesGlobalSetting(t *testing.T) {
	// >1-minute windows exist in the wild (some tiers cap by 5-minute
	// rolling window). The response must expose the raw window so the
	// frontend can compute per-minute display values itself.
	setupGroupTestDB(t)
	withGroupCatalogFixture(t,
		`{"default":1}`,
		`{"default":"Default"}`,
	)
	withRateLimitFixture(t, true, 5, `{"default":[500,300]}`)

	data := callGetUserGroups(t)
	defaultGroup := data["default"]
	require.EqualValues(t, 300, defaultGroup["rpm"])
	require.EqualValues(t, 5, defaultGroup["rpm_window_minutes"])
}
