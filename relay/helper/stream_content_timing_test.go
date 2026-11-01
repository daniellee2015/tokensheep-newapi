package helper

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamScanner_ContentTimingDoesNotCountProtocolOnlyFrames(t *testing.T) {
	cases := map[string][]string{
		"messages":  {`{"type":"message_start","message":{"content":[]}}`, `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`, `{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"synthetic"}}`, `{"type":"message_stop"}`},
		"responses": {`{"type":"response.created","response":{"output":[]}}`, `{"type":"response.in_progress"}`, `{"type":"response.completed","response":{"output":[]}}`},
		"chat":      {`{"choices":[{"delta":{"role":"assistant","content":""}}]}`, `{"choices":[],"usage":{"completion_tokens":0}}`},
	}
	for name, frames := range cases {
		t.Run(name, func(t *testing.T) {
			body := "data: " + strings.Join(frames, "\n\ndata: ") + "\n\ndata: [DONE]\n\n"
			c, resp, info := setupStreamTest(t, strings.NewReader(body))
			info.IsStream = true
			info.StartTime = time.Now().Add(-time.Second)
			info.FirstResponseTime = info.StartTime.Add(250 * time.Millisecond)
			var forwarded []string
			StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
				forwarded = append(forwarded, data)
				assert.NoError(t, StringData(c, data))
			})
			require.Equal(t, frames, forwarded, "timing must not change or suppress event data")
			other := service.GenerateTextOtherInfo(c, info, 1, 1, 1, 0, 1, 1, 1)
			assert.Equal(t, float64(-1), other["frt"], "protocol-only stream has no semantic first content")
			assert.Equal(t, float64(250), other["frt_event_ms"], "retain raw first-event timing separately")
			assert.Equal(t, "downstream_first_content", other["frt_metric"])
		})
	}
}

func TestStreamScanner_ContentTimingMeasuresTextAndPreservesLiteral(t *testing.T) {
	for name, frame := range map[string]string{
		"messages":  `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"."}}`,
		"responses": `{"type":"response.output_text.delta","delta":"OK"}`,
		"chat":      `{"choices":[{"delta":{"content":"0.92 s."}}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			c, resp, info := setupStreamTest(t, strings.NewReader("data: "+frame+"\n\ndata: [DONE]\n\n"))
			info.IsStream = true
			info.StartTime = time.Now().Add(-time.Second)
			info.FirstResponseTime = info.StartTime.Add(250 * time.Millisecond)
			var forwarded []string
			StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
				forwarded = append(forwarded, data)
				assert.NoError(t, StringData(c, data))
			})
			require.Equal(t, []string{frame}, forwarded)
			other := service.GenerateTextOtherInfo(c, info, 1, 1, 1, 0, 1, 1, 1)
			require.IsType(t, float64(0), other["frt"])
			assert.Greater(t, other["frt"].(float64), float64(250), "content arrived after the protocol event")
			assert.Equal(t, other["frt"], other["first_text_ms"])
			assert.Equal(t, float64(250), other["frt_event_ms"])
		})
	}
}

func TestStreamScanner_ContentTimingDoesNotCountSuppressedReasoning(t *testing.T) {
	start := `{"type":"message_start","message":{"content":[]}}`
	thought := `{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"reason"}}`
	answer := `{"type":"content_block_delta","delta":{"type":"text_delta","text":"answer"}}`
	frames := []string{start, thought, answer}
	c, resp, info := setupStreamTest(t, strings.NewReader("data: "+strings.Join(frames, "\n\ndata: ")+"\n\ndata: [DONE]\n\n"))
	info.IsStream = true
	info.StartTime = time.Now().Add(-time.Second)
	info.FirstResponseTime = info.StartTime.Add(250 * time.Millisecond)
	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
		if data != thought {
			assert.NoError(t, StringData(c, data))
		}
	})
	other := service.GenerateTextOtherInfo(c, info, 1, 1, 1, 0, 1, 1, 1)
	assert.Equal(t, other["first_text_ms"], other["frt"])
	assert.NotContains(t, other, "first_thinking_ms")
	assert.False(t, info.ContentTiming.Snapshot().Upstream.FirstThinking.IsZero())
}

type contentTimingFailWriter struct {
	gin.ResponseWriter
	flushPanic bool
}

func (w *contentTimingFailWriter) Write(p []byte) (int, error) {
	if w.flushPanic {
		return w.ResponseWriter.Write(p)
	}
	return 0, errors.New("synthetic downstream write error")
}
func (w *contentTimingFailWriter) Flush() {
	if w.flushPanic {
		panic("synthetic flush failure")
	}
	w.ResponseWriter.Flush()
}

func TestStreamContentTimingWritesAndFailures(t *testing.T) {
	const frame = `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"."}}`
	const responseFrame = `{"type":"response.output_text.delta","delta":"."}`
	var claude dto.ClaudeResponse
	require.NoError(t, common.UnmarshalJsonStr(frame, &claude))
	var response dto.ResponsesStreamResponse
	require.NoError(t, common.UnmarshalJsonStr(responseFrame, &response))
	writes := map[string]func(*gin.Context){
		"string":        func(c *gin.Context) { _ = StringData(c, frame) },
		"claude_object": func(c *gin.Context) { _ = ClaudeData(c, claude) },
		"claude_native": func(c *gin.Context) { ClaudeChunkData(c, claude, frame) },
		"responses":     func(c *gin.Context) { _ = ResponseChunkData(c, response, responseFrame) },
	}
	for name, write := range writes {
		for _, mode := range []string{"success", "write_error", "flush_error", "cancelled"} {
			t.Run(name+"/"+mode, func(t *testing.T) {
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
				info := &relaycommon.RelayInfo{}
				info.EnsureContentTiming(c)
				if mode == "write_error" || mode == "flush_error" {
					c.Writer = &contentTimingFailWriter{ResponseWriter: c.Writer, flushPanic: mode == "flush_error"}
				}
				if mode == "cancelled" {
					ctx, cancel := context.WithCancel(c.Request.Context())
					cancel()
					c.Request = c.Request.WithContext(ctx)
				}
				write(c)
				first := info.ContentTiming.Snapshot().Downstream.FirstContent()
				assert.Equal(t, mode == "success", !first.IsZero(), "failed delivery must not become content TTFT")
				if mode == "success" {
					assert.Contains(t, recorder.Body.String(), `"."`)
				}
			})
		}
	}
}
