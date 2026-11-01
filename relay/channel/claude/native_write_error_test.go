package claude

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type claudeCancelOnFlushWriter struct {
	gin.ResponseWriter
	cancel context.CancelFunc
}

func (w *claudeCancelOnFlushWriter) Flush() {
	w.ResponseWriter.Flush()
	w.cancel()
}

func TestNativeClaudeCancellationKeepsUsageWithoutSuccessTerminal(t *testing.T) {
	previous := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = previous })
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(ctx)
	c.Writer = &claudeCancelOnFlushWriter{ResponseWriter: c.Writer, cancel: cancel}
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude, ChannelMeta: &relaycommon.ChannelMeta{}, Request: &dto.ClaudeRequest{}, DisablePing: true}
	pr, pw := io.Pipe()
	defer pw.Close()
	go func() {
		_, _ = io.WriteString(pw, "data: "+`{"type":"message_start","message":{"id":"msg_fixture","model":"fixture","usage":{"input_tokens":7,"output_tokens":1}}}`+"\n\n")
	}()
	usage, err := ClaudeStreamHandler(c, &http.Response{Body: pr}, info)
	require.Nil(t, err, "accounting uses partial usage with client_gone stream status")
	require.NotNil(t, usage)
	assert.Equal(t, 7, usage.PromptTokens)
	assert.Equal(t, 1, usage.CompletionTokens)
	assert.Equal(t, relaycommon.StreamEndReasonClientGone, info.StreamStatus.EndReason)
	assert.NotContains(t, recorder.Body.String(), "message_stop")
}

type claudeEventFailWriter struct {
	gin.ResponseWriter
	match              string
	failed             bool
	writesAfterFailure int
}

func (w *claudeEventFailWriter) Write(p []byte) (int, error) {
	if w.failed {
		w.writesAfterFailure++
	}
	if strings.Contains(string(p), w.match) {
		w.failed = true
		return 0, errors.New("synthetic Claude downstream failure")
	}
	return w.ResponseWriter.Write(p)
}

func TestClaudeStreamPropagatesDownstreamFailures(t *testing.T) {
	oldTimeout, oldDisable := constant.StreamingTimeout, common.AutomaticDisableChannelEnabled
	constant.StreamingTimeout, common.AutomaticDisableChannelEnabled = 30, true
	t.Cleanup(func() { constant.StreamingTimeout, common.AutomaticDisableChannelEnabled = oldTimeout, oldDisable })
	frames := []string{
		`{"type":"message_start","message":{"id":"msg_fixture","type":"message","role":"assistant","model":"claude-fixture","content":[],"usage":{"input_tokens":7,"output_tokens":1}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"EARLY"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":7,"output_tokens":4}}`,
		`{"type":"message_stop"}`,
	}
	for _, tc := range []struct {
		name   string
		format types.RelayFormat
		match  string
	}{
		{"native header", types.RelayFormatClaude, "event: message_start"},
		{"native payload", types.RelayFormatClaude, "EARLY"},
		{"native terminal", types.RelayFormatClaude, "data: {\"type\":\"message_stop\"}"},
		{"converted payload", types.RelayFormatOpenAI, "EARLY"},
		{"converted final usage", types.RelayFormatOpenAI, `"choices":[],"usage":`},
		{"converted DONE", types.RelayFormatOpenAI, "[DONE]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			writer := &claudeEventFailWriter{ResponseWriter: c.Writer, match: tc.match}
			c.Writer = writer
			info := &relaycommon.RelayInfo{RelayFormat: tc.format, ChannelMeta: &relaycommon.ChannelMeta{}, Request: &dto.ClaudeRequest{}, DisablePing: true, ShouldIncludeUsage: true}
			resp := &http.Response{Body: io.NopCloser(strings.NewReader("data: " + strings.Join(frames, "\n\ndata: ") + "\n\n"))}
			usage, err := ClaudeStreamHandler(c, resp, info)
			require.True(t, writer.failed, "fixture must hit the selected write")
			require.NotNil(t, err, "a failed downstream write cannot return success")
			assert.Nil(t, usage)
			assert.True(t, types.IsSkipRetryError(err))
			assert.False(t, service.ShouldDisableChannel(err))
			assert.Zero(t, writer.writesAfterFailure)
			assert.NotContains(t, recorder.Body.String(), "[DONE]")
			assert.NotContains(t, recorder.Body.String(), `data: {"type":"message_stop"}`)
		})
	}
}
