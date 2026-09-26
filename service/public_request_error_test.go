package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublicRequestErrorsExplainKnownFailuresWithoutLeakingProviderData(t *testing.T) {
	tests := []struct {
		name, raw, want string
		status          int
	}{
		{"missing content", "* GenerateContentRequest.contents: contents is not specified", "Request content is missing or became empty during processing. Check the endpoint and message content; if the input is valid, contact support with this request ID.", 400},
		{"context limit", "The input token count exceeds the maximum number of tokens allowed 1048576.", "Input exceeds the model context limit. Shorten the conversation or reduce the input.", 400},
		{"model turn", "Requests ending with a model turn are not supported.", "The conversation ends with an assistant message that this operation does not support. Check the message sequence; if it is valid, contact support with this request ID.", 400},
		{"safety rejection", "request blocked by Gemini API: PROHIBITED_CONTENT", "The request was rejected by content safety checks. Review the submitted content.", 400},
		{"remote image", "Antigravity rejects fileData.fileUri pointing to https://private.example/user-image.jpeg: the upstream does not fetch third-party URLs", "Remote file URLs are not supported for this request. Send the file using a supported upload or inline format.", 400},
		{"unsupported content", "basispoints supports text and HTTPS input_image content only (path=input[104].content[1]; type=unknown)", "The request contains an unsupported content type. Check the text, image, and file formats accepted by this operation.", 400},
		{"unknown invalid argument", "private invalid argument on node-49", "Invalid request", 400},
		{"unavailable resource", "private model abc was not found", "The requested model or endpoint is unavailable. Check the model and endpoint; if both are correct, contact support with this request ID.", 404},
		{"unsupported operation", "private route only accepts PATCH", "The request method or operation is not supported. Check the API endpoint and HTTP method.", 405},
		{"conflict", "private account busy", "The request conflicts with the current resource state. Refresh the resource state before trying again.", 409},
		{"expired resource", "private resource expired", "The requested resource has expired or is no longer available. Create a new resource or update the request.", 410},
		{"large body", "private proxy body limit 12345", "The request body is too large. Reduce the size of messages or attachments.", 413},
		{"media type", "private media handler rejected input", "The request media type is not supported. Check Content-Type and the file format.", 415},
		{"invalid parameters", "private validation failed", "The request parameters could not be accepted. Check required fields, value types, and supported ranges.", 422},
		{"capacity stays private", "contents is not specified; account pool empty", "Service temporarily unavailable", 503},
		{"rate limit stays private", "PROHIBITED_CONTENT; account pool empty", "Rate limit exceeded. Retry later.", 429},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rawMessage := tt.raw + "; secret=sk-private-secret; https://internal.example/node-49 (request id: upstream-private-id)"
			body, err := common.Marshal(map[string]any{"error": types.OpenAIError{
				Message: rawMessage, Type: "provider_private_type", Code: "provider_private_code",
				Param: "private_parameter", Metadata: []byte(`{"node":"private"}`),
			}})
			require.NoError(t, err)
			raw := RelayErrorHandler(context.Background(), &http.Response{
				StatusCode: tt.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body))),
			}, false)
			require.NotNil(t, raw)
			originalError := raw.Error()
			public := PublicUpstreamError(raw)
			require.NotNil(t, public)
			assert.Equal(t, tt.want, public.Error())
			assert.Equal(t, tt.want, public.ToOpenAIError().Message)
			assert.Equal(t, tt.want, public.ToClaudeError().Message)
			assert.Empty(t, public.ToOpenAIError().Param)
			assert.Empty(t, public.ToOpenAIError().Metadata)
			assert.Empty(t, public.Metadata)
			assert.Equal(t, originalError, raw.Error(), "retry policy and admin diagnostics need the original error")
			assert.Equal(t, tt.status, raw.StatusCode)
			if tt.status != 429 && tt.status != 503 {
				assert.Equal(t, http.StatusBadRequest, public.StatusCode)
				assert.Equal(t, types.ErrorCodeInvalidRequest, public.GetErrorCode())
				assert.Equal(t, "invalid_request", public.ToOpenAIError().Type)
			}
			task := PublicUpstreamTaskError(&taskdto.TaskError{StatusCode: tt.status, Message: rawMessage, Error: errors.New(rawMessage), Code: "private_code"})
			assert.Equal(t, tt.want, task.Message)
			assert.EqualError(t, task.Error, tt.want)
		})
	}
}
