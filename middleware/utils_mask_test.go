package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	_ "github.com/QuantumNous/new-api/service" // installs the error-mask hook
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAbortWithOpenAiMessage_MasksInternalWording covers the middleware
// rejection path. These replies are written straight to the response and never
// become a NewAPIError, so they do not pass through ToOpenAIError/ToClaudeError
// and would otherwise leak internal wording that the relay path masks.
func TestAbortWithOpenAiMessage_MasksInternalWording(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name        string
		path        string
		message     string
		mustContain string
		mustNotHave []string
	}{
		{
			name:        "openai path hides group name and middleware name",
			path:        "/v1/chat/completions",
			message:     "No available channel for model gemini-3.5-flash under group gemini-lowprice (distributor)",
			mustContain: "gemini-3.5-flash",
			mustNotHave: []string{"gemini-lowprice", "distributor"},
		},
		{
			name:        "anthropic path is masked too",
			path:        "/v1/messages",
			message:     "No available channel for model claude-opus-5 under group claude-max-sale (distributor)",
			mustContain: "claude-opus-5",
			mustNotHave: []string{"claude-max-sale", "distributor"},
		},
		{
			name:        "credential pool size never reaches the caller",
			path:        "/v1/chat/completions",
			message:     "分组 [free] 无可用凭据（0/247 可用）",
			mustContain: "Service temporarily unavailable",
			mustNotHave: []string{"247", "凭据"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, tc.path, nil)

			abortWithOpenAiMessage(c, http.StatusServiceUnavailable, tc.message)

			body := recorder.Body.String()
			require.NotEmpty(t, body)
			assert.Contains(t, body, tc.mustContain)
			for _, leak := range tc.mustNotHave {
				assert.NotContains(t, body, leak,
					"middleware rejection leaked %q", leak)
			}
		})
	}
}

// TestAbortWithOpenAiMessage_KeepsEnvelopeShape guards the protocol contract:
// masking must not disturb the error envelope each client parses.
func TestAbortWithOpenAiMessage_KeepsEnvelopeShape(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("anthropic envelope keeps top-level type", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

		abortWithOpenAiMessage(c, http.StatusNotFound, "model not found")

		body := recorder.Body.String()
		assert.Contains(t, body, `"type":"error"`)
		assert.Contains(t, body, `"not_found_error"`)
	})

	t.Run("openai envelope keeps error code", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

		abortWithOpenAiMessage(c, http.StatusServiceUnavailable, "model not found")

		body := recorder.Body.String()
		assert.Contains(t, body, `"type":"new_api_error"`)
	})
}
