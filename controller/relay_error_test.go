package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestWriteRelayErrorUsesClaudeSSEAfterStreamStarted(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	helper.SetEventStreamHeaders(c)
	c.Writer.WriteHeader(http.StatusOK)
	_, _ = c.Writer.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\"}\n\n"))

	// The fixture message deliberately avoids vocabulary the error-mask rules
	// rewrite, so this test stays focused on SSE framing rather than on mask
	// behaviour (covered by service.TestApplyGlobalErrorMask_*).
	relayErr := types.NewErrorWithStatusCode(
		assertionError("stream timeout before terminal event"),
		types.ErrorCodeBadResponseBody,
		http.StatusInternalServerError,
	)
	writeRelayError(c, nil, types.RelayFormatClaude, relayErr)

	body := recorder.Body.String()
	require.Contains(t, body, "event: error")
	require.Contains(t, body, `"type":"error"`)
	require.Contains(t, body, "stream timeout before terminal event")
	require.NotContains(t, body, `}{"type":"error"`)
	require.True(t, strings.HasPrefix(recorder.Header().Get("Content-Type"), "text/event-stream"))
}

func TestWriteRelayErrorUsesOpenAISSEAfterStreamStarted(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	helper.SetEventStreamHeaders(c)
	c.Writer.WriteHeader(http.StatusOK)
	_, _ = c.Writer.Write([]byte("data: {\"choices\":[]}\n\n"))

	relayErr := types.NewErrorWithStatusCode(
		assertionError("Service temporarily unavailable"),
		types.ErrorCodeServiceUnavailable,
		http.StatusServiceUnavailable,
	)
	writeRelayError(c, nil, types.RelayFormatOpenAI, relayErr)

	body := recorder.Body.String()
	require.Contains(t, body, `data: {"error":`)
	require.Contains(t, body, `"message":"Service temporarily unavailable"`)
	require.NotContains(t, body, `}{"error":`)
	require.True(t, strings.HasPrefix(recorder.Header().Get("Content-Type"), "text/event-stream"))
}

func TestWriteRelayErrorEmptyMessagesIs400InvalidRequest(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	relayErr := types.NewErrorWithStatusCode(
		assertionError("field messages is required"),
		types.ErrorCodeInvalidRequest,
		http.StatusBadRequest,
	)
	writeRelayError(c, nil, types.RelayFormatClaude, relayErr)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	require.Contains(t, recorder.Body.String(), `"type":"error"`)
	require.Contains(t, recorder.Body.String(), `"invalid_request_error"`)
	require.NotContains(t, recorder.Body.String(), `"new_api_error"`)
}

func TestWriteRelayErrorReplacesUpstreamRepresentationHeaders(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Header("Content-Type", "text/event-stream")
	c.Header("Content-Length", "999")
	c.Header("Content-Disposition", `attachment; filename="upstream-error.json"`)
	c.Header("X-Codex-Turn-State", "private-upstream-state")

	relayErr := types.NewErrorWithStatusCode(
		assertionError("Service temporarily unavailable"),
		types.ErrorCodeServiceUnavailable,
		http.StatusServiceUnavailable,
	)
	relayErr.SetRetryAfter(1500 * time.Millisecond)
	writeRelayError(c, nil, types.RelayFormatOpenAI, relayErr)

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Equal(t, "application/json; charset=utf-8", recorder.Header().Get("Content-Type"))
	require.NotEqual(t, "999", recorder.Header().Get("Content-Length"))
	require.Empty(t, recorder.Header().Get("Content-Disposition"))
	require.Empty(t, recorder.Header().Get("X-Codex-Turn-State"))
	require.Equal(t, "2", recorder.Header().Get("Retry-After"))
}

func TestChannelFallbackDelayUsesOnlyShortRetryableSignals(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		retryAfter time.Duration
		want       time.Duration
	}{
		{name: "short 429", statusCode: http.StatusTooManyRequests, retryAfter: time.Second, want: time.Second},
		{name: "short 503", statusCode: http.StatusServiceUnavailable, retryAfter: 2 * time.Second, want: 2 * time.Second},
		{name: "long quota reset", statusCode: http.StatusTooManyRequests, retryAfter: time.Minute},
		{name: "bad request", statusCode: http.StatusBadRequest, retryAfter: time.Second},
		{name: "missing delay", statusCode: http.StatusTooManyRequests},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := types.NewErrorWithStatusCode(assertionError("upstream failure"), types.ErrorCodeBadResponseStatusCode, test.statusCode)
			err.SetRetryAfter(test.retryAfter)
			require.Equal(t, test.want, channelFallbackDelay(err))
		})
	}
}

func TestShouldRetrySkipsLongQuotaCooldown(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	long429 := types.NewErrorWithStatusCode(
		assertionError("rate limit exceeded"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusTooManyRequests,
	)
	long429.SetRetryAfter(10 * time.Minute)
	require.False(t, shouldRetry(c, long429, 2))

	short429 := types.NewErrorWithStatusCode(
		assertionError("rate limit exceeded"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusTooManyRequests,
	)
	short429.SetRetryAfter(time.Second)
	require.True(t, shouldRetry(c, short429, 2))
}

func TestShouldRetrySkipsUnstructuredQuotaExhaustion(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	quota429 := types.NewErrorWithStatusCode(
		assertionError("Resource has been exhausted (e.g. check quota)."),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusTooManyRequests,
	)
	require.False(t, shouldRetry(c, quota429, 2))

	individualQuota429 := types.NewErrorWithStatusCode(
		assertionError("Individual quota reached. Please upgrade your subscription to increase your limits. Resets in 132h."),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusTooManyRequests,
	)
	require.False(t, shouldRetry(c, individualQuota429, 2))
}

func TestWaitBeforeChannelFallbackHonorsCancellation(t *testing.T) {
	err := types.NewErrorWithStatusCode(assertionError("upstream busy"), types.ErrorCodeBadResponseStatusCode, http.StatusTooManyRequests)
	err.SetRetryAfter(time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.ErrorIs(t, waitBeforeChannelFallback(ctx, err), context.Canceled)
}

type assertionError string

func (e assertionError) Error() string { return string(e) }
