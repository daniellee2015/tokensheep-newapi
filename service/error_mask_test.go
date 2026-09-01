package service

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestApplyGlobalErrorMask_RealUpstreamLeaks feeds the shipped default rules the
// exact upstream strings observed in production logs and asserts none of the
// infrastructure vocabulary survives into a downstream-visible message.
func TestApplyGlobalErrorMask_RealUpstreamLeaks(t *testing.T) {
	setting := operation_setting.GetErrorMaskSetting()
	require.True(t, setting.Enabled, "default rules must be enabled for this test")

	cases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "credential pool size",
			input:    "分组 [free] 无可用凭据（0/247 可用）",
			expected: "Service temporarily unavailable",
		},
		{
			name:     "no available accounts",
			input:    "No available accounts: no available accounts",
			expected: "Service temporarily unavailable",
		},
		{
			name:     "cpu overload threshold",
			input:    "system cpu overloaded (current: 93.4%, threshold: 90%)",
			expected: "Service temporarily unavailable",
		},
		{
			name:     "do request failed",
			input:    "upstream error: do request failed",
			expected: "Service temporarily unavailable",
		},
		{
			name:     "interrupted prefix is stripped and inner leak masked",
			input:    "Upstream connection was interrupted mid-response after 0 bytes: 分组 [free] 无可用凭据（0/247 可用）",
			expected: "Service temporarily unavailable",
		},
		{
			name:     "distributor middleware name and group",
			input:    "No available channel for model gemini-3.5-flash under group gemini-lowprice (distributor)",
			expected: "Model gemini-3.5-flash is not available",
		},
		{
			name:     "unknown provider",
			input:    "unknown provider for model claude-sonnet-4-6-thinking",
			expected: "Model claude-sonnet-4-6-thinking is not available",
		},
		{
			name:     "chinese stream failure keeps status code",
			input:    "流式 API 请求失败: 400 Bad Request",
			expected: "Request failed with status 400",
		},
		{
			name:     "security policy block",
			input:    "该会话已被网络安全策略屏蔽，请开启新会话 / This session is blocked by cyber-security policy",
			expected: "Request rejected",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ApplyGlobalErrorMask(tc.input)
			assert.Equal(t, tc.expected, got)
		})
	}
}

// TestApplyGlobalErrorMask_NoVocabularyLeaks is the backstop for wording the
// explicit rules do not enumerate: whatever the upstream says, these terms must
// never reach a downstream caller.
func TestApplyGlobalErrorMask_NoVocabularyLeaks(t *testing.T) {
	forbidden := []string{"upstream", "Upstream", "凭据", "号池", "中转", "代理商", "distributor"}

	inputs := []string{
		"分组 [free] 无可用凭据（0/247 可用）",
		"Upstream connection was interrupted mid-response after 512 bytes: upstream rate limited",
		"upstream error: do request failed",
		"some brand new upstream wording we never saw before",
		"渠道凭据耗尽，号池已空",
		"routed by distributor to fallback",
	}

	for _, input := range inputs {
		got := ApplyGlobalErrorMask(input)
		for _, term := range forbidden {
			assert.NotContains(t, got, term, "input %q leaked %q via %q", input, term, got)
		}
	}
}

func TestApplyErrorMaskRules_LiteralAndRegex(t *testing.T) {
	t.Run("literal replace leaves regex metacharacters alone", func(t *testing.T) {
		rules := []dto.ErrorMaskRule{{Pattern: "a.b", Replace: "X"}}
		// Literal mode must not treat "." as a wildcard.
		assert.Equal(t, "X", applyErrorMaskRules("a.b", rules))
		assert.Equal(t, "axb", applyErrorMaskRules("axb", rules))
	})

	t.Run("regex captures are substituted", func(t *testing.T) {
		rules := []dto.ErrorMaskRule{{Pattern: `pool (\d+)/(\d+)`, Replace: "size $2", IsRegex: true}}
		assert.Equal(t, "size 247", applyErrorMaskRules("pool 0/247", rules))
	})

	t.Run("case insensitive literal", func(t *testing.T) {
		rules := []dto.ErrorMaskRule{{Pattern: "UPSTREAM", Replace: "service", IgnoreCase: true}}
		assert.Equal(t, "service failed", applyErrorMaskRules("Upstream failed", rules))
	})

	t.Run("invalid regex is skipped without altering the message", func(t *testing.T) {
		rules := []dto.ErrorMaskRule{{Pattern: "([unclosed", Replace: "X", IsRegex: true}}
		assert.Equal(t, "original text", applyErrorMaskRules("original text", rules))
	})

	t.Run("empty pattern is ignored", func(t *testing.T) {
		rules := []dto.ErrorMaskRule{{Pattern: "", Replace: "X"}}
		assert.Equal(t, "unchanged", applyErrorMaskRules("unchanged", rules))
	})

	t.Run("rules apply in order", func(t *testing.T) {
		rules := []dto.ErrorMaskRule{
			{Pattern: "first", Replace: "second"},
			{Pattern: "second", Replace: "third"},
		}
		// The second rule sees the first rule's output.
		assert.Equal(t, "third", applyErrorMaskRules("first", rules))
	})
}

// TestNormalizeMaskedMessage_FallbackOnEmpty guards the case where a removal
// rule consumes the whole message: a downstream caller must still get a body.
func TestNormalizeMaskedMessage_FallbackOnEmpty(t *testing.T) {
	fallback := operation_setting.GetErrorMaskFallbackMessage()

	cases := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "empty", input: "", expected: fallback},
		{name: "whitespace only", input: "   \t ", expected: fallback},
		{name: "punctuation only", input: " . , : ; - ", expected: fallback},
		{name: "collapses internal whitespace", input: "too   many    spaces", expected: "too many spaces"},
		{name: "trims edges", input: "  trimmed  ", expected: "trimmed"},
		{name: "real text preserved", input: "Rate limit exceeded", expected: "Rate limit exceeded"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, normalizeMaskedMessage(tc.input))
		})
	}
}

// TestApplyGlobalErrorMask_PreservesActionableClientErrors makes sure masking
// does not swallow the 4xx messages a caller needs in order to fix its request.
func TestApplyGlobalErrorMask_PreservesActionableClientErrors(t *testing.T) {
	actionable := []string{
		"max_tokens must be less than or equal to 64000",
		"Requests ending with a model turn are not supported.",
		"No tool output found for custom tool call call_abc123.",
		"invalid_request_error: messages must not be empty",
	}

	for _, input := range actionable {
		assert.Equal(t, input, ApplyGlobalErrorMask(input),
			"actionable client error must reach the caller unchanged")
	}
}
