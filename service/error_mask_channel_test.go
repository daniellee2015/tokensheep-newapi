package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMaskTestContext(rulesJSON string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	if rulesJSON != "" {
		c.Set(string(constant.ContextKeyChannelErrorMaskRules), rulesJSON)
	}
	return c
}

// TestApplyChannelErrorMask_LayersWithGlobal covers the two-tier contract: the
// channel rules rewrite the message first, then the global rules still run when
// the message is rendered for the caller.
func TestApplyChannelErrorMask_LayersWithGlobal(t *testing.T) {
	t.Run("channel rule handles wording the global set does not", func(t *testing.T) {
		c := newMaskTestContext(`[{"pattern":"quota pool exhausted on node-7","replace":"Service temporarily unavailable"}]`)
		relayErr := types.NewOpenAIError(
			assertErr("quota pool exhausted on node-7"),
			types.ErrorCodeBadResponseStatusCode,
			http.StatusServiceUnavailable,
		)

		ApplyChannelErrorMask(c, relayErr)

		// Rendering applies the global layer on top.
		assert.Equal(t, "Service temporarily unavailable", relayErr.ToOpenAIError().Message)
	})

	t.Run("global rules still apply to what the channel rule leaves behind", func(t *testing.T) {
		// The channel rule only renames the node; "无可用凭据" must still be
		// caught by the global rule set.
		c := newMaskTestContext(`[{"pattern":"node-7","replace":"backend"}]`)
		relayErr := types.NewOpenAIError(
			assertErr("node-7: 分组 [free] 无可用凭据（0/247 可用）"),
			types.ErrorCodeBadResponseStatusCode,
			http.StatusServiceUnavailable,
		)

		ApplyChannelErrorMask(c, relayErr)
		rendered := relayErr.ToOpenAIError().Message

		assert.NotContains(t, rendered, "247", "pool size must not survive")
		assert.NotContains(t, rendered, "凭据")
	})

	t.Run("no channel rules leaves the message for the global layer", func(t *testing.T) {
		c := newMaskTestContext("")
		relayErr := types.NewOpenAIError(
			assertErr("system cpu overloaded (current: 93.4%, threshold: 90%)"),
			types.ErrorCodeBadResponseStatusCode,
			http.StatusServiceUnavailable,
		)

		ApplyChannelErrorMask(c, relayErr)

		assert.Equal(t, "Service temporarily unavailable", relayErr.ToOpenAIError().Message)
	})

	t.Run("empty rule array is a no-op", func(t *testing.T) {
		c := newMaskTestContext("[]")
		relayErr := types.NewOpenAIError(
			assertErr("plain message"),
			types.ErrorCodeBadResponseStatusCode,
			http.StatusBadRequest,
		)

		ApplyChannelErrorMask(c, relayErr)

		assert.Equal(t, "plain message", relayErr.ToOpenAIError().Message)
	})

	t.Run("malformed channel rules are ignored", func(t *testing.T) {
		c := newMaskTestContext(`{not valid json`)
		relayErr := types.NewOpenAIError(
			assertErr("original message"),
			types.ErrorCodeBadResponseStatusCode,
			http.StatusBadRequest,
		)

		ApplyChannelErrorMask(c, relayErr)

		assert.Equal(t, "original message", relayErr.ToOpenAIError().Message)
	})

	t.Run("channel rule emptying the message falls back", func(t *testing.T) {
		c := newMaskTestContext(`[{"pattern":"everything","replace":""}]`)
		relayErr := types.NewOpenAIError(
			assertErr("everything"),
			types.ErrorCodeBadResponseStatusCode,
			http.StatusServiceUnavailable,
		)

		ApplyChannelErrorMask(c, relayErr)

		assert.Equal(t, "Service temporarily unavailable", relayErr.ToOpenAIError().Message)
	})

	t.Run("nil error and nil context are safe", func(t *testing.T) {
		require.NotPanics(t, func() {
			ApplyChannelErrorMask(newMaskTestContext("[]"), nil)
			ApplyChannelErrorMask(nil, types.NewOpenAIError(
				assertErr("x"), types.ErrorCodeBadResponseStatusCode, http.StatusBadRequest))
		})
	})
}

// TestMaskingAppliesToClaudeFormat guards the Claude render path, which builds
// its message separately from the OpenAI one.
func TestMaskingAppliesToClaudeFormat(t *testing.T) {
	relayErr := types.NewOpenAIError(
		assertErr("分组 [free] 无可用凭据（0/247 可用）"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusServiceUnavailable,
	)

	claudeMessage := relayErr.ToClaudeError().Message
	openAIMessage := relayErr.ToOpenAIError().Message

	assert.Equal(t, "Service temporarily unavailable", claudeMessage)
	assert.Equal(t, openAIMessage, claudeMessage, "both formats must mask identically")
}

// TestMaskingAppliesToUserVisibleLogText covers logs.content, which is written
// from MaskSensitiveErrorWithStatusCode and is visible in a user's own log view.
func TestMaskingAppliesToUserVisibleLogText(t *testing.T) {
	relayErr := types.NewOpenAIError(
		assertErr("分组 [free] 无可用凭据（0/247 可用）"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusServiceUnavailable,
	)

	logged := relayErr.MaskSensitiveErrorWithStatusCode()

	assert.Contains(t, logged, "status_code=503")
	assert.Contains(t, logged, "Service temporarily unavailable")
	assert.NotContains(t, logged, "247", "log text is user-visible and must not leak pool size")
}

type assertErr string

func (e assertErr) Error() string { return string(e) }
