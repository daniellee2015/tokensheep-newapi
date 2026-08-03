package middleware

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type tokenLimitErrorResponse struct {
	Error struct {
		Code string `json:"code"`
	} `json:"error"`
}

func withTokenLimits(tokenId int, rpmLimit int, concurrencyLimit int) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("token_id", tokenId)
		c.Set("rpm_limit", rpmLimit)
		c.Set("concurrency_limit", concurrencyLimit)
		c.Next()
	}
}

func performTokenLimitRequest(router http.Handler) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	router.ServeHTTP(recorder, request)
	return recorder
}

func decodeTokenLimitError(t *testing.T, recorder *httptest.ResponseRecorder) tokenLimitErrorResponse {
	t.Helper()
	var response tokenLimitErrorResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func TestSetupContextForTokenIncludesPerTokenLimits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	rpmLimit := 12
	concurrencyLimit := 4
	token := &model.Token{Id: 91, UserId: 17, RpmLimit: &rpmLimit, ConcurrencyLimit: &concurrencyLimit}

	require.NoError(t, SetupContextForToken(context, token))
	assert.Equal(t, 91, context.GetInt("token_id"))
	assert.Equal(t, 12, context.GetInt("rpm_limit"))
	assert.Equal(t, 4, context.GetInt("concurrency_limit"))
}

func TestTokenRPMLimiterIsolatesTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldRedisEnabled := common.RedisEnabled
	oldRedisClient := common.RDB
	common.RedisEnabled = false
	common.RDB = nil
	t.Cleanup(func() {
		common.RedisEnabled = oldRedisEnabled
		common.RDB = oldRedisClient
	})

	limiter := TokenRPMLimiter()
	newRouter := func(tokenId int) *gin.Engine {
		router := gin.New()
		router.Use(withTokenLimits(tokenId, 2, 0), limiter)
		router.POST("/v1/chat/completions", func(c *gin.Context) { c.Status(http.StatusNoContent) })
		return router
	}

	tokenOneRouter := newRouter(101)
	assert.Equal(t, http.StatusNoContent, performTokenLimitRequest(tokenOneRouter).Code)
	assert.Equal(t, http.StatusNoContent, performTokenLimitRequest(tokenOneRouter).Code)
	limited := performTokenLimitRequest(tokenOneRouter)
	assert.Equal(t, http.StatusTooManyRequests, limited.Code)
	assert.Equal(t, "token_rpm_limit_exceeded", decodeTokenLimitError(t, limited).Error.Code)

	tokenTwoRouter := newRouter(102)
	assert.Equal(t, http.StatusNoContent, performTokenLimitRequest(tokenTwoRouter).Code)
}

func TestTokenConcurrencyLimiterIsolatesAndReleasesTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldRedisEnabled := common.RedisEnabled
	oldRedisClient := common.RDB
	common.RedisEnabled = false
	common.RDB = nil
	t.Cleanup(func() {
		common.RedisEnabled = oldRedisEnabled
		common.RDB = oldRedisClient
	})

	limiter := TokenConcurrencyLimiter()
	entered := make(chan struct{}, 1)
	releaseFirst := make(chan struct{})
	var blockFirst atomic.Bool
	blockFirst.Store(true)

	newRouter := func(tokenId int) *gin.Engine {
		router := gin.New()
		router.Use(withTokenLimits(tokenId, 0, 1), limiter)
		router.POST("/v1/chat/completions", func(c *gin.Context) {
			if tokenId == 201 && blockFirst.CompareAndSwap(true, false) {
				entered <- struct{}{}
				<-releaseFirst
			}
			c.Status(http.StatusNoContent)
		})
		return router
	}

	tokenOneRouter := newRouter(201)
	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		firstDone <- performTokenLimitRequest(tokenOneRouter)
	}()
	<-entered

	limited := performTokenLimitRequest(tokenOneRouter)
	assert.Equal(t, http.StatusTooManyRequests, limited.Code)
	assert.Equal(t, "token_concurrency_limit_exceeded", decodeTokenLimitError(t, limited).Error.Code)

	tokenTwoRouter := newRouter(202)
	assert.Equal(t, http.StatusNoContent, performTokenLimitRequest(tokenTwoRouter).Code)

	close(releaseFirst)
	assert.Equal(t, http.StatusNoContent, (<-firstDone).Code)
	assert.Equal(t, http.StatusNoContent, performTokenLimitRequest(tokenOneRouter).Code)
}
