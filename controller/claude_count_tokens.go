package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// ClaudeCountTokens serves POST /v1/messages/count_tokens.
//
// Anthropic exposes this endpoint and clients call it to size a prompt before
// sending it. Relaying it upstream is not viable: most channels behind this
// gateway are not Anthropic and answer 404, which the caller sees as the whole
// endpoint being missing. Counting locally always answers, costs no quota, and
// keeps the number consistent with the estimate used for billing.
func ClaudeCountTokens(c *gin.Context) {
	var request dto.ClaudeRequest
	if err := common.UnmarshalBodyReusable(c, &request); err != nil {
		claudeCountTokensError(c, http.StatusBadRequest, "invalid_request_error", "request body is not valid JSON")
		return
	}
	if strings.TrimSpace(request.Model) == "" {
		claudeCountTokensError(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}

	var prompt strings.Builder
	if system := claudeContentText(request.System); system != "" {
		prompt.WriteString(system)
		prompt.WriteByte('\n')
	}
	for _, message := range request.Messages {
		prompt.WriteString(message.Role)
		prompt.WriteString(": ")
		prompt.WriteString(claudeContentText(message.Content))
		prompt.WriteByte('\n')
	}
	if request.Tools != nil {
		// Tool schemas occupy prompt space, so a count that ignored them would
		// under-report for exactly the requests most at risk of overflowing.
		if encoded, err := json.Marshal(request.Tools); err == nil {
			prompt.Write(encoded)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"input_tokens": service.CountTextToken(prompt.String(), request.Model),
	})
}

// claudeContentText flattens Anthropic's string-or-block content into the text
// the tokenizer can measure.
func claudeContentText(content any) string {
	switch value := content.(type) {
	case nil:
		return ""
	case string:
		return value
	case []any:
		parts := make([]string, 0, len(value))
		for _, item := range value {
			block, ok := item.(map[string]any)
			if !ok {
				parts = append(parts, fmt.Sprintf("%v", item))
				continue
			}
			if text, ok := block["text"].(string); ok {
				parts = append(parts, text)
				continue
			}
			// Non-text blocks (images, documents, tool results) still consume
			// context; their serialized form is the closest local proxy.
			if encoded, err := json.Marshal(block); err == nil {
				parts = append(parts, string(encoded))
			}
		}
		return strings.Join(parts, "\n")
	default:
		if encoded, err := json.Marshal(value); err == nil {
			return string(encoded)
		}
		return fmt.Sprintf("%v", value)
	}
}

func claudeCountTokensError(c *gin.Context, statusCode int, errorType, message string) {
	c.JSON(statusCode, gin.H{
		"type": "error",
		"error": gin.H{
			"type":    errorType,
			"message": common.MessageWithRequestId(message, c.GetString(common.RequestIdKey)),
		},
	})
}
