package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestShouldCopyUpstreamHeaderUsesPublicAllowlist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	require.True(t, ShouldCopyUpstreamHeader(c, "Content-Type", []string{"audio/mpeg"}))
	require.True(t, ShouldCopyUpstreamHeader(c, "Content-Disposition", []string{"attachment"}))
	require.False(t, ShouldCopyUpstreamHeader(c, "Server", []string{"private-gateway"}))
	require.False(t, ShouldCopyUpstreamHeader(c, "Via", []string{"internal-proxy"}))
	require.False(t, ShouldCopyUpstreamHeader(c, "X-New-Api-Version", []string{"private-version"}))
	require.False(t, ShouldCopyUpstreamHeader(c, "Set-Cookie", []string{"private=value"}))
	require.False(t, ShouldCopyUpstreamHeader(c, "request-id", []string{"req_upstream"}))
	require.Equal(t, "req_upstream", c.GetString(common.UpstreamRequestIdKey))
	require.False(t, ShouldCopyUpstreamHeader(c, common.RequestIdKey, []string{"internal-upstream-id"}))
	require.Equal(t, "internal-upstream-id", c.GetString(common.UpstreamRequestIdKey))

	response := &http.Response{Header: http.Header{"Content-Type": []string{"application/json"}}}
	require.True(t, ShouldCopyUpstreamHeader(c, "Content-Type", response.Header.Values("Content-Type")))
}
