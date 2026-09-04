package service

import (
	"encoding/json"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

// ExtractRiskScanText returns the text that should be fed into the sensitive-word
// scanner. It intentionally scans ONLY the text the caller (end user) wrote in
// this request, and drops everything the model or a tool produced.
//
// Why this exists (see docs/attachments/risk-control-strategy-and-porting.md):
// The old code fed `TokenCountMeta.CombineText` into CheckSensitiveText.
// CombineText is built for token counting and therefore includes:
//
//   - system prompts
//   - the entire conversation history (each round resends it)
//   - `tool_use` / `tool_calls` arguments (file paths, shell commands, ...)
//   - `tool_result` / `role=tool` payloads (i.e. the *contents* of every file
//     Claude Code has ever Read in this session)
//   - tool definitions (name, description, JSON schema)
//
// So if any of that transcript happens to contain a sensitive keyword (very
// easy inside this repository, where the sensitive-word list itself lives in
// source), the request is rejected. Because the client keeps resending the
// full transcript on every turn, once a session is poisoned it stays broken
// forever — matching the "server keeps rejecting me, only fixed by starting a
// new session" symptom users reported.
//
// The fix is to extract only what the user actually typed in this request.
// For request types that only carry a single user prompt (audio, embeddings,
// rerank, image, ...) there is no history and no tool output, so CombineText
// already equals the user input and we fall back to it.
func ExtractRiskScanText(request dto.Request) string {
	if request == nil {
		return ""
	}

	switch r := request.(type) {
	case *dto.ClaudeRequest:
		return extractClaudeUserText(r)
	case *dto.GeneralOpenAIRequest:
		return extractOpenAIUserText(r)
	case *dto.GeminiChatRequest:
		return extractGeminiUserText(r)
	case *dto.OpenAIResponsesRequest:
		return extractResponsesUserText(r)
	case *dto.OpenAIResponsesCompactionRequest:
		// Compaction requests are the model's own conversation-summary path;
		// they carry no fresh user input, so skip them.
		return ""
	}

	// Fallback for single-shot request formats (audio, embeddings, rerank,
	// image, alpha_search, ...). Their CombineText is already user input.
	if meta := request.GetTokenCountMeta(); meta != nil {
		return meta.CombineText
	}
	return ""
}

// extractClaudeUserText only collects text from user-role messages, and only
// media blocks whose Type is "text". It skips tool_use (model output),
// tool_result (tool/file playback), system prompts, and tool definitions.
func extractClaudeUserText(r *dto.ClaudeRequest) string {
	if r == nil {
		return ""
	}
	var parts []string
	for _, message := range r.Messages {
		if message.Role != "user" {
			continue
		}
		if message.IsStringContent() {
			if s := message.GetStringContent(); s != "" {
				parts = append(parts, s)
			}
			continue
		}
		content, err := message.ParseContent()
		if err != nil {
			continue
		}
		for _, media := range content {
			if media.Type != "text" {
				continue
			}
			if s := media.GetText(); s != "" {
				parts = append(parts, s)
			}
		}
	}
	return strings.Join(parts, "\n")
}

// extractOpenAIUserText only collects text from user-role messages. It
// explicitly excludes role=="tool" (function/tool call output), assistant
// replies, tool definitions, and Prompt/Input which are used by legacy
// completion endpoints (those go through CombineText fallback if needed).
func extractOpenAIUserText(r *dto.GeneralOpenAIRequest) string {
	if r == nil {
		return ""
	}
	var parts []string
	for i := range r.Messages {
		message := &r.Messages[i]
		if message.Role != "user" {
			continue
		}
		if message.Content == nil {
			continue
		}
		for _, m := range message.ParseContent() {
			if m.Type != dto.ContentTypeText {
				continue
			}
			if m.Text != "" {
				parts = append(parts, m.Text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

// extractGeminiUserText only collects Part.Text from user-role Content
// entries. Parts holding functionResponse have Text == "" naturally, and
// thought parts are skipped explicitly.
func extractGeminiUserText(r *dto.GeminiChatRequest) string {
	if r == nil {
		return ""
	}
	var parts []string
	for _, content := range r.Contents {
		if content.Role != "" && content.Role != "user" {
			continue
		}
		for _, part := range content.Parts {
			if part.Thought {
				continue
			}
			if part.Text != "" {
				parts = append(parts, part.Text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

// extractResponsesUserText walks r.Input directly instead of using
// ParseInput(), because ParseInput() drops the role/type distinction and
// mixes user input with function_call_output. It keeps only entries where
// type == "message" && role == "user", and their text parts. It deliberately
// skips r.Instructions (system prompt) and r.Tools (tool definitions);
// leaving them in would poison every request in the session the moment the
// client's system prompt happened to contain a keyword.
func extractResponsesUserText(r *dto.OpenAIResponsesRequest) string {
	if r == nil || len(r.Input) == 0 {
		return ""
	}

	// Input can be a bare string.
	if kitutil.GetJsonType(r.Input) == "string" {
		var s string
		if err := kitutil.Unmarshal(r.Input, &s); err == nil {
			return s
		}
		return ""
	}

	if kitutil.GetJsonType(r.Input) != "array" {
		return ""
	}

	var items []responsesInputItem
	if err := kitutil.Unmarshal(r.Input, &items); err != nil {
		return ""
	}

	var parts []string
	for _, item := range items {
		// Skip anything that isn't a user-authored message. In particular:
		//   - type == "function_call" / "function_call_output" (tool traffic)
		//   - role == "assistant" / "system" / "developer"
		if item.Type != "" && item.Type != "message" {
			continue
		}
		if item.Role != "user" {
			continue
		}
		if len(item.Content) == 0 {
			continue
		}
		if kitutil.GetJsonType(item.Content) == "string" {
			var s string
			if err := kitutil.Unmarshal(item.Content, &s); err == nil && s != "" {
				parts = append(parts, s)
			}
			continue
		}
		if kitutil.GetJsonType(item.Content) != "array" {
			continue
		}
		var pieces []responsesInputContentPiece
		if err := kitutil.Unmarshal(item.Content, &pieces); err != nil {
			continue
		}
		for _, piece := range pieces {
			// input_text is what the user typed; input_image / input_file are
			// media (handled elsewhere for billing) and carry no scannable
			// text.
			if piece.Type != "" && piece.Type != "input_text" && piece.Type != "text" {
				continue
			}
			if piece.Text != "" {
				parts = append(parts, piece.Text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

// Local structs used only for user-input extraction. Kept private so nobody
// mistakes them for the request-shaped DTOs in relaykit/dto.
type responsesInputItem struct {
	Type    string          `json:"type,omitempty"`
	Role    string          `json:"role,omitempty"`
	Content json.RawMessage `json:"content,omitempty"`
}

type responsesInputContentPiece struct {
	Type string `json:"type,omitempty"`
	Text string `json:"text,omitempty"`
}
