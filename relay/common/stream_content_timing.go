package common

import (
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// ContentTimeline stores timestamps only; it never retains message contents or
// signatures. Upstream arrival and downstream flush are distinct observations.
type ContentTimeline struct {
	Supported     bool
	Unknown       bool
	FirstText     time.Time
	FirstThinking time.Time
	FirstTool     time.Time
}

func (t ContentTimeline) FirstContent() time.Time {
	var first time.Time
	for _, at := range []time.Time{t.FirstText, t.FirstThinking, t.FirstTool} {
		if !at.IsZero() && (first.IsZero() || at.Before(first)) {
			first = at
		}
	}
	return first
}

type ContentTimingSnapshot struct{ Upstream, Downstream ContentTimeline }

type StreamContentTiming struct {
	mu    sync.Mutex
	value ContentTimingSnapshot
}

func (info *RelayInfo) EnsureContentTiming(c *gin.Context) {
	if info.ContentTiming == nil {
		info.ContentTiming = &StreamContentTiming{}
	}
	if c != nil {
		c.Set(string(constant.ContextKeyStreamContentTiming), info.ContentTiming)
	}
}

func (s *StreamContentTiming) Snapshot() ContentTimingSnapshot {
	if s == nil {
		return ContentTimingSnapshot{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.value
}

func (s *StreamContentTiming) Observe(data string, downstream bool, at time.Time) {
	if s == nil || data == "[DONE]" || strings.TrimSpace(data) == "" {
		return
	}
	kind := classifyStreamContent(data)
	s.mu.Lock()
	defer s.mu.Unlock()
	timeline := &s.value.Upstream
	if downstream {
		timeline = &s.value.Downstream
	}
	timeline.Supported = timeline.Supported || kind.supported
	timeline.Unknown = timeline.Unknown || !kind.supported
	if kind.text && timeline.FirstText.IsZero() {
		timeline.FirstText = at
	}
	if kind.thinking && timeline.FirstThinking.IsZero() {
		timeline.FirstThinking = at
	}
	if kind.tool && timeline.FirstTool.IsZero() {
		timeline.FirstTool = at
	}
}

type streamContentKind struct{ supported, text, thinking, tool bool }

func classifyStreamContent(data string) streamContentKind {
	var kind streamContentKind
	if !gjson.Valid(data) {
		return kind
	}
	root := gjson.Parse(data)
	nonempty := func(v gjson.Result) bool { return v.Type == gjson.String && strings.TrimSpace(v.Str) != "" }
	markBlock := func(block gjson.Result) {
		switch block.Get("type").Str {
		case "text", "output_text":
			kind.text = kind.text || nonempty(block.Get("text"))
		case "refusal":
			kind.text = kind.text || nonempty(block.Get("refusal"))
		case "thinking":
			kind.thinking = kind.thinking || nonempty(block.Get("thinking"))
		case "reasoning_text", "summary_text":
			kind.thinking = kind.thinking || nonempty(block.Get("text"))
		case "tool_use", "server_tool_use", "function_call":
			kind.tool = kind.tool || nonempty(block.Get("name")) || nonempty(block.Get("arguments"))
		}
	}
	typ := root.Get("type").Str
	switch typ {
	case "message_start", "message_delta", "message_stop", "content_block_stop", "ping", "error":
		kind.supported = true
	case "content_block_start":
		kind.supported = true
		markBlock(root.Get("content_block"))
	case "content_block_delta":
		kind.supported = true
		delta := root.Get("delta")
		switch delta.Get("type").Str {
		case "text_delta":
			kind.text = nonempty(delta.Get("text"))
		case "thinking_delta":
			kind.thinking = nonempty(delta.Get("thinking"))
		case "input_json_delta":
			kind.tool = nonempty(delta.Get("partial_json"))
		}
	case "response.created", "response.in_progress", "response.queued", "response.failed", "response.incomplete", "response.output_text.annotation.added":
		kind.supported = true
	case "response.output_text.delta", "response.refusal.delta":
		kind.supported = true
		kind.text = nonempty(root.Get("delta"))
	case "response.reasoning_text.delta", "response.reasoning_summary_text.delta":
		kind.supported = true
		kind.thinking = nonempty(root.Get("delta"))
	case "response.function_call_arguments.delta":
		kind.supported = true
		kind.tool = nonempty(root.Get("delta"))
	case "response.output_text.done":
		kind.supported = true
		kind.text = nonempty(root.Get("text"))
	case "response.refusal.done":
		kind.supported = true
		kind.text = nonempty(root.Get("refusal"))
	case "response.content_part.added", "response.reasoning_summary_part.added", "response.content_part.done", "response.reasoning_summary_part.done":
		kind.supported = true
		markBlock(root.Get("part"))
	case "response.output_item.added", "response.output_item.done":
		kind.supported = true
		markBlock(root.Get("item"))
		for _, block := range root.Get("item.content").Array() {
			markBlock(block)
		}
		for _, block := range root.Get("item.summary").Array() {
			markBlock(block)
		}
	case "response.reasoning_summary_text.done", "response.reasoning_text.done":
		kind.supported = true
		kind.thinking = nonempty(root.Get("text"))
	case "response.function_call_arguments.done":
		kind.supported = true
		kind.tool = nonempty(root.Get("arguments"))
	case "response.completed":
		kind.supported = true
		for _, item := range root.Get("response.output").Array() {
			markBlock(item)
			for _, block := range item.Get("content").Array() {
				markBlock(block)
			}
			for _, block := range item.Get("summary").Array() {
				markBlock(block)
			}
		}
	default:
		choices := root.Get("choices")
		if choices.IsArray() {
			kind.supported = true
			for _, choice := range choices.Array() {
				delta := choice.Get("delta")
				kind.text = kind.text || nonempty(delta.Get("content")) || nonempty(delta.Get("refusal"))
				for _, block := range delta.Get("content").Array() {
					markBlock(block)
				}
				kind.thinking = kind.thinking || nonempty(delta.Get("reasoning_content")) || nonempty(delta.Get("reasoning"))
				for _, call := range delta.Get("tool_calls").Array() {
					kind.tool = kind.tool || nonempty(call.Get("function.name")) || nonempty(call.Get("function.arguments"))
				}
				kind.tool = kind.tool || nonempty(delta.Get("function_call.name")) || nonempty(delta.Get("function_call.arguments"))
			}
		}
	}
	return kind
}
