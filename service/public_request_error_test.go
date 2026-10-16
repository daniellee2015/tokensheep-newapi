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

func TestPublicRequestErrorsPreserveProviderGuidance(t *testing.T) {
	tests := []struct{ name, message string }{
		{"Gemini safety", "request blocked by Gemini API: PROHIBITED_CONTENT"},
		{"security policy", "该会话已被网络安全策略屏蔽，请开启新会话 / This session is blocked by cyber-security policy"},
		{"safety explanation", "Your request was rejected due to content_policy_violation. Please revise the submitted content."},
		{"Gemini context 128K", "The input token count exceeds the maximum number of tokens allowed 131072."},
		{"Gemini context 1M", "The input token count exceeds the maximum number of tokens allowed (1048576)."},
		{"OpenAI context", "This model's maximum context length is 16385 tokens. However, your messages resulted in 17000 tokens."},
		{"unsupported field", `Invalid JSON payload received. Unknown name "requests" at 'request': Cannot find field.`},
		{"function name", "Invalid function name. Must start with a letter or an underscore, with a maximum length of 128."},
		{"tool sequence", "Please ensure that function call turn comes immediately after a user turn or after a function response turn."},
		{"missing tool response id", "Invalid function call history: missing the function response ID"},
		{"unknown validation", "Request contains an invalid argument."},
		{"model turn", "Requests ending with a model turn are not supported."},
		{"missing content", "contents is not specified"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rawMessage := tt.message + " (request id: inner-id) (request-id: outer-id)"
			body, err := common.Marshal(map[string]any{"error": types.OpenAIError{
				Message: rawMessage, Type: "provider_private_type", Code: "provider_private_code",
				Param: "private_parameter", Metadata: []byte(`{"secret":"private"}`),
			}})
			require.NoError(t, err)
			raw := RelayErrorHandler(context.Background(), &http.Response{
				StatusCode: http.StatusBadRequest, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body))),
			}, false)
			require.NotNil(t, raw)
			originalDiagnostic := raw.Error()
			public := PublicUpstreamError(raw)
			require.NotNil(t, public)
			assert.Equal(t, tt.message, public.Error())
			assert.Equal(t, tt.message, public.ToOpenAIError().Message)
			assert.Equal(t, tt.message, public.ToClaudeError().Message)
			assert.Equal(t, "status_code=400, "+tt.message, public.MaskSensitiveErrorWithStatusCode(), "user log must preserve the same explanation")
			assert.Equal(t, http.StatusBadRequest, public.StatusCode)
			assert.Equal(t, types.ErrorCodeInvalidRequest, public.GetErrorCode())
			assert.Empty(t, public.ToOpenAIError().Param)
			assert.Empty(t, public.ToOpenAIError().Metadata)
			assert.Empty(t, public.Metadata)
			assert.Equal(t, originalDiagnostic, raw.Error(), "admin diagnostics must retain the original")
			task := PublicUpstreamTaskError(&taskdto.TaskError{StatusCode: 400, Message: rawMessage, Error: errors.New(rawMessage), Code: "private_code"})
			assert.Equal(t, tt.message, task.Message)
			assert.EqualError(t, task.Error, tt.message)
		})
	}
}

func TestPublicRequestErrorsStillHideOperationalDetails(t *testing.T) {
	tests := []struct{ name, raw string }{
		{"account pool", "All available accounts exhausted at https://internal.example"},
		{"group", "No available channel for model gemini-4 under group private-group (distributor)"},
		{"balance", "用户额度不足, 剩余额度: ＄0.034248, 需要预扣费额度: ＄0.054788"},
		{"price", "Model price is $0.123 per token"},
		{"English quota", "Insufficient quota for account 53"},
		{"routing", "unknown provider for model gemini-4-argon-high"},
		{"model unavailable", "This model is not available."},
		{"mixed safety and pool", "PROHIBITED_CONTENT; account pool empty: 0/247"},
		{"mixed context and price", "context_length_exceeded; upstream price: 0.123"},
		{"credential", "Invalid argument; secret=sk-private-secret"},
		{"JSON credential", `Invalid argument; {"secret":"private"}`},
		{"node", "private invalid argument on node-49"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := types.NewOpenAIError(errors.New(tt.raw), types.ErrorCodeInvalidRequest, http.StatusBadRequest)
			public := PublicUpstreamError(raw)
			require.NotNil(t, public)
			assert.Equal(t, publicServiceUnavailableMessage, public.Error())
			assert.Equal(t, http.StatusBadRequest, public.StatusCode)
			assert.Equal(t, tt.raw, raw.Error())
		})
	}
}

func TestPublicRequestMessagesKeepOtherStatusPolicies(t *testing.T) {
	tests := []struct {
		status  int
		message string
	}{
		{429, publicRateLimitMessage}, {503, publicServiceUnavailableMessage},
	}
	for _, tt := range tests {
		raw := types.NewOpenAIError(errors.New("PROHIBITED_CONTENT; account pool empty"), types.ErrorCodeInvalidRequest, tt.status)
		public := PublicUpstreamError(raw)
		require.NotNil(t, public)
		assert.Equal(t, tt.status, public.StatusCode)
		assert.Equal(t, tt.message, public.Error())
	}
}
