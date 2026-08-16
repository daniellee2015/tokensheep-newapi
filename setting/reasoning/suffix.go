package reasoning

import (
	"strings"

	"github.com/samber/lo"
)

var EffortSuffixes = []string{"-max", "-xhigh", "-high", "-medium", "-low", "-minimal"}

var OpenAIEffortSuffixes = []string{"-high", "-minimal", "-low", "-medium", "-none", "-xhigh"}

var DeepSeekV4EffortSuffixes = []string{"-none", "-max"}

// TrimEffortSuffix -> modelName level(low) exists
func TrimEffortSuffix(modelName string) (string, string, bool) {
	return TrimEffortSuffixWithSuffixes(modelName, EffortSuffixes)
}

func TrimEffortSuffixWithSuffixes(modelName string, suffixes []string) (string, string, bool) {
	suffix, found := lo.Find(suffixes, func(s string) bool {
		return strings.HasSuffix(modelName, s)
	})
	if !found {
		return modelName, "", false
	}
	return strings.TrimSuffix(modelName, suffix), strings.TrimPrefix(suffix, "-"), true
}

func ParseOpenAIReasoningEffortFromModelSuffix(modelName string) (string, string) {
	baseModel, effort, ok := TrimEffortSuffixWithSuffixes(modelName, OpenAIEffortSuffixes)
	if !ok {
		return "", modelName
	}
	return effort, baseModel
}

func ParseDeepSeekV4ThinkingSuffix(modelName string) (baseModel string, thinkingType string, effort string, ok bool) {
	baseModel, suffix, ok := TrimEffortSuffixWithSuffixes(modelName, DeepSeekV4EffortSuffixes)
	if !ok || !strings.HasPrefix(baseModel, "deepseek-v4-") {
		return modelName, "", "", false
	}
	switch suffix {
	case "none":
		return baseModel, "disabled", "", true
	case "max":
		return baseModel, "enabled", "max", true
	default:
		return modelName, "", "", false
	}
}

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
