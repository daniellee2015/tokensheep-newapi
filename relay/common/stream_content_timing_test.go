package common

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestStreamContentTimingProtocolShapes(t *testing.T) {
	cases := []struct {
		name, data, kind string
		supported        bool
	}{
		{"message_start", `{"type":"message_start","message":{"content":[]}}`, "", true},
		{"empty_block", `{"type":"content_block_start","content_block":{"type":"text","text":""}}`, "", true},
		{"signature", `{"type":"content_block_delta","delta":{"type":"signature_delta","signature":"synthetic"}}`, "", true},
		{"redacted", `{"type":"content_block_start","content_block":{"type":"redacted_thinking","data":"synthetic"}}`, "", true},
		{"empty_text", `{"type":"content_block_delta","delta":{"type":"text_delta","text":""}}`, "", true},
		{"whitespace", `{"type":"content_block_delta","delta":{"type":"text_delta","text":"\n "}}`, "", true},
		{"period", `{"type":"content_block_delta","delta":{"type":"text_delta","text":"."}}`, "text", true},
		{"chinese_period", `{"type":"content_block_delta","delta":{"type":"text_delta","text":"。"}}`, "text", true},
		{"thinking", `{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"reason"}}`, "thinking", true},
		{"thinking_start", `{"type":"content_block_start","content_block":{"type":"thinking","thinking":"reason"}}`, "thinking", true},
		{"tool_start", `{"type":"content_block_start","content_block":{"type":"tool_use","id":"toolu_fixture","name":"lookup","input":{}}}`, "tool", true},
		{"tool_args", `{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{}"}}`, "tool", true},
		{"usage", `{"type":"message_delta","usage":{"output_tokens":4}}`, "", true},
		{"error", `{"type":"error","error":{"type":"overloaded_error"}}`, "", true},
		{"chat_role", `{"choices":[{"delta":{"role":"assistant","content":null}}]}`, "", true},
		{"chat_ok", `{"choices":[{"delta":{"content":"OK"}}]}`, "text", true},
		{"chat_array", `{"choices":[{"delta":{"content":[{"type":"text","text":"answer"}]}}]}`, "text", true},
		{"chat_reasoning", `{"choices":[{"delta":{"reasoning_content":"reason"}}]}`, "thinking", true},
		{"chat_reasoning_alias", `{"choices":[{"delta":{"reasoning":"reason"}}]}`, "thinking", true},
		{"chat_tool", `{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"lookup","arguments":""}}]}}]}`, "tool", true},
		{"chat_legacy_tool", `{"choices":[{"delta":{"function_call":{"arguments":"{}"}}}]}`, "tool", true},
		{"chat_usage", `{"choices":[],"usage":{"completion_tokens":4}}`, "", true},
		{"responses_created", `{"type":"response.created","response":{"output":[]}}`, "", true},
		{"responses_item_empty", `{"type":"response.output_item.added","item":{"type":"message","content":[]}}`, "", true},
		{"responses_text", `{"type":"response.output_text.delta","delta":"answer"}`, "text", true},
		{"responses_refusal", `{"type":"response.refusal.delta","delta":"refused"}`, "text", true},
		{"responses_thinking", `{"type":"response.reasoning_summary_text.delta","delta":"reason"}`, "thinking", true},
		{"responses_raw_thinking", `{"type":"response.reasoning_text.delta","delta":"reason"}`, "thinking", true},
		{"responses_tool", `{"type":"response.output_item.added","item":{"type":"function_call","name":"lookup","arguments":""}}`, "tool", true},
		{"responses_args", `{"type":"response.function_call_arguments.delta","delta":"{}"}`, "tool", true},
		{"responses_complete_empty", `{"type":"response.completed","response":{"output":[]}}`, "", true},
		{"responses_done_content", `{"type":"response.output_item.done","item":{"type":"message","content":[{"type":"output_text","text":"answer"}]}}`, "text", true},
		{"responses_complete_content", `{"type":"response.completed","response":{"output":[{"type":"message","content":[{"type":"output_text","text":"answer"}]}]}}`, "text", true},
		{"invalid", `not-json`, "", false},
		{"gemini_unmeasured", `{"candidates":[{"content":{"parts":[{"text":"answer"}]}}]}`, "", false},
		{"audio_unmeasured", `{"type":"response.audio.delta","delta":"synthetic"}`, "", false},
	}
	at := time.Unix(1000, 0)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			timing := &StreamContentTiming{}
			timing.Observe(tc.data, true, at)
			got := timing.Snapshot().Downstream
			assert.Equal(t, tc.supported, got.Supported)
			assert.Equal(t, !tc.supported, got.Unknown)
			assert.Equal(t, tc.kind == "text", !got.FirstText.IsZero())
			assert.Equal(t, tc.kind == "thinking", !got.FirstThinking.IsZero())
			assert.Equal(t, tc.kind == "tool", !got.FirstTool.IsZero())
			if tc.kind != "" {
				assert.Equal(t, at, got.FirstContent())
			}
		})
	}
}

func TestStreamContentTimingKeepsIndependentFirstOccurrences(t *testing.T) {
	start := time.Unix(1000, 0)
	timing := &StreamContentTiming{}
	timing.Observe(`{"type":"message_start"}`, true, start)
	thought := `{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"reason"}}`
	text := `{"type":"content_block_delta","delta":{"type":"text_delta","text":"answer"}}`
	timing.Observe(thought, false, start.Add(100*time.Millisecond))
	timing.Observe(text, false, start.Add(300*time.Millisecond))
	timing.Observe(text, true, start.Add(350*time.Millisecond))
	timing.Observe(text, true, start.Add(500*time.Millisecond))
	timing.Observe(`{"type":"content_block_start","content_block":{"type":"tool_use","name":"lookup"}}`, true, start.Add(600*time.Millisecond))
	timing.Observe(`[DONE]`, true, start.Add(time.Second))
	snapshot := timing.Snapshot()
	require.True(t, snapshot.Downstream.Supported)
	assert.False(t, snapshot.Downstream.Unknown)
	assert.True(t, snapshot.Downstream.FirstThinking.IsZero(), "suppressed upstream reasoning is not delivered")
	assert.Equal(t, start.Add(100*time.Millisecond), snapshot.Upstream.FirstContent())
	assert.Equal(t, start.Add(350*time.Millisecond), snapshot.Downstream.FirstContent())
	assert.Equal(t, start.Add(350*time.Millisecond), snapshot.Downstream.FirstText)
	assert.Equal(t, start.Add(600*time.Millisecond), snapshot.Downstream.FirstTool)
}
