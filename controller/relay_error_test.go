package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	writeRelayError(c, nil, types.RelayFormatOpenAI, relayErr)

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Equal(t, "application/json; charset=utf-8", recorder.Header().Get("Content-Type"))
	require.NotEqual(t, "999", recorder.Header().Get("Content-Length"))
	require.Empty(t, recorder.Header().Get("Content-Disposition"))
	require.Empty(t, recorder.Header().Get("X-Codex-Turn-State"))
}

type assertionError string

func (e assertionError) Error() string { return string(e) }
