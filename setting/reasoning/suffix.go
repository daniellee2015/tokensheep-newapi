// Package reasoning re-exports the pure model-name effort-suffix helpers,
// which moved to the conversion kit (service/relayconvert/reasoning) as part
// of the relaykit extraction. Host code keeps importing this path unchanged.
package reasoning

import (
	"strings"

	kitreasoning "github.com/QuantumNous/new-api/relaykit/relayconvert/reasoning"
)

var (
	EffortSuffixes           = kitreasoning.EffortSuffixes
	OpenAIEffortSuffixes     = kitreasoning.OpenAIEffortSuffixes
	DeepSeekV4EffortSuffixes = kitreasoning.DeepSeekV4EffortSuffixes
)

var (
	TrimEffortSuffix                          = kitreasoning.TrimEffortSuffix
	TrimEffortSuffixWithSuffixes              = kitreasoning.TrimEffortSuffixWithSuffixes
	ParseOpenAIReasoningEffortFromModelSuffix = kitreasoning.ParseOpenAIReasoningEffortFromModelSuffix
	ParseDeepSeekV4ThinkingSuffix             = kitreasoning.ParseDeepSeekV4ThinkingSuffix
)

// The Claude adaptive-effort helpers below stay host-side: only the Claude
// relay and handler consult them, no converter does.

func SupportsClaudeAdaptiveEffortSuffix(modelName string) bool {
	baseModel, effort, ok := TrimEffortSuffix(modelName)
	return ok && effort != "" && IsClaudeVersionedModel(baseModel)
}

func SupportsClaudeAdaptiveThinkingAlias(baseModel string) bool {
	return IsClaudeVersionedModel(strings.TrimSuffix(baseModel, "-thinking"))
}

func ShouldUseClaudeLegacyAdaptiveSampling(baseModel string) bool {
	return strings.HasPrefix(baseModel, "claude-opus-4-6")
}

func IsClaudeVersionedModel(modelName string) bool {
	trimmed := strings.TrimSpace(modelName)
	if !strings.HasPrefix(trimmed, "claude-") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(trimmed, "claude-"), "-")
	if len(parts) == 0 {
		return false
	}
	if startsWithSupportedClaudeVersion(parts[0]) {
		return true
	}
	if len(parts) >= 2 && isClaudeFamily(parts[0]) && startsWithSupportedClaudeVersion(parts[1]) {
		return true
	}
	return false
}

func startsWithSupportedClaudeVersion(part string) bool {
	if part == "" || part[0] < '0' || part[0] > '9' {
		return false
	}
	return part[0] != '3'
}

func isClaudeFamily(part string) bool {
	switch part {
	case "opus", "sonnet", "haiku", "fable":
		return true
	default:
		return false
	}
}
