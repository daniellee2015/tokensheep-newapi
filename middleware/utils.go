package middleware

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
)

func abortWithOpenAiMessage(c *gin.Context, statusCode int, message string, code ...types.ErrorCode) {
	codeStr := ""
	if len(code) > 0 {
		codeStr = string(code[0])
	}
	userId := c.GetInt("id")
	withRequestId := common.MessageWithRequestId(message, c.GetString(common.RequestIdKey))

	// Middleware runs before the route handler picks a relay format, so the
	// envelope is chosen from the path. An Anthropic client parses the body
	// through its own error type and rejects one that lacks the top-level
	// "type":"error" wrapper, turning a clear rejection ("no channel for this
	// model") into an opaque parse failure.
	if isAnthropicRequestPath(c) {
		c.JSON(statusCode, gin.H{
			"type": "error",
			"error": gin.H{
				"type":    anthropicErrorType(statusCode),
				"message": withRequestId,
			},
		})
	} else {
		c.JSON(statusCode, gin.H{
			"error": gin.H{
				"message": withRequestId,
				"type":    "new_api_error",
				"code":    codeStr,
			},
		})
	}
	c.Abort()
	logger.LogError(c.Request.Context(), fmt.Sprintf("user %d | %s", userId, message))
}

// isAnthropicRequestPath reports whether the caller is speaking the Anthropic
// Messages protocol and therefore expects Anthropic-shaped errors.
func isAnthropicRequestPath(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return false
	}
	path := c.Request.URL.Path
	return strings.HasSuffix(path, "/v1/messages") ||
		strings.HasSuffix(path, "/v1/messages/count_tokens")
}

// anthropicModelNotFoundStatus uses official 404 not_found_error for unknown
// models on the Messages API. Other formats keep 503 so existing OpenAI
// clients that treat 503 as retryable are unchanged.
func anthropicModelNotFoundStatus(c *gin.Context) int {
	if isAnthropicRequestPath(c) {
		return http.StatusNotFound
	}
	return http.StatusServiceUnavailable
}

// anthropicErrorType maps an HTTP status onto Anthropic's documented error
// type strings, which clients switch on to decide whether to retry.
func anthropicErrorType(statusCode int) string {
	switch statusCode {
	case http.StatusBadRequest:
		return "invalid_request_error"
	case http.StatusUnauthorized:
		return "authentication_error"
	case http.StatusForbidden:
		return "permission_error"
	case http.StatusNotFound:
		return "not_found_error"
	case http.StatusRequestEntityTooLarge:
		return "request_too_large"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	case http.StatusServiceUnavailable:
		return "overloaded_error"
	default:
		return "api_error"
	}
}

// logRateLimitedRejection 异步落一条 LogTypeRateLimited 日志。
// 从 gin.Context 里取 userId / tokenId / tokenName / path 快照，避免限流器
// 拒绝时 relay 主链路还没写入这些字段导致日志缺失。userId<=0 时不写日志，
// 因为限流器本身允许匿名请求走 IP 桶，那类拒绝不属于账户级审计范畴。
func logRateLimitedRejection(c *gin.Context, reason string) {
	if c == nil {
		return
	}
	userId := c.GetInt("id")
	if userId <= 0 {
		return
	}
	tokenId := c.GetInt("token_id")
	tokenName := c.GetString("token_name")
	path := ""
	if c.Request != nil && c.Request.URL != nil {
		path = c.Request.URL.Path
	}
	gopool.Go(func() {
		model.RecordRateLimitedLog(userId, tokenId, tokenName, path, reason)
	})
}

func abortWithMidjourneyMessage(c *gin.Context, statusCode int, code int, description string) {
	c.JSON(statusCode, gin.H{
		"description": description,
		"type":        "new_api_error",
		"code":        code,
	})
	c.Abort()
	logger.LogError(c.Request.Context(), description)
}
