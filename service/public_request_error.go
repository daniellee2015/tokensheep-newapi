package service

import (
	"net/http"
	"strings"
)

// publicRequestErrorMessage classifies request failures into gateway-owned
// guidance. Only fixed strings leave this boundary: never include provider
// names, field values, URLs, token limits, metadata, or upstream request IDs.
// HTTP status and protocol error types remain compatible with the public policy.
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

	message := strings.ToLower(rawMessage)
	switch {
	case strings.Contains(message, "input token count exceeds"),
		strings.Contains(message, "maximum context length"),
		strings.Contains(message, "context_length_exceeded"):
		return "Input exceeds the model context limit. Shorten the conversation or reduce the input."
	case strings.Contains(message, "prohibited_content"),
		strings.Contains(message, "content_policy_violation"):
		return "The request was rejected by content safety checks. Review the submitted content."
	case strings.Contains(message, "requests ending with a model turn are not supported"):
		return "The conversation ends with an assistant message that this operation does not support. Check the message sequence; if it is valid, contact support with this request ID."
	case strings.Contains(message, "contents is not specified"),
		strings.Contains(message, "messages must not be empty"),
		strings.Contains(message, "field messages is required"):
		return "Request content is missing or became empty during processing. Check the endpoint and message content; if the input is valid, contact support with this request ID."
	case strings.Contains(message, "filedata.fileuri") && strings.Contains(message, "does not fetch third-party urls"):
		return "Remote file URLs are not supported for this request. Send the file using a supported upload or inline format."
	case strings.Contains(message, "supports text and https input_image content only"),
		strings.Contains(message, "unsupported content type"):
		return "The request contains an unsupported content type. Check the text, image, and file formats accepted by this operation."
	case statusCode == http.StatusUnprocessableEntity:
		return "The request parameters could not be accepted. Check required fields, value types, and supported ranges."
	default:
		return publicInvalidRequestMessage
	}
}
