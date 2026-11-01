package service

import (
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func TestLogContentTimingPreservesUnknownAndNonstreamSemantics(t *testing.T) {
	start := time.Unix(1000, 0)
	for _, stream := range []bool{false, true} {
		info := &relaycommon.RelayInfo{IsStream: stream, StartTime: start, FirstResponseTime: start.Add(100 * time.Millisecond)}
		info.EnsureContentTiming(nil)
		info.ContentTiming.Observe(`{"candidates":[{"content":{"parts":[{"text":"answer"}]}}]}`, true, start.Add(300*time.Millisecond))
		other := map[string]interface{}{"frt": float64(100)}
		appendStreamContentTiming(info, other)
		assert.Equal(t, float64(100), other["frt"])
		assert.NotContains(t, other, "first_text_ms")
		if stream {
			assert.Equal(t, "upstream_first_event", other["frt_metric"])
		} else {
			assert.NotContains(t, other, "frt_metric")
		}
	}
}

func TestLogContentTimingSeparatesEventThinkingTextAndTool(t *testing.T) {
	start := time.Unix(1000, 0)
	info := &relaycommon.RelayInfo{IsStream: true, StartTime: start, FirstResponseTime: start.Add(100 * time.Millisecond)}
	info.EnsureContentTiming(nil)
	thought := `{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"reason"}}`
	info.ContentTiming.Observe(thought, false, start.Add(500*time.Millisecond))
	info.ContentTiming.Observe(thought, true, start.Add(600*time.Millisecond))
	info.ContentTiming.Observe(`{"type":"content_block_start","content_block":{"type":"tool_use","name":"lookup"}}`, true, start.Add(700*time.Millisecond))
	info.ContentTiming.Observe(`{"type":"content_block_delta","delta":{"type":"text_delta","text":"answer"}}`, true, start.Add(900*time.Millisecond))
	other := map[string]interface{}{"frt": float64(100), "model_ratio": float64(1), "cache_tokens": 100}
	appendStreamContentTiming(info, other)
	assert.Equal(t, float64(600), other["frt"])
	assert.Equal(t, float64(100), other["frt_event_ms"])
	assert.Equal(t, float64(500), other["upstream_first_content_ms"])
	assert.Equal(t, float64(600), other["first_thinking_ms"])
	assert.Equal(t, float64(700), other["first_tool_ms"])
	assert.Equal(t, float64(900), other["first_text_ms"])
	assert.Equal(t, float64(1), other["model_ratio"])
	assert.Equal(t, 100, other["cache_tokens"])
}
