package reasoning

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSupportsClaudeAdaptiveEffortSuffix(t *testing.T) {
	tests := []struct {
		name  string
		model string
		want  bool
	}{
		{name: "opus 4.8", model: "claude-opus-4-8-high", want: true},
		{name: "fable 5", model: "claude-fable-5-high", want: true},
		{name: "new family order", model: "claude-5-sonnet-medium", want: true},
		{name: "cursor dotted version", model: "claude-4.6-sonnet-medium", want: true},
		{name: "claude 3 remains legacy", model: "claude-3-5-sonnet-high", want: false},
		{name: "no suffix", model: "claude-fable-5", want: false},
		{name: "non claude", model: "gpt-5-high", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, SupportsClaudeAdaptiveEffortSuffix(test.model))
		})
	}
}

func TestSupportsClaudeAdaptiveThinkingAlias(t *testing.T) {
	require.True(t, SupportsClaudeAdaptiveThinkingAlias("claude-sonnet-5"))
	require.True(t, SupportsClaudeAdaptiveThinkingAlias("claude-4.6-sonnet"))
	require.False(t, SupportsClaudeAdaptiveThinkingAlias("claude-3-5-sonnet"))
	require.False(t, SupportsClaudeAdaptiveThinkingAlias("gpt-5"))
}
