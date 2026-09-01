package service

import (
	"regexp"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

// errorMaskRegexCache memoises compiled rule patterns. Rules live in settings
// and are re-read on every error, so compiling per call would put a regex
// compile on the failure path of every request.
var errorMaskRegexCache sync.Map // cacheKey -> *regexp.Regexp or errorMaskBadPattern

// errorMaskBadPattern marks a pattern that failed to compile, so a malformed
// operator-supplied rule is skipped instead of retried on every error.
type errorMaskBadPattern struct{}

func compileErrorMaskRule(rule dto.ErrorMaskRule) *regexp.Regexp {
	cacheKey := rule.Pattern
	if rule.IgnoreCase {
		cacheKey = "(?i)" + cacheKey
	}
	if cached, ok := errorMaskRegexCache.Load(cacheKey); ok {
		if re, ok := cached.(*regexp.Regexp); ok {
			return re
		}
		return nil
	}
	re, err := regexp.Compile(cacheKey)
	if err != nil {
		errorMaskRegexCache.Store(cacheKey, errorMaskBadPattern{})
		common.SysError("invalid error mask pattern, rule skipped: " + rule.Pattern + ", err: " + err.Error())
		return nil
	}
	errorMaskRegexCache.Store(cacheKey, re)
	return re
}

func applyErrorMaskRules(message string, rules []dto.ErrorMaskRule) string {
	for _, rule := range rules {
		if rule.Pattern == "" || message == "" {
			continue
		}
		if rule.IsRegex {
			re := compileErrorMaskRule(rule)
			if re == nil {
				continue
			}
			message = re.ReplaceAllString(message, rule.Replace)
			continue
		}
		if rule.IgnoreCase {
			// Literal, case-insensitive: quote the pattern so regex
			// metacharacters in it stay literal.
			re := compileErrorMaskRule(dto.ErrorMaskRule{
				Pattern:    regexp.QuoteMeta(rule.Pattern),
				IgnoreCase: true,
			})
			if re == nil {
				continue
			}
			message = re.ReplaceAllLiteralString(message, rule.Replace)
			continue
		}
		message = strings.ReplaceAll(message, rule.Pattern, rule.Replace)
	}
	return message
}

// ApplyGlobalErrorMask runs the globally configured rules. It is injected into
// kitutil so every downstream-facing message (relay error responses and the
// user-visible logs.content column) passes through it.
func ApplyGlobalErrorMask(message string) string {
	setting := operation_setting.GetErrorMaskSetting()
	if setting == nil || !setting.Enabled || len(setting.Rules) == 0 {
		return message
	}
	return normalizeMaskedMessage(applyErrorMaskRules(message, setting.Rules))
}

// normalizeMaskedMessage collapses the whitespace a removal rule leaves behind
// and substitutes the fallback when the rules consumed the whole message.
func normalizeMaskedMessage(message string) string {
	message = strings.Join(strings.Fields(message), " ")
	// A message reduced to punctuation carries no information either.
	if strings.Trim(message, " .,:;-—") == "" {
		return operation_setting.GetErrorMaskFallbackMessage()
	}
	return message
}

func init() {
	kitutil.SetExtraMask(ApplyGlobalErrorMask)
}

// ApplyChannelErrorMask rewrites the message with the selected channel's rules,
// which run before the global set. The global rules and the built-in URL/IP
// masking still apply afterwards, because ToOpenAIError/ToClaudeError and the
// log writer all funnel through MaskSensitiveInfo at render time.
func ApplyChannelErrorMask(c *gin.Context, newApiErr *types.NewAPIError) {
	if newApiErr == nil || c == nil {
		return
	}
	rulesJSON := c.GetString(string(constant.ContextKeyChannelErrorMaskRules))
	if rulesJSON == "" || rulesJSON == "[]" {
		return
	}

	var rules []dto.ErrorMaskRule
	if err := common.UnmarshalJsonStr(rulesJSON, &rules); err != nil {
		common.SysError("failed to parse channel error mask rules: " + err.Error())
		return
	}
	if len(rules) == 0 {
		return
	}

	masked := applyErrorMaskRules(newApiErr.Error(), rules)
	// Normalising here would pre-empt the global layer, so only guard against a
	// channel rule emptying the message entirely.
	if strings.TrimSpace(masked) == "" {
		masked = operation_setting.GetErrorMaskFallbackMessage()
	}
	newApiErr.SetMessage(masked)
}
