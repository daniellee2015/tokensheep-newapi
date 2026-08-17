package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/types"
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

	relayErr := types.NewErrorWithStatusCode(
		assertionError("upstream stream timeout before terminal event"),
		types.ErrorCodeBadResponseBody,
		http.StatusInternalServerError,
	)
	writeRelayError(c, nil, types.RelayFormatClaude, relayErr)

	body := recorder.Body.String()
	require.Contains(t, body, "event: error")
	require.Contains(t, body, `"type":"error"`)
	require.Contains(t, body, "upstream stream timeout before terminal event")
	require.NotContains(t, body, `}{"type":"error"`)
	require.True(t, strings.HasPrefix(recorder.Header().Get("Content-Type"), "text/event-stream"))
}

type assertionError string

func (e assertionError) Error() string { return string(e) }
