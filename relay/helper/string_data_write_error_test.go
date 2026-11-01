package helper

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStringAndObjectDataPropagateRenderErrors(t *testing.T) {
	for name, write := range map[string]func(*gin.Context) error{
		"string": func(c *gin.Context) error { return StringData(c, `{"choices":[{"delta":{"content":"EARLY"}}]}`) },
		"object": func(c *gin.Context) error {
			return ObjectData(c, map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": "EARLY"}}}})
		},
	} {
		t.Run(name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{}
			info.EnsureContentTiming(c)
			c.Writer = &contentTimingFailWriter{ResponseWriter: c.Writer}
			err := write(c)
			require.ErrorContains(t, err, "synthetic downstream write error")
			assert.True(t, info.ContentTiming.Snapshot().Downstream.FirstContent().IsZero())
		})
	}
}

func TestStringDataDoesNotReturnOlderContextError(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	_ = c.Error(errors.New("older unrelated context error"))
	require.NoError(t, StringData(c, `{"choices":[{"delta":{"content":"EARLY"}}]}`))
	assert.Contains(t, recorder.Body.String(), "EARLY")
}
