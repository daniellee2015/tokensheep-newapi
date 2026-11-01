package openai

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type convertedCancelWriter struct {
	gin.ResponseWriter
	cancel      context.CancelFunc
	contentSeen bool
}

func (w *convertedCancelWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	if strings.Contains(string(p[:n]), "EARLY") {
		w.contentSeen = true
	}
	return n, err
}

func (w *convertedCancelWriter) Flush() {
	w.ResponseWriter.Flush()
	if w.contentSeen {
		w.cancel()
	}
}

func TestConvertedCancellationPreservesReportedUsage(t *testing.T) {
	chat := `{"id":"chatcmpl_fixture","created":1710000000,"model":"fixture","choices":[{"index":0,"delta":{"role":"assistant","content":"EARLY"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":4,"total_tokens":11}}`
	responses := `{"type":"response.completed","response":{"id":"resp_fixture","model":"fixture","status":"completed","output":[{"type":"message","id":"msg_fixture","role":"assistant","content":[{"type":"output_text","text":"EARLY"}]}],"usage":{"input_tokens":7,"output_tokens":4,"total_tokens":11}}}`
	for _, route := range []struct {
		name   string
		format types.RelayFormat
		frame  string
		handle func(*gin.Context, *relaycommon.RelayInfo, *http.Response) (*dto.Usage, *types.NewAPIError)
	}{
		{"native chat final content", types.RelayFormatOpenAI, chat, OaiStreamHandler},
		{"chat to Claude", types.RelayFormatClaude, chat, OaiStreamHandler},
		{"chat to Responses", types.RelayFormatOpenAIResponses, chat, OaiChatToResponsesStreamHandler},
		{"Responses to chat", types.RelayFormatOpenAI, responses, OaiResponsesToChatStreamHandler},
		{"Responses to Claude", types.RelayFormatClaude, responses, OaiResponsesToChatStreamHandler},
	} {
		t.Run(route.name, func(t *testing.T) {
			c, recorder, info, resp := newUpstreamStreamErrorFixture(t, "data: "+route.frame+"\n\ndata: [DONE]\n\n", relayconstant.RelayModeChatCompletions)
			ctx, cancel := context.WithCancel(c.Request.Context())
			defer cancel()
			c.Request = c.Request.WithContext(ctx)
			writer := &convertedCancelWriter{ResponseWriter: c.Writer, cancel: cancel}
			c.Writer = writer
			info.RelayFormat = route.format
			info.UpstreamModelName = "fixture"
			info.IsStream, info.DisablePing, info.ShouldIncludeUsage = true, true, true
			if route.format == types.RelayFormatClaude {
				info.ClaudeConvertInfo = &relaycommon.ClaudeConvertInfo{LastMessagesType: relaycommon.LastMessageTypeNone}
			}
			usage, err := route.handle(c, info, resp)
			require.ErrorIs(t, ctx.Err(), context.Canceled, "fixture must cancel after actual content")
			require.Nil(t, err, "cancelled delivery must retain already reported usage for accounting")
			require.NotNil(t, usage)
			assert.Equal(t, 7, usage.PromptTokens)
			assert.Equal(t, 4, usage.CompletionTokens)
			assert.Equal(t, 11, usage.TotalTokens)
			assert.NotContains(t, recorder.Body.String(), "[DONE]")
			assert.NotContains(t, recorder.Body.String(), `"type":"message_stop"`)
			assert.NotContains(t, recorder.Body.String(), `"type":"response.completed"`)
			if route.name == "native chat final content" {
				// Native content and stop arrived in the same upstream frame;
				// cancellation happens after that frame, before the DONE write.
				assert.Contains(t, recorder.Body.String(), `"finish_reason":"stop"`)
			} else {
				assert.NotContains(t, recorder.Body.String(), `"finish_reason":"stop"`)
			}
		})
	}
}

type convertedFailureWriter struct {
	gin.ResponseWriter
	match              string
	failed             bool
	writesAfterFailure int
	cancel             context.CancelFunc
}

func (w *convertedFailureWriter) Write(p []byte) (int, error) {
	if w.failed {
		w.writesAfterFailure++
	}
	if strings.Contains(string(p), w.match) {
		w.failed = true
		if w.cancel != nil {
			w.cancel()
		}
		return 0, errors.New("synthetic converted stream write failure")
	}
	return w.ResponseWriter.Write(p)
}

func TestConvertedStreamsPropagateDownstreamFailures(t *testing.T) {
	oldDisable := common.AutomaticDisableChannelEnabled
	common.AutomaticDisableChannelEnabled = true
	t.Cleanup(func() { common.AutomaticDisableChannelEnabled = oldDisable })
	chat := []string{
		`{"id":"chatcmpl_fixture","created":1710000000,"model":"fixture","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`,
		`{"id":"chatcmpl_fixture","created":1710000000,"model":"fixture","choices":[{"index":0,"delta":{"content":"EARLY"},"finish_reason":null}]}`,
		`{"id":"chatcmpl_fixture","created":1710000000,"model":"fixture","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_fixture","type":"function","function":{"name":"lookup"}}]},"finish_reason":null}]}`,
		`{"id":"chatcmpl_fixture","created":1710000000,"model":"fixture","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"q\":\"ARG_MARKER\"}"}}]},"finish_reason":null}]}`,
		`{"id":"chatcmpl_fixture","created":1710000000,"model":"fixture","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":7,"completion_tokens":4,"total_tokens":11}}`,
	}
	responses := []string{
		`{"type":"response.created","response":{"id":"resp_fixture","model":"fixture","created_at":1710000000,"status":"in_progress"}}`,
		`{"type":"response.output_text.delta","delta":"EARLY"}`,
		`{"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","id":"fc_fixture","call_id":"call_fixture","name":"lookup"}}`,
		`{"type":"response.function_call_arguments.delta","output_index":1,"delta":"{\"q\":\"ARG_MARKER\"}"}`,
		`{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":7,"output_tokens":4,"total_tokens":11}}}`,
	}
	for _, route := range []struct {
		name     string
		format   types.RelayFormat
		frames   []string
		header   string
		terminal string
		handle   func(*gin.Context, *relaycommon.RelayInfo, *http.Response) (*dto.Usage, *types.NewAPIError)
	}{
		{"chat to Claude", types.RelayFormatClaude, chat, "event: message_start", `"type":"message_stop"`, OaiStreamHandler},
		{"chat to Responses", types.RelayFormatOpenAIResponses, chat, "event: response.created", `"type":"response.completed"`, OaiChatToResponsesStreamHandler},
		{"Responses to chat", types.RelayFormatOpenAI, responses, `"role":"assistant"`, "[DONE]", OaiResponsesToChatStreamHandler},
		{"Responses to Claude", types.RelayFormatClaude, responses, "event: message_start", `"type":"message_stop"`, OaiResponsesToChatStreamHandler},
	} {
		for _, site := range []struct {
			name, match         string
			cancelDuringFailure bool
		}{{"header", route.header, false}, {"text", "EARLY", false}, {"tool arguments", "ARG_MARKER", false}, {"terminal", route.terminal, false}, {"unrelated failure during cancel", "EARLY", true}} {
			t.Run(route.name+"/"+site.name, func(t *testing.T) {
				body := "data: " + strings.Join(route.frames, "\n\ndata: ") + "\n\ndata: [DONE]\n\n"
				c, recorder, info, resp := newUpstreamStreamErrorFixture(t, body, relayconstant.RelayModeChatCompletions)
				c.Set(common.RequestIdKey, "converted-fixture")
				info.RelayFormat = route.format
				info.UpstreamModelName = "fixture"
				info.IsStream, info.DisablePing, info.ShouldIncludeUsage = true, true, true
				if route.format == types.RelayFormatClaude {
					info.ClaudeConvertInfo = &relaycommon.ClaudeConvertInfo{LastMessagesType: relaycommon.LastMessageTypeNone}
				}
				writer := &convertedFailureWriter{ResponseWriter: c.Writer, match: site.match}
				if site.cancelDuringFailure {
					ctx, cancel := context.WithCancel(c.Request.Context())
					defer cancel()
					c.Request = c.Request.WithContext(ctx)
					writer.cancel = cancel
				}
				c.Writer = writer
				usage, err := route.handle(c, info, resp)
				require.True(t, writer.failed, "fixture must reach the selected output write")
				require.NotNil(t, err, "failed delivery cannot return success")
				assert.Nil(t, usage)
				assert.True(t, types.IsSkipRetryError(err), "a downstream failure must never trigger replay")
				assert.False(t, service.ShouldDisableChannel(err))
				assert.Zero(t, writer.writesAfterFailure, "no writes after the first failed event")
				assert.NotContains(t, recorder.Body.String(), route.terminal)
			})
		}
	}
}
