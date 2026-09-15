package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOaiStreamHandlerDoesNotForwardUpstreamError(t *testing.T) {
	c, recorder, info, resp := newUpstreamStreamErrorFixture(t,
		`data: {"error":{"message":"All available accounts exhausted (request id: upstream-id)","type":"provider_private","code":"pool_empty"}}`+"\n\n",
		relayconstant.RelayModeChatCompletions,
	)

	usage, relayErr := OaiStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, relayErr)
	require.Contains(t, relayErr.Error(), "All available accounts exhausted")
	require.Empty(t, recorder.Body.String())
	require.NotContains(t, recorder.Body.String(), "upstream-id")
}

func TestOaiResponsesStreamHandlerDoesNotForwardFailedEvent(t *testing.T) {
	c, recorder, info, resp := newUpstreamStreamErrorFixture(t,
		`data: {"type":"response.failed","response":{"error":{"message":"private node 49 exhausted","type":"provider_private","code":"pool_empty"}}}`+"\n\n",
		relayconstant.RelayModeResponses,
	)

	usage, relayErr := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, relayErr)
	require.Contains(t, relayErr.Error(), "private node 49 exhausted")
	require.Empty(t, recorder.Body.String())
	require.NotContains(t, recorder.Body.String(), "node 49")
}

func newUpstreamStreamErrorFixture(t *testing.T, body string, relayMode int) (*gin.Context, *httptest.ResponseRecorder, *relaycommon.RelayInfo, *http.Response) {
	t.Helper()
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/test", nil)
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatOpenAI,
		RelayMode:   relayMode,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	return c, recorder, info, resp
}
