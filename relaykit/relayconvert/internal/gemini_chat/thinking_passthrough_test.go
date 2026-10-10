package geminichat

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	oaichat "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/oai_chat"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Regression for the 2026-10-10 "include_thoughts=false silently dropped"
// incident. GeminiThinkingConfig.IncludeThoughts is tri-state via *bool so
// an explicit `false` reaches upstream instead of being omitted by `omitempty`.
func TestGeminiThinkingConfigIncludeThoughtsRoundTrip(t *testing.T) {
	cases := []struct {
		name, input, want string
	}{
		{"camel_false", `{"includeThoughts":false}`, `{"includeThoughts":false}`},
		{"snake_false", `{"include_thoughts":false}`, `{"includeThoughts":false}`},
		{"camel_true", `{"includeThoughts":true}`, `{"includeThoughts":true}`},
		{"snake_true", `{"include_thoughts":true}`, `{"includeThoughts":true}`},
		{"absent", `{}`, `{}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var cfg dto.GeminiThinkingConfig
			require.NoError(t, kitutil.Unmarshal([]byte(tc.input), &cfg))
			got, err := kitutil.Marshal(cfg)
			require.NoError(t, err)
			assert.JSONEq(t, tc.want, string(got))
		})
	}
}

// End-to-end: an OpenAI /chat/completions request carrying
// extra_body.google.thinking_config.include_thoughts=false must forward
// includeThoughts:false to the upstream Gemini generationConfig.
func TestOpenAIExtraBodyIncludeThoughtsFalseReachesUpstream(t *testing.T) {
	raw := []byte(`{
		"model":"gemini-2.5-flash",
		"messages":[{"role":"user","content":"hi"}],
		"extra_body":{"google":{"thinking_config":{"include_thoughts":false}}}
	}`)
	var req dto.GeneralOpenAIRequest
	require.NoError(t, kitutil.Unmarshal(raw, &req))

	converted, err := oaichat.OpenAIChatRequestToGeminiGenerateContent(t.Context(), req, nil)
	require.NoError(t, err)
	require.NotNil(t, converted.GenerationConfig.ThinkingConfig)
	require.NotNil(t, converted.GenerationConfig.ThinkingConfig.IncludeThoughts)
	assert.False(t, *converted.GenerationConfig.ThinkingConfig.IncludeThoughts)

	wire, err := kitutil.Marshal(converted)
	require.NoError(t, err)
	assert.Contains(t, string(wire), `"includeThoughts":false`,
		"explicit false must survive serialization so upstream can honor it")
}

// Preserving pre-pointer wire semantics: a thinking_budget-only request must
// not grow a stray `includeThoughts` field, in either direction of the budget.
// Historically only a positive budget emitted includeThoughts:true (the
// `budget>0` branch) and non-positive budgets were silently dropped by
// bool+omitempty; we must emit the same wire shape now that IncludeThoughts
// is a pointer.
func TestOpenAIExtraBodyThinkingBudgetDoesNotForceIncludeThoughts(t *testing.T) {
	cases := []struct {
		name                      string
		budget                    int
		wantIncludeThoughtsOnWire bool
		wantIncludeThoughtsValue  bool
	}{
		{"positive_budget_implies_true", 1024, true, true},
		{"zero_budget_stays_unspecified", 0, false, false},
		{"negative_budget_stays_unspecified", -1, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte(`{
				"model":"gemini-2.5-flash",
				"messages":[{"role":"user","content":"hi"}],
				"extra_body":{"google":{"thinking_config":{"thinking_budget":` +
				itoa(tc.budget) + `}}}
			}`)
			var req dto.GeneralOpenAIRequest
			require.NoError(t, kitutil.Unmarshal(raw, &req))

			converted, err := oaichat.OpenAIChatRequestToGeminiGenerateContent(t.Context(), req, nil)
			require.NoError(t, err)
			require.NotNil(t, converted.GenerationConfig.ThinkingConfig)

			if tc.wantIncludeThoughtsOnWire {
				require.NotNil(t, converted.GenerationConfig.ThinkingConfig.IncludeThoughts)
				assert.Equal(t, tc.wantIncludeThoughtsValue,
					*converted.GenerationConfig.ThinkingConfig.IncludeThoughts)
			} else {
				assert.Nil(t, converted.GenerationConfig.ThinkingConfig.IncludeThoughts,
					"budget-only request must leave IncludeThoughts unspecified")
			}
		})
	}
}

// Only extra_body.google.thinking_config is a recognized entry point for the
// OpenAI-compatible path; top-level `include_thoughts` and top-level `google`
// are ignored. Pinning this stops accidental expansion of entry points.
func TestOpenAIIncludeThoughtsOnlyThroughExtraBodyGoogle(t *testing.T) {
	cases := []struct {
		name, extras string
		wantConfig   bool
	}{
		{"top_level_bare", `"include_thoughts":true`, false},
		{"top_level_google", `"google":{"thinking_config":{"include_thoughts":true}}`, false},
		{"extra_body_google", `"extra_body":{"google":{"thinking_config":{"include_thoughts":true}}}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte(`{"model":"gemini-2.5-flash","messages":[{"role":"user","content":"hi"}],` + tc.extras + `}`)
			var req dto.GeneralOpenAIRequest
			require.NoError(t, kitutil.Unmarshal(raw, &req))
			got, err := oaichat.OpenAIChatRequestToGeminiGenerateContent(t.Context(), req, nil)
			require.NoError(t, err)
			assert.Equal(t, tc.wantConfig, got.GenerationConfig.ThinkingConfig != nil)
		})
	}
}

// Regression for the streaming converter's "shared buffer leaks content into
// reasoning" bug: when a single Gemini frame contains both a thought part and
// an answer part, each must route to its own OpenAI delta field.
func TestStreamResponseSeparatesThoughtAndContent(t *testing.T) {
	cases := []struct {
		name, parts, wantContent, wantReasoning string
	}{
		{"thought_then_answer",
			`[{"text":"THOUGHT_A","thought":true},{"text":"ANSWER"}]`,
			"ANSWER", "THOUGHT_A"},
		{"answer_then_thought",
			`[{"text":"ANSWER"},{"text":"THOUGHT_A","thought":true}]`,
			"ANSWER", "THOUGHT_A"},
		{"thought_only",
			`[{"text":"THOUGHT_A","thought":true}]`,
			"", "THOUGHT_A"},
		{"answer_only",
			`[{"text":"ANSWER"}]`,
			"ANSWER", ""},
		{"multiple_thoughts_accumulate",
			`[{"text":"T1","thought":true},{"text":"T2","thought":true},{"text":"ANSWER"}]`,
			"ANSWER", "T1\nT2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte(`{"candidates":[{"content":{"role":"model","parts":` + tc.parts + `}}]}`)
			var resp dto.GeminiChatResponse
			require.NoError(t, kitutil.Unmarshal(raw, &resp))
			got, _ := StreamResponseGeminiChat2OpenAI(&resp)
			require.Len(t, got.Choices, 1)
			assert.Equal(t, tc.wantContent, got.Choices[0].Delta.GetContentString())
			assert.Equal(t, tc.wantReasoning, got.Choices[0].Delta.GetReasoningContent())
		})
	}
}

// Regression for the non-streaming converter's "last thought wins" bug:
// multiple thought parts in a single candidate must all survive into the
// final reasoning_content, not just the last one.
func TestNonStreamResponseAccumulatesThoughtParts(t *testing.T) {
	raw := []byte(`{"candidates":[{"content":{"role":"model","parts":[
		{"text":"T1","thought":true},
		{"text":"T2","thought":true},
		{"text":"ANSWER"}
	]}}]}`)
	var resp dto.GeminiChatResponse
	require.NoError(t, kitutil.Unmarshal(raw, &resp))

	got := ResponseGeminiChat2OpenAI("audit", 1, &resp)
	require.Len(t, got.Choices, 1)
	assert.Equal(t, "ANSWER", got.Choices[0].Message.Content)
	require.NotNil(t, got.Choices[0].Message.ReasoningContent)
	assert.Equal(t, "T1\nT2", *got.Choices[0].Message.ReasoningContent)
}

// itoa avoids pulling strconv just for one call site. The test file never
// touches negative-vs-positive formatting beyond the three cases above.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		digits[i] = '-'
	}
	return string(digits[i:])
}
