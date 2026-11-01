package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelayPreservesLastUpstreamErrorWhenFallbackIsExhausted(t *testing.T) {
	originalDB, originalLogDB := model.DB, model.LOG_DB
	originalCache, originalRedis := common.MemoryCacheEnabled, common.RedisEnabled
	originalMainDBType, originalLogDBType := common.MainDatabaseType(), common.LogDatabaseType()
	originalRetries, originalCountToken, originalErrorLog := common.RetryTimes, constant.CountToken, constant.ErrorLogEnabled
	originalGroupRatio := ratio_setting.GroupRatio2JSONString()
	originalModelRatio := ratio_setting.ModelRatio2JSONString()
	originalFreePreconsume := operation_setting.GetQuotaSetting().EnableFreeModelPreConsume
	t.Cleanup(func() {
		model.DB, model.LOG_DB = originalDB, originalLogDB
		common.MemoryCacheEnabled, common.RedisEnabled = originalCache, originalRedis
		common.SetDatabaseTypes(originalMainDBType, originalLogDBType)
		common.RetryTimes, constant.CountToken, constant.ErrorLogEnabled = originalRetries, originalCountToken, originalErrorLog
		operation_setting.GetQuotaSetting().EnableFreeModelPreConsume = originalFreePreconsume
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatio))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(originalModelRatio))
	})
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "relay-test", Group: "retry-test"}).Error)
	common.MemoryCacheEnabled, common.RedisEnabled = false, false
	common.RetryTimes, constant.CountToken, constant.ErrorLogEnabled = 2, false, true
	operation_setting.GetQuotaSetting().EnableFreeModelPreConsume = false
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"retry-test":0}`))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"gemini-2.0-flash":1}`))
	service.InitHttpClient()

	for _, test := range []struct {
		name, contentType, body, wantMessage string
	}{
		{"structured", "application/json", `{"error":{"message":"Upstream service temporarily unavailable","code":"private_code"}}`, "Upstream service temporarily unavailable"},
		// HTML has no public protocol message; only its status is preserved.
		{"html", "text/html", `<!DOCTYPE html><title>private.example | 502: Bad gateway</title>`, "Service temporarily unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var upstreamCalls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				upstreamCalls.Add(1)
				w.Header().Set("Content-Type", test.contentType)
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte(test.body))
			}))
			t.Cleanup(upstream.Close)
			channel := &model.Channel{Id: 166, Type: constant.ChannelTypeGemini, Name: "test-gemini", Key: "test-key", BaseURL: &upstream.URL, AutoBan: common.GetPointer(0)}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.0-flash:streamGenerateContent?alt=sse", strings.NewReader(`{"contents":[{"role":"user","parts":[{"text":"Hi"}]}]}`))
			c.Request.Header.Set("Content-Type", "application/json")
			common.SetContextKey(c, constant.ContextKeyUsingGroup, "retry-test")
			common.SetContextKey(c, constant.ContextKeyUserGroup, "retry-test")
			common.SetContextKey(c, constant.ContextKeyTokenGroup, "retry-test")
			common.SetContextKey(c, constant.ContextKeyUserId, 1)
			c.Set(common.RequestIdKey, test.name)
			require.Nil(t, middleware.SetupContextForSelectedChannel(c, channel, "gemini-2.0-flash"))

			Relay(c, types.RelayFormatGemini)

			assert.Equal(t, int32(1), upstreamCalls.Load(), "an exhausted fallback must not repeat the paid upstream call")
			assert.Equal(t, http.StatusBadGateway, recorder.Code)
			assert.Contains(t, recorder.Body.String(), test.wantMessage)
			assert.NotContains(t, recorder.Body.String(), "private.example")
			assert.NotContains(t, recorder.Body.String(), "private_code")
			var logs []model.Log
			require.NoError(t, db.Where("request_id = ?", test.name).Find(&logs).Error)
			require.Len(t, logs, 1)
			assert.Equal(t, model.LogTypeError, logs[0].Type)
			assert.Equal(t, "status_code=502, "+test.wantMessage, logs[0].Content)
			assert.Zero(t, logs[0].Quota)
		})
	}
}
