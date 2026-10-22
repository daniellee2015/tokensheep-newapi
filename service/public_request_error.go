package service

import (
	"net/http"
	"regexp"

	"github.com/QuantumNous/new-api/common"
)

// Operational details are private even when a provider reports them as a 400.
// Request validation, safety explanations and context limits remain public.
var privateRequestErrorPattern = regexp.MustCompile(`(?i)` +
	`\b(?:quota|balance|credits?|billing|pricing|price|cost|credentials?)\b|` +
	`\b(?:account[ _-]?pool|accounts? exhausted|no available accounts?|all available accounts?|unknown provider|no available channel|distributor)\b|` +
	`\b(?:group|channel|node|account)[ _:#=-]+[\w-]+|` +
	`\b(?:api[_-]?key|access[_-]?token|refresh[_-]?token|secret|authorization)["']?\s*[:=]|\bbearer\s+\S+|\bsk-[\w-]+|` +
	`this model is not available|额度|余额|预扣|计费|价格|费用|成本|账号|账户|帐号|凭据|号池|分组|渠道|节点|并发|[$＄€¥￥]`)

// Suppliers sometimes wrap actionable request rejections in a 403 or 5xx.
// Match explicit rejection reasons, not generic mentions of a safety service.
var publicRequestRejectionPattern = regexp.MustCompile(`(?i)` +
	`\b(?:context_length_exceeded|context_window_exceeded|input token count exceeds|maximum context length|prompt is too long|prompt too long|` +
	`prompt_blocked|prohibited_content|content_policy_violation|request blocked by Gemini API|` +
	`blocked by cyber-security policy|rejected by content safety checks|blocked by (?:our |the )?content filter)\b|` +
	`上下文(?:长度)?(?:超出|超过|过长)|(?:网络)?安全策略屏蔽|内容安全(?:拦截|拒绝)`)

// publicRequestErrorMessage preserves actionable provider messages while
// hiding operational details. Protocol envelopes and status policy stay local.
func publicRequestErrorMessage(statusCode int, rawMessage string) string {
	switch statusCode {
	case http.StatusNotFound:
		return "The requested model or endpoint is unavailable. Check the model and endpoint; if both are correct, contact support with this request ID."
	case http.StatusMethodNotAllowed:
		return "The request method or operation is not supported. Check the API endpoint and HTTP method."
	case http.StatusConflict:
		return "The request conflicts with the current resource state. Refresh the resource state before trying again."
	case http.StatusGone:
		return "The requested resource has expired or is no longer available. Create a new resource or update the request."
	case http.StatusRequestEntityTooLarge:
		return "The request body is too large. Reduce the size of messages or attachments."
	case http.StatusUnsupportedMediaType:
		return "The request media type is not supported. Check Content-Type and the file format."
	}

	return publicProviderErrorMessage(rawMessage, publicInvalidRequestMessage)
}

// publicProviderErrorMessage is the message trust boundary shared by request
// rejections and gateway failures. The caller supplies only the empty-message
// fallback; operational details always retain the private-error fallback.
func publicProviderErrorMessage(rawMessage, emptyMessage string) string {
	message := common.StripNestedRequestIDs(rawMessage)
	if privateRequestErrorPattern.MatchString(message) {
		return publicServiceUnavailableMessage
	}
	if message == "" {
		return emptyMessage
	}
	return common.MaskSensitiveInfo(message)
}
