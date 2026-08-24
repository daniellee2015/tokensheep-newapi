package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
)

func TestAbortWithOpenAiMessageUsesAnthropicEnvelopeOnMessages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	abortWithOpenAiMessage(c, http.StatusNotFound, "no channel", types.ErrorCodeModelNotFound)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["type"] != "error" {
		t.Fatalf("top-level type = %v, want error", body["type"])
	}
	errObj, _ := body["error"].(map[string]any)
	if errObj["type"] != "not_found_error" {
		t.Fatalf("error.type = %v, want not_found_error", errObj["type"])
	}
}

func TestAnthropicModelNotFoundStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	if got := anthropicModelNotFoundStatus(c); got != http.StatusNotFound {
		t.Fatalf("messages status = %d, want 404", got)
	}
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	if got := anthropicModelNotFoundStatus(c); got != http.StatusServiceUnavailable {
		t.Fatalf("openai status = %d, want 503", got)
	}
}
