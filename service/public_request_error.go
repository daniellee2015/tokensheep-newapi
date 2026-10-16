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

	message := common.StripNestedRequestIDs(rawMessage)
	if privateRequestErrorPattern.MatchString(message) {
		return publicServiceUnavailableMessage
	}
	if message == "" {
		return publicInvalidRequestMessage
	}
	return common.MaskSensitiveInfo(message)
}
