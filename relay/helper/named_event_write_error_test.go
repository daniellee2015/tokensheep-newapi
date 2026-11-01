package helper

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type namedEventFailWriter struct {
	gin.ResponseWriter
	match              string
	failed             bool
	writesAfterFailure int
}

func (w *namedEventFailWriter) Write(p []byte) (int, error) {
	if w.failed {
		w.writesAfterFailure++
	}
	if strings.Contains(string(p), w.match) {
		w.failed = true
		return 0, errors.New("synthetic named event write failure")
	}
	return w.ResponseWriter.Write(p)
}

func TestNamedEventHelpersPropagateFailures(t *testing.T) {
	const text = `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"."}}`
	var claude dto.ClaudeResponse
	require.NoError(t, common.UnmarshalJsonStr(text, &claude))
	for name, write := range map[string]func(*gin.Context) error{
		"claude":        func(c *gin.Context) error { return ClaudeData(c, claude) },
		"claude_native": func(c *gin.Context) error { return ClaudeChunkData(c, claude, text) },
		"responses": func(c *gin.Context) error {
			return ResponseChunkData(c, dto.ResponsesStreamResponse{Type: "response.output_text.delta"}, `{"type":"response.output_text.delta","delta":"."}`)
		},
	} {
		for _, mode := range []string{"header", "payload", "flush", "cancelled", "older_error"} {
			t.Run(name+"/"+mode, func(t *testing.T) {
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/test", nil)
				info := &relaycommon.RelayInfo{}
				info.EnsureContentTiming(c)
				var failing *namedEventFailWriter
				switch mode {
				case "header", "payload":
					match := "event: "
					if mode == "payload" {
						match = "data: "
					}
					failing = &namedEventFailWriter{ResponseWriter: c.Writer, match: match}
					c.Writer = failing
				case "flush":
					c.Writer = &contentTimingFailWriter{ResponseWriter: c.Writer, flushPanic: true}
				case "cancelled":
					ctx, cancel := context.WithCancel(c.Request.Context())
					cancel()
					c.Request = c.Request.WithContext(ctx)
				case "older_error":
					_ = c.Error(errors.New("unrelated previous error"))
				}
				err := write(c)
				if mode == "older_error" {
					require.NoError(t, err)
					assert.Contains(t, recorder.Body.String(), `"."`)
					assert.False(t, info.ContentTiming.Snapshot().Downstream.FirstText.IsZero())
					return
				}
				require.Error(t, err)
				assert.True(t, info.ContentTiming.Snapshot().Downstream.FirstContent().IsZero())
				if failing != nil {
					assert.Zero(t, failing.writesAfterFailure, "stop writing a partial event after the first failure")
				}
			})
		}
	}
}

func TestClaudeDataMarshalFailureEmitsNoEvent(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	err := ClaudeData(c, dto.ClaudeResponse{Type: "content_block_start", ContentBlock: &dto.ClaudeMediaMessage{Type: "tool_use", Input: make(chan int)}})
	require.Error(t, err)
	assert.Empty(t, recorder.Body.String())
	assert.False(t, recorder.Flushed)
}
