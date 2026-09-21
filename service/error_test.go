package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResetStatusCode(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name             string
		statusCode       int
		statusCodeConfig string
		expectedCode     int
	}{
		{
			name:             "map string value",
			statusCode:       429,
			statusCodeConfig: `{"429":"503"}`,
			expectedCode:     503,
		},
		{
			name:             "map int value",
			statusCode:       429,
			statusCodeConfig: `{"429":503}`,
			expectedCode:     503,
		},
		{
			name:             "skip invalid string value",
			statusCode:       429,
			statusCodeConfig: `{"429":"bad-code"}`,
			expectedCode:     429,
		},
		{
			name:             "skip status code 200",
			statusCode:       200,
			statusCodeConfig: `{"200":503}`,
			expectedCode:     200,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			newAPIError := &types.NewAPIError{
				StatusCode: tc.statusCode,
			}
			ResetStatusCode(newAPIError, tc.statusCodeConfig)
			require.Equal(t, tc.expectedCode, newAPIError.StatusCode)
		})
	}
}

func TestRelayErrorHandlerTruncatesInvalidJSONBodyInLog(t *testing.T) {
	withDebugEnabled(t, false)

	body := strings.Repeat("b", common.LocalLogContentLimit+256)
	var logBuffer bytes.Buffer

	common.LogWriterMu.Lock()
	oldWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &logBuffer
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = oldWriter
		common.LogWriterMu.Unlock()
	})

	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	newAPIError := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, newAPIError)
	require.Equal(t, "bad response status code 500", newAPIError.Error())
	require.Contains(t, logBuffer.String(), "[truncated")
	require.Contains(t, logBuffer.String(), fmt.Sprintf("original_length=%d", len(body)))
	require.NotContains(t, logBuffer.String(), strings.Repeat("b", common.LocalLogContentLimit+1))
}

func TestPublicUpstreamErrorUsesOnlyGatewayControlledFields(t *testing.T) {
	testCases := []struct {
		name           string
		upstreamStatus int
		publicStatus   int
		publicMessage  string
		publicCode     types.ErrorCode
	}{
		{name: "bad request", upstreamStatus: http.StatusBadRequest, publicStatus: http.StatusBadRequest, publicMessage: "Invalid request", publicCode: types.ErrorCodeInvalidRequest},
		{name: "upstream authentication", upstreamStatus: http.StatusUnauthorized, publicStatus: http.StatusServiceUnavailable, publicMessage: "Service temporarily unavailable", publicCode: types.ErrorCodeServiceUnavailable},
		{name: "rate limit", upstreamStatus: http.StatusTooManyRequests, publicStatus: http.StatusTooManyRequests, publicMessage: "Rate limit exceeded. Retry later.", publicCode: types.ErrorCodeRateLimitExceeded},
		{name: "bad gateway", upstreamStatus: http.StatusBadGateway, publicStatus: http.StatusServiceUnavailable, publicMessage: "Service temporarily unavailable", publicCode: types.ErrorCodeServiceUnavailable},
		{name: "invalid status", upstreamStatus: 0, publicStatus: http.StatusServiceUnavailable, publicMessage: "Service temporarily unavailable", publicCode: types.ErrorCodeServiceUnavailable},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			upstream := types.WithOpenAIError(types.OpenAIError{
				Message:  "All available accounts exhausted at https://internal.example (request id: upstream-id)",
				Type:     "provider_private_type",
				Code:     "provider_private_code",
				Metadata: []byte(`{"provider":"private"}`),
			}, tc.upstreamStatus)
			upstream.SetRetryAfter(time.Second)

			public := PublicUpstreamError(upstream)

			require.Equal(t, tc.publicStatus, public.StatusCode)
			require.Equal(t, tc.publicMessage, public.Error())
			require.Equal(t, tc.publicCode, public.GetErrorCode())
			require.Empty(t, public.Metadata)
			require.Equal(t, tc.publicMessage, public.ToOpenAIError().Message)
			require.Equal(t, string(tc.publicCode), public.ToOpenAIError().Type)
			require.Equal(t, tc.publicMessage, public.ToClaudeError().Message)
			require.Equal(t, time.Second, public.RetryAfter())
			require.NotContains(t, public.ToOpenAIError().Message, "accounts")
			require.NotContains(t, public.ToOpenAIError().Message, "upstream-id")
		})
	}
}

func TestPublicUpstreamTaskErrorDropsOriginalError(t *testing.T) {
	upstream := &taskdto.TaskError{
		Code:       "provider_pool_exhausted",
		Message:    "No available accounts on node-49 (request id: upstream-id)",
		StatusCode: http.StatusBadGateway,
		Error:      errors.New("raw provider error"),
	}

	public := PublicUpstreamTaskError(upstream)

	require.Equal(t, http.StatusServiceUnavailable, public.StatusCode)
	require.Equal(t, "service_unavailable", public.Code)
	require.Equal(t, "Service temporarily unavailable", public.Message)
	require.EqualError(t, public.Error, "Service temporarily unavailable")
}

func TestEmbeddedUpstreamErrorRecognizesHTTP200ErrorEnvelopes(t *testing.T) {
	for _, body := range []string{
		`{"error":{"message":"private upstream failure","type":"private_type","code":"private_code"}}`,
		`{"message":"private gateway failure"}`,
	} {
		embeddedErr := EmbeddedUpstreamError([]byte(body))
		require.NotNil(t, embeddedErr)
		require.Equal(t, http.StatusInternalServerError, embeddedErr.StatusCode)
		require.Contains(t, embeddedErr.Error(), "private")
	}

	require.Nil(t, EmbeddedUpstreamError([]byte(`{"text":"successful transcription"}`)))
}

func TestRelayErrorHandlerKeepsStructuredErrorMessage(t *testing.T) {
	message := strings.Repeat("c", common.LocalLogContentLimit+256)
	body := `{"message":"` + message + `"}`
	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	newAPIError := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, newAPIError)
	require.Equal(t, message, newAPIError.Error())
}

func TestRelayErrorHandlerKeepsOpenAIErrorMessage(t *testing.T) {
	message := strings.Repeat("d", common.LocalLogContentLimit+256)
	body := `{"error":{"message":"` + message + `","type":"server_error","code":"server_error"}}`
	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	newAPIError := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, newAPIError)
	require.Equal(t, message, newAPIError.Error())
}

func TestRelayErrorHandlerPreservesRetryAfter(t *testing.T) {
	tests := []struct {
		name       string
		header     string
		want       time.Duration
		statusCode int
	}{
		{name: "delta seconds", header: "1", want: time.Second, statusCode: http.StatusTooManyRequests},
		{name: "http date", header: time.Now().UTC().Add(3 * time.Second).Format(http.TimeFormat), want: 3 * time.Second, statusCode: http.StatusServiceUnavailable},
		{name: "invalid", header: "later", statusCode: http.StatusTooManyRequests},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Second)
			header := test.header
			if test.name == "http date" {
				header = now.Add(test.want).Format(http.TimeFormat)
			}
			resp := &http.Response{
				StatusCode: test.statusCode,
				Header:     http.Header{"Retry-After": []string{header}},
				Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"busy"}}`)),
			}

			newAPIError := relayErrorHandlerAt(context.Background(), resp, false, now)

			require.NotNil(t, newAPIError)
			require.Equal(t, test.want, newAPIError.RetryAfter())
		})
	}
}

func TestRelayErrorHandlerPacesModelCapacityFallbackWithoutRetryHeader(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		want       time.Duration
	}{
		{
			name:       "structured capacity reason",
			statusCode: http.StatusServiceUnavailable,
			body:       `{"error":{"code":503,"message":"No capacity available for model gemini-test on the server","status":"UNAVAILABLE","details":[{"reason":"MODEL_CAPACITY_EXHAUSTED"}]}}`,
			want:       time.Second,
		},
		{
			name:       "temporary unavailable retry message",
			statusCode: http.StatusServiceUnavailable,
			body:       `{"error":{"code":503,"message":"Model gemini-test is temporarily unavailable. Retry in 1s."}}`,
			want:       time.Second,
		},
		{
			name:       "generic service unavailable",
			statusCode: http.StatusServiceUnavailable,
			body:       `{"error":{"code":503,"message":"upstream connection failed"}}`,
		},
		{
			name:       "capacity text on bad request",
			statusCode: http.StatusBadRequest,
			body:       `{"error":{"code":400,"message":"No capacity available for model gemini-test on the server"}}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: test.statusCode,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(test.body)),
			}

			newAPIError := RelayErrorHandler(context.Background(), resp, false)

			require.NotNil(t, newAPIError)
			require.Equal(t, test.want, newAPIError.RetryAfter())
		})
	}
}

func TestRelayErrorHandlerKeepsInvalidJSONBodyInDebugLog(t *testing.T) {
	withDebugEnabled(t, true)

	body := strings.Repeat("e", common.LocalLogContentLimit+256)
	var logBuffer bytes.Buffer

	common.LogWriterMu.Lock()
	oldWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &logBuffer
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = oldWriter
		common.LogWriterMu.Unlock()
	})

	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	newAPIError := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, newAPIError)
	require.NotContains(t, logBuffer.String(), "[truncated")
	require.Contains(t, logBuffer.String(), body)
}

func withDebugEnabled(t *testing.T, enabled bool) {
	t.Helper()

	oldDebug := common.DebugEnabled
	common.DebugEnabled = enabled
	t.Cleanup(func() {
		common.DebugEnabled = oldDebug
	})
}
