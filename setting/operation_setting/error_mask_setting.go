package operation_setting

import (
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/config"
)

// ErrorMaskFallbackMessage replaces a message that a rule set emptied out, so a
// downstream caller never receives a blank error body.
const ErrorMaskFallbackMessage = "Service temporarily unavailable"

type ErrorMaskSetting struct {
	Enabled bool `json:"enabled"`
	// FallbackMessage overrides ErrorMaskFallbackMessage when non-empty.
	FallbackMessage string              `json:"fallback_message"`
	Rules           []dto.ErrorMaskRule `json:"rules"`
}

// Rule ordering matters: whole-sentence patterns run before the single-word
// fallbacks, otherwise rewriting "upstream" first would break the longer
// patterns that contain it.
var errorMaskSetting = ErrorMaskSetting{
	Enabled:         true,
	FallbackMessage: ErrorMaskFallbackMessage,
	Rules: []dto.ErrorMaskRule{
		// --- Whole-sentence rewrites: credential-pool and account-pool wording ---
		// Leaks pool size, e.g. "分组 [free] 无可用凭据（0/247 可用）".
		{Pattern: `分组\s*\[[^\]]*\]\s*无可用凭据[^。;]*`, Replace: ErrorMaskFallbackMessage, IsRegex: true},
		{Pattern: `[Nn]o available accounts(:\s*no available accounts)?`, Replace: ErrorMaskFallbackMessage, IsRegex: true},
		// Strip the framing prefix but keep whatever follows it.
		{Pattern: `[Uu]pstream connection was interrupted mid-response after \d+ bytes:\s*`, Replace: "", IsRegex: true},
		{Pattern: `upstream error: do request failed`, Replace: ErrorMaskFallbackMessage},
		{Pattern: `[Uu]pstream (rate limit exceeded|rate limited)\.?\s*(Retry later\.?|T)?`, Replace: "Rate limit exceeded. Retry later.", IsRegex: true},
		{Pattern: `[Uu]pstream temporarily unavailable`, Replace: ErrorMaskFallbackMessage, IsRegex: true},

		// --- Infrastructure and topology ---
		// Proxy hop, e.g. "socks connect tcp ***:7401->***:443: unknown".
		{Pattern: `socks connect tcp[^\s]*(\s*->\s*[^\s:]*)?:?[^,;]*`, Replace: "Connection failed", IsRegex: true},
		{Pattern: `system cpu overloaded \([^)]*\)`, Replace: ErrorMaskFallbackMessage, IsRegex: true},
		{Pattern: `[Cc]loudflare could not establish a TCP connection to the origin server[^.]*\.?`, Replace: ErrorMaskFallbackMessage, IsRegex: true},
		{Pattern: `The TCP handshake[^.]*\.?`, Replace: "", IsRegex: true},
		// Internal middleware name plus group naming.
		{Pattern: `[Nn]o available channel for model (\S+) under group \S+\s*\(distributor\)`, Replace: "Model $1 is not available", IsRegex: true},
		{Pattern: `[Nn]o available channel for model (\S+)[^(]*`, Replace: "Model $1 is not available", IsRegex: true},

		// --- Provider / routing vocabulary ---
		{Pattern: `unknown provider for model (\S+)`, Replace: "Model $1 is not available", IsRegex: true},
		{Pattern: `流式 API 请求失败:\s*(\d+)[^\n]*`, Replace: "Request failed with status $1", IsRegex: true},
		{Pattern: `该会话已被网络安全策略屏蔽[^/]*(/[^(]*)?`, Replace: "Request rejected", IsRegex: true},

		// --- Catch-all vocabulary, last so it only touches leftovers ---
		{Pattern: `凭据`, Replace: "service"},
		{Pattern: `号池`, Replace: "service"},
		{Pattern: `中转`, Replace: "service"},
		{Pattern: `代理商`, Replace: "service"},
		{Pattern: `\bupstream\b`, Replace: "service", IsRegex: true, IgnoreCase: true},
		{Pattern: `\bdistributor\b`, Replace: "router", IsRegex: true, IgnoreCase: true},
	},
}

func init() {
	config.GlobalConfig.Register("error_mask_setting", &errorMaskSetting)
}

func GetErrorMaskSetting() *ErrorMaskSetting {
	return &errorMaskSetting
}

// GetErrorMaskFallbackMessage returns the configured fallback, defaulting to
// ErrorMaskFallbackMessage when the operator cleared the field.
func GetErrorMaskFallbackMessage() string {
	if errorMaskSetting.FallbackMessage == "" {
		return ErrorMaskFallbackMessage
	}
	return errorMaskSetting.FallbackMessage
}
