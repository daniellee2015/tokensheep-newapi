package openai

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"

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

// The upstream is deliberately gated, not delayed for a measured duration.
// The deadline only bounds a broken implementation's deadlock. No timing
// percentile or synthetic model-speed assertion is made here.
type contentFlushObserver struct {
	gin.ResponseWriter
	content string
	written strings.Builder
	flushed chan struct{}
}

func TestNativeChatFlushPreservesOrderedFramesAndUsage(t *testing.T) {
	role := `{"id":"chatcmpl_fixture","model":"claude-fixture","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`
	thought := `{"id":"chatcmpl_fixture","model":"claude-fixture","choices":[{"index":0,"delta":{"reasoning_content":"synthetic thought","extension":{"signature":"fixture"}},"finish_reason":null}]}`
	text := `{"id":"chatcmpl_fixture","model":"claude-fixture","choices":[{"index":0,"delta":{"content":"."},"finish_reason":null}]}`
	stop := `{"id":"chatcmpl_fixture","model":"claude-fixture","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`
	usage := `{"id":"chatcmpl_fixture","model":"claude-fixture","choices":[],"usage":{"prompt_tokens":7,"completion_tokens":4,"total_tokens":11,"prompt_tokens_details":{"cached_tokens":3}}}`
	for _, include := range []bool{false, true} {
		t.Run(map[bool]string{false: "usage omitted", true: "usage included"}[include], func(t *testing.T) {
			frames := []string{role, thought, text, stop, usage}
			c, recorder, info, resp := newUpstreamStreamErrorFixture(t, "data: "+strings.Join(frames, "\n\ndata: ")+"\n\ndata: [DONE]\n\n", relayconstant.RelayModeChatCompletions)
			info.IsStream, info.DisablePing, info.ShouldIncludeUsage = true, true, include
			info.UpstreamModelName = "claude-fixture"
			got, relayErr := OaiStreamHandler(c, info, resp)
			require.Nil(t, relayErr)
			require.NotNil(t, got)
			assert.Equal(t, 7, got.PromptTokens)
			assert.Equal(t, 4, got.CompletionTokens)
			assert.Equal(t, 11, got.TotalTokens)
			assert.Equal(t, 3, got.PromptTokensDetails.CachedTokens)
			expected := append([]string{}, frames[:4]...)
			if include {
				expected = append(expected, usage)
			}
			expected = append(expected, "[DONE]")
			var actual []string
			for _, line := range strings.Split(recorder.Body.String(), "\n") {
				if strings.HasPrefix(line, "data: ") {
					actual = append(actual, strings.TrimPrefix(line, "data: "))
				}
			}
			assert.Equal(t, expected, actual, "all content/extension fields, ordering and usage policy must survive")
			assert.NotContains(t, recorder.Body.String(), "<think>")
		})
	}
}

func TestNativeChatDeliveredContentSurvivesLateUpstreamError(t *testing.T) {
	content := `{"id":"chatcmpl_fixture","model":"claude-fixture","choices":[{"index":0,"delta":{"content":"EARLY"},"finish_reason":null}]}`
	body := "data: " + content + "\n\ndata: " + `{"error":{"message":"private upstream failure","type":"provider_private"}}` + "\n\n"
	c, recorder, info, resp := newUpstreamStreamErrorFixture(t, body, relayconstant.RelayModeChatCompletions)
	info.IsStream, info.DisablePing = true, true
	info.UpstreamModelName = "claude-fixture"
	usage, relayErr := OaiStreamHandler(c, info, resp)
	require.Nil(t, usage)
	require.NotNil(t, relayErr)
	assert.Contains(t, recorder.Body.String(), content)
	assert.NotContains(t, recorder.Body.String(), "private upstream failure")
	assert.NotContains(t, recorder.Body.String(), "[DONE]", "controller must finish with an error, not invented success")
}

type contentFlushFailWriter struct {
	gin.ResponseWriter
	match string
}

func (w *contentFlushFailWriter) Write(p []byte) (int, error) {
	if strings.Contains(string(p), w.match) {
		return 0, errors.New("synthetic downstream write failure")
	}
	return w.ResponseWriter.Write(p)
}

func TestNativeChatFlushPropagatesDownstreamWriteFailure(t *testing.T) {
	previousAutoDisable := common.AutomaticDisableChannelEnabled
	common.AutomaticDisableChannelEnabled = true
	t.Cleanup(func() { common.AutomaticDisableChannelEnabled = previousAutoDisable })
	role := `{"id":"chatcmpl_fixture","model":"claude-fixture","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`
	content := `{"id":"chatcmpl_fixture","model":"claude-fixture","choices":[{"index":0,"delta":{"content":"EARLY"},"finish_reason":null}]}`
	last := `{"id":"chatcmpl_fixture","model":"claude-fixture","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
	for _, match := range []string{`"role":"assistant"`, "EARLY", `"usage"`, "[DONE]"} {
		t.Run(match, func(t *testing.T) {
			c, recorder, info, resp := newUpstreamStreamErrorFixture(t, "data: "+strings.Join([]string{role, content, last}, "\n\ndata: ")+"\n\ndata: [DONE]\n\n", relayconstant.RelayModeChatCompletions)
			info.IsStream, info.DisablePing, info.ShouldIncludeUsage = true, true, true
			info.UpstreamModelName = "claude-fixture"
			c.Writer = &contentFlushFailWriter{ResponseWriter: c.Writer, match: match}
			usage, relayErr := OaiStreamHandler(c, info, resp)
			require.Nil(t, usage)
			require.NotNil(t, relayErr)
			assert.True(t, types.IsSkipRetryError(relayErr))
			assert.False(t, service.ShouldDisableChannel(relayErr), "downstream failure must not disable a healthy upstream channel")
			assert.NotContains(t, recorder.Body.String(), "[DONE]")
			if match == `"role":"assistant"` || match == "EARLY" {
				assert.True(t, info.ContentTiming.Snapshot().Downstream.FirstText.IsZero(), "failed write is not delivered content")
			}
		})
	}
}

func TestNativeChatFlushRetainsFallbackUsageAndItsWriteFailure(t *testing.T) {
	content := `{"id":"chatcmpl_fixture","model":"claude-fixture","choices":[{"index":0,"delta":{"content":"answer"},"finish_reason":null}]}`
	last := `{"id":"chatcmpl_fixture","model":"claude-fixture","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`
	for _, failWrite := range []bool{false, true} {
		t.Run(map[bool]string{false: "usage fallback", true: "failed fallback write"}[failWrite], func(t *testing.T) {
			c, recorder, info, resp := newUpstreamStreamErrorFixture(t, "data: "+content+"\n\ndata: "+last+"\n\ndata: [DONE]\n\n", relayconstant.RelayModeChatCompletions)
			info.IsStream, info.DisablePing, info.ShouldIncludeUsage = true, true, true
			info.UpstreamModelName = "claude-fixture"
			info.SetEstimatePromptTokens(7)
			if failWrite {
				c.Writer = &contentFlushFailWriter{ResponseWriter: c.Writer, match: `"usage"`}
			}
			usage, relayErr := OaiStreamHandler(c, info, resp)
			if failWrite {
				require.Nil(t, usage)
				require.NotNil(t, relayErr)
				assert.True(t, types.IsSkipRetryError(relayErr))
				assert.NotContains(t, recorder.Body.String(), "[DONE]")
				return
			}
			require.Nil(t, relayErr)
			require.NotNil(t, usage)
			assert.Equal(t, 7, usage.PromptTokens)
			assert.Equal(t, service.CountTextToken("answer", "claude-fixture"), usage.CompletionTokens)
			assert.Equal(t, usage.PromptTokens+usage.CompletionTokens, usage.TotalTokens)
			assert.Equal(t, 1, strings.Count(recorder.Body.String(), content))
			assert.Equal(t, 1, strings.Count(recorder.Body.String(), `"usage"`))
			assert.Equal(t, 1, strings.Count(recorder.Body.String(), "data: [DONE]"))
		})
	}
}

// Preserve the separate legacy formatting/audio branches. Exact expected
// snapshots are collected independently through the pre-patch handler by the
// private differential runner, not generated by assertions in this test.
func TestNativeChatFlushOtherModes(t *testing.T) {
	for _, mode := range []string{"audio", "force-format", "thinking-to-content"} {
		t.Run(mode, func(t *testing.T) {
			body := "data: " + `{"id":"chatcmpl_fixture","model":"fixture","choices":[{"index":0,"delta":{"reasoning_content":"synthetic"},"finish_reason":null}]}` + "\n\ndata: " + `{"id":"chatcmpl_fixture","model":"fixture","choices":[{"index":0,"delta":{"content":"answer"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}` + "\n\ndata: [DONE]\n\n"
			c, recorder, info, resp := newUpstreamStreamErrorFixture(t, body, relayconstant.RelayModeChatCompletions)
			info.IsStream, info.DisablePing, info.ShouldIncludeUsage = true, true, true
			info.UpstreamModelName = "fixture"
			if mode == "audio" {
				info.UpstreamModelName = "fixture-audio"
			}
			info.ChannelSetting.ForceFormat = mode == "force-format"
			info.ChannelSetting.ThinkingToContent = mode == "thinking-to-content"
			info.ThinkingContentInfo = relaycommon.ThinkingContentInfo{IsFirstThinkingContent: true}
			usage, relayErr := OaiStreamHandler(c, info, resp)
			require.Nil(t, relayErr)
			require.NotNil(t, usage)
			assert.Equal(t, 2, usage.TotalTokens)
			assert.Equal(t, 1, strings.Count(recorder.Body.String(), "data: [DONE]"))
			if mode != "thinking-to-content" {
				assert.NotContains(t, recorder.Body.String(), "<think>")
			}
			// Parse every data record so a formatting regression cannot pass on
			// a substring found inside invalid JSON.
			for _, line := range strings.Split(recorder.Body.String(), "\n") {
				if strings.HasPrefix(line, "data: ") && line != "data: [DONE]" {
					var parsed map[string]any
					require.NoError(t, common.UnmarshalJsonStr(strings.TrimPrefix(line, "data: "), &parsed))
				}
			}
		})
	}
}

func (w *contentFlushObserver) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	w.written.Write(p[:n])
	return n, err
}

func (w *contentFlushObserver) Flush() {
	w.ResponseWriter.Flush()
	if strings.Contains(w.written.String(), w.content) {
		select {
		case w.flushed <- struct{}{}:
		default:
		}
	}
}

func TestNativeChatContentFlushesBeforeNextUpstreamEvent(t *testing.T) {
	for name, delta := range map[string]string{
		"text":      `{"content":"EARLY","vendor_extension":{"signed":"synthetic"}}`,
		"reasoning": `{"reasoning_content":"EARLY","vendor_extension":{"signed":"synthetic"}}`,
		"tool":      `{"tool_calls":[{"index":0,"id":"call_fixture","type":"function","function":{"name":"lookup_city","arguments":"{\"city\":\"EARLY\"}"}}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			c, recorder, info, resp := newUpstreamStreamErrorFixture(t, "", relayconstant.RelayModeChatCompletions)
			info.IsStream, info.DisablePing, info.ShouldIncludeUsage = true, true, true
			info.UpstreamModelName = "claude-fixture"
			info.StartTime = time.Now()
			role := `{"id":"chatcmpl_fixture","model":"claude-fixture","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`
			content := `{"id":"chatcmpl_fixture","model":"claude-fixture","choices":[{"index":0,"delta":` + delta + `,"finish_reason":null}]}`
			end := `{"id":"chatcmpl_fixture","model":"claude-fixture","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
			observer := &contentFlushObserver{ResponseWriter: c.Writer, content: content, flushed: make(chan struct{}, 1)}
			c.Writer = observer
			reader, writer := io.Pipe()
			resp.Body = reader
			release := make(chan struct{})
			producerDone := make(chan struct{})
			go func() {
				defer close(producerDone)
				defer writer.Close()
				_, _ = io.WriteString(writer, "data: "+role+"\n\ndata: "+content+"\n\n")
				<-release
				_, _ = io.WriteString(writer, "data: "+end+"\n\ndata: [DONE]\n\n")
			}()
			type result struct {
				usage *dto.Usage
				err   *types.NewAPIError
			}
			done := make(chan result, 1)
			go func() { usage, err := OaiStreamHandler(c, info, resp); done <- result{usage, err} }()
			beforeRelease := false
			select {
			case <-observer.flushed:
				beforeRelease = true
			case <-time.After(2 * time.Second):
			}
			close(release)
			var got result
			select {
			case got = <-done:
			case <-time.After(5 * time.Second):
				_ = reader.Close()
				_ = writer.Close()
				t.Fatal("handler did not finish after releasing upstream")
			}
			<-producerDone
			require.Nil(t, got.err)
			require.NotNil(t, got.usage)
			assert.Equal(t, 2, got.usage.TotalTokens)
			assert.Equal(t, 1, strings.Count(recorder.Body.String(), content), "content must be exact and emitted once")
			assert.Equal(t, 1, strings.Count(recorder.Body.String(), end), "terminal metadata/usage must be retained once")
			assert.Equal(t, 1, strings.Count(recorder.Body.String(), "data: [DONE]"))
			assert.True(t, beforeRelease, "already-arrived content was held until another upstream event")
		})
	}
}
