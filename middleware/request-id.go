package middleware

import (
	"context"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

func RequestId() func(c *gin.Context) {
	return func(c *gin.Context) {
		id := common.NewRequestId()
		c.Set(common.RequestIdKey, id)
		ctx := context.WithValue(c.Request.Context(), common.RequestIdKey, id)
		c.Request = c.Request.WithContext(ctx)
		c.Header(common.RequestIdKey, id)
		// Anthropic and OpenAI SDKs read the correlation id from `request-id`
		// and surface it in error messages and bug reports. The internal
		// X-Oneapi-Request-Id stays date-prefixed for billing; the public
		// header must be Anthropic-shaped (`req_…`) or protocol checks fail.
		c.Header("request-id", common.NewAnthropicRequestId())
		c.Next()
	}
}
