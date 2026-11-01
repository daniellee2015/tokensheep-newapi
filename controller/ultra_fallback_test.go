package controller

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUltraFallbackHandlesUpstreamModelUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name, body                       string
		scoped, written                  bool
		channel, remaining, publicStatus int
		retry                            bool
	}{
		{"CPA empty model pool", `{"error":{"message":"unknown provider for model gemini-4-argon-high","type":"invalid_request_error","code":"model_not_found","param":"model"}}`, true, false, 148, 2, 503, true},
		{"ordinary client model error", `{"error":{"message":"Invalid model name","type":"invalid_request_error","code":"model_not_found"}}`, true, false, 148, 2, 400, false},
		{"invalid request", `{"error":{"message":"Invalid messages","type":"invalid_request_error","code":"invalid_request"}}`, true, false, 148, 2, 400, false},
		{"outside configured scope", `{"error":{"message":"unknown provider for model gemini-4-argon-high","code":"model_not_found"}}`, false, false, 148, 2, 503, false},
		{"stream already started", `{"error":{"message":"unknown provider for model gemini-4-argon-high","code":"model_not_found"}}`, true, true, 148, 2, 503, false},
		{"reserve terminal", `{"error":{"message":"unknown provider for model gemini-4-argon-high","code":"model_not_found"}}`, true, false, 159, 2, 503, false},
		{"budget exhausted", `{"error":{"message":"unknown provider for model gemini-4-argon-high","code":"model_not_found"}}`, true, false, 148, 0, 503, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
			c.Set("channel_id", tc.channel)
			if tc.scoped {
				c.Set("independent_channel_fallback", &operation_setting.IndependentChannelFallback{PrimaryChannelIDs: []int{148, 161}, ReserveChannelID: 159})
			}
			if tc.written {
				_, err := c.Writer.Write([]byte(": heartbeat\n\n"))
				require.NoError(t, err)
			}
			err := service.RelayErrorHandler(context.Background(), &http.Response{StatusCode: 400, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.body))}, false)
			require.NotNil(t, err)
			assert.Equal(t, tc.retry, shouldRetry(c, err, tc.remaining))
			assert.Equal(t, tc.publicStatus, service.PublicUpstreamError(err).StatusCode)
			assert.Equal(t, 400, err.StatusCode, "retain raw upstream status for diagnostics")
		})
	}
}
