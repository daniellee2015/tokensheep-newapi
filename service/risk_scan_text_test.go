package service

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The keyword we plant inside "poisoned" transcript regions. Any keyword
// would do; using an unmistakable literal makes assertion failures obvious.
const scanTriggerWord = "台独"

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

func stringPtr(s string) *string { return &s }

func TestExtractRiskScanText_NilRequest(t *testing.T) {
	got := ExtractRiskScanText(nil)
	assert.Empty(t, got)
}

// Claude Code sends the whole transcript on every turn: system prompt +
// prior user/assistant messages + every tool_use call the model made +
// every tool_result the tools returned (i.e. file contents that were Read).
// Only the fresh user text should be scanned; everything else must be
// excluded, otherwise a single poisoned file permanently kills the session.
func TestExtractRiskScanText_ClaudeExcludesToolResultAndSystem(t *testing.T) {
	req := &dto.ClaudeRequest{
		System: "You are a helpful assistant. Never discuss " + scanTriggerWord,
		Messages: []dto.ClaudeMessage{
			{
				Role: "user",
				Content: []dto.ClaudeMediaMessage{
					{Type: "text", Text: stringPtr("please read main.go")},
				},
			},
			{
				Role: "assistant",
				Content: []dto.ClaudeMediaMessage{
					{Type: "text", Text: stringPtr("sure — mentioning " + scanTriggerWord + " to see if I get through")},
					{Type: "tool_use", Id: "call_1", Name: "Read", Input: map[string]any{"path": "main.go"}},
				},
			},
			{
				Role: "user",
				Content: []dto.ClaudeMediaMessage{
					{
						Type:      "tool_result",
						ToolUseId: "call_1",
						// The file the tool returned contains the trigger word.
						// This is the exact "poisoned session" scenario.
						Content: "package main\n// mentions " + scanTriggerWord + " in a comment",
					},
					{Type: "text", Text: stringPtr("what does the file say?")},
				},
			},
		},
	}

	got := ExtractRiskScanText(req)

	assert.Contains(t, got, "please read main.go")
	assert.Contains(t, got, "what does the file say?")
	assert.NotContains(t, got, scanTriggerWord, "tool_result / assistant / system must NOT be scanned")
}

// tool_use content is model-authored (arguments to the tool call). Even if
// they appear inside a user-role message via ParseContent, only "text"
// blocks may be scanned.
func TestExtractRiskScanText_ClaudeSkipsToolUseInsideUserRole(t *testing.T) {
	req := &dto.ClaudeRequest{
		Messages: []dto.ClaudeMessage{
			{
				Role: "user",
				Content: []dto.ClaudeMediaMessage{
					{Type: "tool_use", Name: scanTriggerWord, Input: map[string]any{"x": scanTriggerWord}},
					{Type: "text", Text: stringPtr("normal question")},
				},
			},
		},
	}

	got := ExtractRiskScanText(req)

	assert.Equal(t, "normal question", got)
}

// tool role messages carry function/tool output. Even though the array
// representation is similar to a user message, they must be skipped.
func TestExtractRiskScanText_OpenAIExcludesToolRole(t *testing.T) {
	req := &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{
			{Role: "system", Content: "system prompt mentioning " + scanTriggerWord},
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi — leaking " + scanTriggerWord + " here"},
			{
				Role:       "tool",
				ToolCallId: "call_1",
				// A tool returned a file that contains the trigger word.
				Content: "file body: " + scanTriggerWord,
			},
			{Role: "user", Content: "please summarize the file"},
		},
	}

	got := ExtractRiskScanText(req)

	assert.Contains(t, got, "hello")
	assert.Contains(t, got, "please summarize the file")
	assert.NotContains(t, got, scanTriggerWord)
}

// OpenAI multi-modal content: only pieces whose type == "text" count as
// user input; image_url and others must not be scanned as text.
func TestExtractRiskScanText_OpenAIMultiModalUserOnlyText(t *testing.T) {
	req := &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{
			{
				Role: "user",
				Content: []any{
					map[string]any{"type": "text", "text": "user typed question"},
					map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/x.png"}},
				},
			},
		},
	}

	got := ExtractRiskScanText(req)
	assert.Equal(t, "user typed question", got)
}

// Gemini's functionResponse parts have Text == "" naturally, so they are
// excluded already; explicit "thought" parts must also be skipped so that
// upstream reasoning replayed back to us cannot poison future turns.
func TestExtractRiskScanText_GeminiExcludesFunctionResponseAndThought(t *testing.T) {
	req := &dto.GeminiChatRequest{
		Contents: []dto.GeminiChatContent{
			{
				Role: "user",
				Parts: []dto.GeminiPart{
					{Text: "user question"},
				},
			},
			{
				Role: "model",
				Parts: []dto.GeminiPart{
					{Text: "assistant reply mentioning " + scanTriggerWord},
					{Thought: true, Text: "internal thought: " + scanTriggerWord},
				},
			},
			{
				// functionResponse turns: the raw payload isn't in Part.Text,
				// but we set Part.Text here to prove it would still be
				// filtered by role even if a client shoved text in.
				Role: "function",
				Parts: []dto.GeminiPart{
					{Text: "tool output: " + scanTriggerWord},
				},
			},
		},
	}

	got := ExtractRiskScanText(req)
	assert.Equal(t, "user question", got)
	assert.NotContains(t, got, scanTriggerWord)
}

// Responses API is the most dangerous case: Instructions (system prompt)
// and Tools (tool definitions) are the same on every request in a session.
// If either contains a trigger word, every single request would be blocked
// forever — even starting a new conversation wouldn't help. They must be
// excluded, along with function_call_output items in Input.
func TestExtractRiskScanText_ResponsesExcludesInstructionsToolsAndFunctionOutput(t *testing.T) {
	inputItems := []any{
		map[string]any{
			"type": "message",
			"role": "user",
			"content": []any{
				map[string]any{"type": "input_text", "text": "user question"},
			},
		},
		map[string]any{
			"type":    "function_call",
			"name":    "read_file",
			"call_id": "c1",
			// If the model called a tool with an argument that contains
			// the trigger word, that must NOT get us blocked.
			"arguments": `{"path":"` + scanTriggerWord + `"}`,
		},
		map[string]any{
			"type":    "function_call_output",
			"call_id": "c1",
			// Tool output containing the trigger word (Read of a file
			// that discusses it). This is the classic poisoning vector.
			"output": "file body about " + scanTriggerWord,
		},
		map[string]any{
			"type": "message",
			"role": "assistant",
			"content": []any{
				map[string]any{"type": "output_text", "text": "assistant leak: " + scanTriggerWord},
			},
		},
		map[string]any{
			"type": "message",
			"role": "user",
			"content": []any{
				map[string]any{"type": "input_text", "text": "please continue"},
			},
		},
	}
	req := &dto.OpenAIResponsesRequest{
		Instructions: mustJSON(t, "system prompt mentioning "+scanTriggerWord),
		Tools:        mustJSON(t, []any{map[string]any{"type": "function", "name": scanTriggerWord}}),
		Input:        mustJSON(t, inputItems),
	}

	got := ExtractRiskScanText(req)

	assert.Contains(t, got, "user question")
	assert.Contains(t, got, "please continue")
	assert.NotContains(t, got, scanTriggerWord, "instructions/tools/function_call_output/assistant must all be excluded")
}

// Responses Input can also be a bare string — it is entirely user text.
func TestExtractRiskScanText_ResponsesInputAsPlainString(t *testing.T) {
	req := &dto.OpenAIResponsesRequest{
		Input: mustJSON(t, "just a plain user prompt"),
	}
	got := ExtractRiskScanText(req)
	assert.Equal(t, "just a plain user prompt", got)
}

// Fallback path: request types that don't override user-input extraction
// (audio, embeddings, rerank, image, alpha_search, ...) fall back to
// CombineText from their GetTokenCountMeta. Those requests carry a single
// prompt with no history and no tool output, so CombineText already equals
// user input.
func TestExtractRiskScanText_FallbackUsesCombineText(t *testing.T) {
	req := &dto.AlphaSearchRequest{
		Model:   "gpt-search",
		RawBody: json.RawMessage(`{"q":"hello"}`),
	}
	got := ExtractRiskScanText(req)
	assert.Equal(t, `{"q":"hello"}`, got)
}

// The compaction request is the model's own conversation-summary path —
// it carries prior assistant/tool history, not fresh user text. Scanning
// it would re-introduce exactly the poisoning bug we're fixing.
func TestExtractRiskScanText_ResponsesCompactionSkipped(t *testing.T) {
	req := &dto.OpenAIResponsesCompactionRequest{
		Instructions: mustJSON(t, "old system prompt with "+scanTriggerWord),
		Input:        mustJSON(t, "old transcript with "+scanTriggerWord),
	}
	got := ExtractRiskScanText(req)
	assert.Empty(t, got)
}
