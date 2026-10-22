package service

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublicUpstreamErrorsPreserveSafeFailureDetails(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		message    string
		wantStatus int
		want       string
		wantCode   types.ErrorCode
	}{
		{"supplier 502", 502, "Upstream service temporarily unavailable", 502, "Upstream service temporarily unavailable", types.ErrorCodeBadResponseStatusCode},
		{"empty 502", 502, "", 502, publicServiceUnavailableMessage, types.ErrorCodeBadResponseStatusCode},
		{"private 502", 502, "All available accounts exhausted on node-49", 502, publicServiceUnavailableMessage, types.ErrorCodeBadResponseStatusCode},
		{"safety 403", 403, "Your request was rejected due to content_policy_violation. Please revise the submitted content.", 403, "Your request was rejected due to content_policy_violation. Please revise the submitted content.", types.ErrorCodeInvalidRequest},
		{"content filter 403", 403, "Your request was blocked by our content filter.", 403, "Your request was blocked by our content filter.", types.ErrorCodeInvalidRequest},
		{"security policy 403", 403, "该会话已被网络安全策略屏蔽，请开启新会话", 403, "该会话已被网络安全策略屏蔽，请开启新会话", types.ErrorCodeInvalidRequest},
		{"context 422", 422, "The input token count exceeds the maximum number of tokens allowed 131072.", 422, "The input token count exceeds the maximum number of tokens allowed 131072.", types.ErrorCodeInvalidRequest},
		{"wrapped context 503", 503, "context_length_exceeded: reduce the conversation below 131072 tokens.", 503, "context_length_exceeded: reduce the conversation below 131072 tokens.", types.ErrorCodeServiceUnavailable},
		{"wrapped long prompt 500", 500, "prompt is too long: 213062 tokens > 200000 maximum", 500, "prompt is too long: 213062 tokens > 200000 maximum", types.ErrorCodeServiceUnavailable},
		{"private safety 403", 403, "PROHIBITED_CONTENT; account pool empty: 0/247", 503, publicServiceUnavailableMessage, types.ErrorCodeServiceUnavailable},
		{"safety service failure", 500, "The safety service is unavailable", 503, publicServiceUnavailableMessage, types.ErrorCodeServiceUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := types.WithOpenAIError(types.OpenAIError{
				Message: test.message + " (request id: supplier-id)",
				Type:    "private-type", Code: "private-code", Param: "private-field",
				Metadata: []byte(`{"secret":"private-secret"}`),
			}, test.status)
			raw.SetRetryAfter(time.Second)
			original := raw.Error()
			public := PublicUpstreamError(raw)
			require.NotNil(t, public)
			assert.Equal(t, test.wantStatus, public.StatusCode)
			assert.Equal(t, test.want, public.Error())
			assert.Equal(t, test.wantCode, public.GetErrorCode())
			assert.Equal(t, test.want, public.ToOpenAIError().Message)
			assert.Equal(t, test.want, public.ToClaudeError().Message)
			assert.Equal(t, fmt.Sprintf("status_code=%d, %s", test.wantStatus, test.want), public.MaskSensitiveErrorWithStatusCode())
			assert.Empty(t, public.ToOpenAIError().Metadata)
			assert.Empty(t, public.ToOpenAIError().Param)
			assert.Equal(t, time.Second, public.RetryAfter())
			assert.Equal(t, original, raw.Error(), "retry and admin diagnostics keep the original failure")

			task := PublicUpstreamTaskError(&taskdto.TaskError{
				StatusCode: test.status, Message: test.message, Code: "private-code", Error: errors.New(test.message),
			})
			require.NotNil(t, task)
			assert.Equal(t, test.wantStatus, task.StatusCode)
			assert.Equal(t, test.want, task.Message)
			assert.Equal(t, string(test.wantCode), task.Code)
			assert.EqualError(t, task.Error, test.want)
		})
	}
}

func TestPublicUpstreamBadGatewayDoesNotExposeInfrastructure(t *testing.T) {
	raw := types.NewOpenAIError(errors.New("Upstream service temporarily unavailable at https://internal.example (request id: supplier-id)"), types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway)
	public := PublicUpstreamError(raw)
	require.NotNil(t, public)
	assert.Equal(t, http.StatusBadGateway, public.StatusCode)
	assert.Contains(t, public.Error(), "Upstream service temporarily unavailable")
	assert.NotContains(t, public.Error(), "internal.example")
	assert.NotContains(t, public.Error(), "supplier-id")
}
