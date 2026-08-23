package claude

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/dto"
)

// Anthropic's message shape is wider than dto.ClaudeResponse models. The relay
// must not silently drop the difference on the way back to the caller.
func TestPatchNativeClaudeMessageResponseDataKeepsUnmodelledFields(t *testing.T) {
	upstream := []byte(`{"id":"msg_original","type":"message","role":"assistant",` +
		`"content":[{"type":"text","text":"hi"}],"model":"claude-opus-4-8",` +
		`"stop_reason":"stop_sequence","stop_sequence":"4",` +
		`"usage":{"input_tokens":5,"output_tokens":7}}`)

	response := &dto.ClaudeResponse{}
	if err := json.Unmarshal(upstream, response); err != nil {
		t.Fatal(err)
	}
	response.Id = "msg_rewritten"

	patched := patchNativeClaudeMessageResponseData(upstream, response, false)

	var got map[string]any
	if err := json.Unmarshal(patched, &got); err != nil {
		t.Fatalf("patched body is not JSON: %v", err)
	}
	if got["stop_sequence"] != "4" {
		t.Fatalf("stop_sequence = %v, want it preserved", got["stop_sequence"])
	}
	if got["stop_reason"] != "stop_sequence" {
		t.Fatalf("stop_reason = %v", got["stop_reason"])
	}
	if got["id"] != "msg_rewritten" {
		t.Fatalf("id = %v, want the relay's rewrite applied", got["id"])
	}
}

// A null stop_sequence must survive too: clients assert the key exists on every
// message, not merely that it is non-empty.
func TestPatchNativeClaudeMessageResponseDataKeepsNullStopSequence(t *testing.T) {
	upstream := []byte(`{"id":"msg_x","type":"message","role":"assistant",` +
		`"content":[{"type":"text","text":"hi"}],"model":"claude-opus-4-8",` +
		`"stop_reason":"end_turn","stop_sequence":null}`)

	response := &dto.ClaudeResponse{}
	if err := json.Unmarshal(upstream, response); err != nil {
		t.Fatal(err)
	}

	patched := patchNativeClaudeMessageResponseData(upstream, response, false)

	var got map[string]any
	if err := json.Unmarshal(patched, &got); err != nil {
		t.Fatal(err)
	}
	if _, present := got["stop_sequence"]; !present {
		t.Fatal("stop_sequence key was dropped")
	}
	if got["stop_sequence"] != nil {
		t.Fatalf("stop_sequence = %v, want null", got["stop_sequence"])
	}
}

func TestPatchNativeClaudeMessageResponseDataAppliesNormalizedUsage(t *testing.T) {
	upstream := []byte(`{"id":"msg_x","type":"message","model":"m",` +
		`"usage":{"input_tokens":9999,"output_tokens":7}}`)

	response := &dto.ClaudeResponse{}
	if err := json.Unmarshal(upstream, response); err != nil {
		t.Fatal(err)
	}
	response.Usage.InputTokens = 12

	patched := patchNativeClaudeMessageResponseData(upstream, response, true)

	var got struct {
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(patched, &got); err != nil {
		t.Fatal(err)
	}
	if got.Usage.InputTokens != 12 {
		t.Fatalf("input_tokens = %d, want the normalized value", got.Usage.InputTokens)
	}
	if got.Usage.OutputTokens != 7 {
		t.Fatalf("output_tokens = %d, want it untouched", got.Usage.OutputTokens)
	}
}
