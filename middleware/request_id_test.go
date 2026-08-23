package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

func TestRequestIdPublishesAnthropicPublicHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestId())
	r.GET("/v1/messages", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/messages", nil)
	r.ServeHTTP(rec, req)

	public := rec.Header().Get("request-id")
	internal := rec.Header().Get(common.RequestIdKey)
	if !strings.HasPrefix(public, "req_") {
		t.Fatalf("request-id = %q, want req_ prefix", public)
	}
	if internal == "" {
		t.Fatal("missing internal X-Oneapi-Request-Id")
	}
	if public == internal {
		t.Fatalf("public request-id must not reuse the internal date-prefixed id: %q", public)
	}
}
