package controller

import (
	"context"
	"fmt"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIndependentFallbackRetryBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name                                         string
		code                                         int
		delay                                        time.Duration
		remaining                                    int
		channel                                      int
		configured, cancelled, written, pinned, skip bool
		want                                         bool
	}{
		{name: "quota reaches independent pool", code: 429, remaining: 2, channel: 12, configured: true, want: true},
		{name: "long quota reset only blocks its pool", code: 429, delay: 4 * time.Hour, remaining: 1, channel: 140, configured: true, want: true},
		{name: "capacity reaches reserve", code: 503, delay: 60 * time.Second, remaining: 1, channel: 140, configured: true, want: true},
		{name: "legacy quota remains terminal", code: 429, remaining: 2, channel: 12},
		{name: "budget exhausted", code: 429, remaining: 0, channel: 140, configured: true},
		{name: "reserve is terminal", code: 429, remaining: 1, channel: 146, configured: true},
		{name: "request error", code: 400, remaining: 2, channel: 12, configured: true},
		{name: "cancelled", code: 503, remaining: 2, channel: 12, configured: true, cancelled: true},
		{name: "committed SSE", code: 503, remaining: 2, channel: 12, configured: true, written: true},
		{name: "pinned channel", code: 429, remaining: 2, channel: 12, configured: true, pinned: true},
		{name: "explicit skip", code: 429, remaining: 2, channel: 12, configured: true, skip: true},
		{name: "client close", code: 499, remaining: 2, channel: 12, configured: true},
		{name: "stream break", code: 598, remaining: 2, channel: 12, configured: true},
		{name: "existing timeout policy", code: 504, remaining: 2, channel: 12, configured: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
			c.Set("channel_id", tc.channel)
			if tc.configured {
				c.Set("independent_channel_fallback", &operation_setting.IndependentChannelFallback{PrimaryChannelIDs: []int{12, 140}, ReserveChannelID: 146})
			}
			if tc.cancelled {
				ctx, cancel := context.WithCancel(c.Request.Context())
				cancel()
				c.Request = c.Request.WithContext(ctx)
			}
			if tc.written {
				_, err := c.Writer.Write([]byte(": heartbeat\n\n"))
				require.NoError(t, err)
			}
			if tc.pinned {
				c.Set("specific_channel_id", 12)
			}
			err := types.NewOpenAIError(fmt.Errorf("Resource has been exhausted (e.g. check quota)."), types.ErrorCode("upstream_error"), tc.code)
			if tc.skip {
				err = types.NewErrorWithStatusCode(fmt.Errorf("skip retry"), types.ErrorCode("skip"), tc.code, types.ErrOptionWithSkipRetry())
			}
			err.SetRetryAfter(tc.delay)
			assert.Equal(t, tc.want, shouldRetry(c, err, tc.remaining))
		})
	}
}
