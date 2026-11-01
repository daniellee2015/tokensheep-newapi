package channel

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

var requestIDHTTPClientOnce sync.Once

func requestIDTestContext(t *testing.T) *gin.Context {
	t.Helper()
	requestIDHTTPClientOnce.Do(service.InitHttpClient)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(common.RequestIdKey, "local-request")
	c.Writer.Header().Set(common.RequestIdKey, "local-request")
	return c
}

func requestIDTestCall(t *testing.T, c *gin.Context, url string, stream bool, canceled bool) (*http.Response, error) {
	t.Helper()
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader("{}"))
	c.Request.Header.Set("request-id", "caller-must-not-be-adopted")
	c.Request.Header.Set("x-request-id", "caller-must-not-be-adopted")
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader("{}"))
	require.NoError(t, err)
	if canceled {
		ctx, cancel := context.WithCancel(req.Context())
		cancel()
		req = req.WithContext(ctx)
	}
	return DoRequest(c, req, &relaycommon.RelayInfo{
		IsStream: stream, DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{},
	})
}

func TestUpstreamRequestIDHTTPHeadersAcrossOutcomes(t *testing.T) {
	cases := []struct {
		name    string
		headers map[string]string
		want    string
	}{
		{"legacy", map[string]string{common.RequestIdKey: "legacy"}, "legacy"},
		{"standard", map[string]string{"request-id": "req_011ActualTrace"}, "req_011ActualTrace"},
		{"empty_standard_legacy", map[string]string{"request-id": "", common.RequestIdKey: "legacy"}, "legacy"},
		{"anthropic_priority", map[string]string{common.RequestIdKey: "legacy", "request-id": "req_011ActualTrace", "x-request-id": "x-standard"}, "req_011ActualTrace"},
		{"standard_priority", map[string]string{"request-id": "req_011ActualTrace", "x-request-id": "x-standard"}, "req_011ActualTrace"},
		{"empty_legacy_fallback", map[string]string{common.RequestIdKey: "", "request-id": "req_011ActualTrace"}, "req_011ActualTrace"},
	}
	for _, tc := range cases {
		for _, stream := range []bool{false, true} {
			for _, status := range []int{http.StatusOK, http.StatusBadRequest, http.StatusBadGateway} {
				t.Run(tc.name+"/"+http.StatusText(status)+map[bool]string{false: "/json", true: "/sse"}[stream], func(t *testing.T) {
					body := `{"fixture":true}`
					if stream && status == http.StatusOK {
						body = "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
					}
					upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						for key, value := range tc.headers {
							w.Header().Set(key, value)
						}
						if stream && status == http.StatusOK {
							w.Header().Set("Content-Type", "text/event-stream")
						} else {
							w.Header().Set("Content-Type", "application/json")
						}
						w.WriteHeader(status)
						_, _ = io.WriteString(w, body)
					}))
					t.Cleanup(upstream.Close)
					c := requestIDTestContext(t)
					response, err := requestIDTestCall(t, c, upstream.URL, stream, false)
					require.NoError(t, err)
					defer response.Body.Close()
					actual, err := io.ReadAll(response.Body)
					require.NoError(t, err)
					require.Equal(t, status, response.StatusCode)
					require.Equal(t, body, string(actual))
					require.Equal(t, tc.want, c.GetString(common.UpstreamRequestIdKey), "request boundary must select the existing preferred ID")
					for _, key := range []string{"request-id", "x-request-id", common.RequestIdKey} {
						if values := response.Header.Values(key); len(values) > 0 {
							service.ShouldCopyUpstreamHeader(c, key, values)
						}
					}
					require.Equal(t, tc.want, c.GetString(common.UpstreamRequestIdKey))
					require.Equal(t, "local-request", c.GetString(common.RequestIdKey))
					require.Equal(t, "local-request", c.Writer.Header().Get(common.RequestIdKey))
				})
			}
		}
	}
}

func TestUpstreamRequestIDDoesNotLeakPreviousAttempt(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "response_without_id", true: "transport_error"}[canceled], func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 1 {
					w.Header().Set(common.RequestIdKey, "previous-attempt")
				}
				_, _ = io.WriteString(w, "{}")
			}))
			t.Cleanup(upstream.Close)
			c := requestIDTestContext(t)
			first, err := requestIDTestCall(t, c, upstream.URL, false, false)
			require.NoError(t, err)
			_, err = io.Copy(io.Discard, first.Body)
			require.NoError(t, err)
			first.Body.Close()
			require.Equal(t, "previous-attempt", c.GetString(common.UpstreamRequestIdKey))
			second, err := requestIDTestCall(t, c, upstream.URL, false, canceled)
			if canceled {
				require.Error(t, err)
				require.Nil(t, second)
			} else {
				require.NoError(t, err)
				second.Body.Close()
			}
			require.Empty(t, c.GetString(common.UpstreamRequestIdKey))
			require.Equal(t, "local-request", c.GetString(common.RequestIdKey))
		})
	}
}

func TestUpstreamRequestIDSurvivesEmptyLegacyHeaderCopy(t *testing.T) {
	c := requestIDTestContext(t)
	c.Set(common.UpstreamRequestIdKey, "req_011ActualTrace")
	require.False(t, service.ShouldCopyUpstreamHeader(c, common.RequestIdKey, []string{""}))
	require.Equal(t, "req_011ActualTrace", c.GetString(common.UpstreamRequestIdKey))
	require.Equal(t, "local-request", c.GetString(common.RequestIdKey))
}
