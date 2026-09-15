package openai

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenaiAudioHandlersDoNotForwardUpstreamErrorBody(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		run  func(*gin.Context, *http.Response) *types.NewAPIError
	}{
		{
			name: "speech",
			run: func(c *gin.Context, resp *http.Response) *types.NewAPIError {
				_, apiErr := OpenaiTTSHandler(c, resp, &relaycommon.RelayInfo{})
				return apiErr
			},
		},
		{
			name: "transcription",
			run: func(c *gin.Context, resp *http.Response) *types.NewAPIError {
				apiErr, _ := OpenaiSTTHandler(c, resp, &relaycommon.RelayInfo{}, "json")
				return apiErr
			},
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			gin.SetMode(gin.TestMode)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio", nil)
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(bytes.NewBufferString(
					`{"error":{"message":"All available accounts exhausted","type":"private_type","code":"private_code"}}`,
				)),
			}

			apiErr := test.run(c, resp)

			require.NotNil(t, apiErr)
			require.Contains(t, apiErr.Error(), "All available accounts exhausted")
			require.Empty(t, recorder.Body.String())
		})
	}
}
