package types

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSetMessage_UpdatesBothRenderPaths pins the contract that SetMessage is
// visible on every render path. ToOpenAIError/ToClaudeError read the message
// from RelayError when the error carries a provider-shaped payload, so updating
// only Err would silently drop the new message for exactly those errors — which
// is how upstream (provider-shaped) errors arrive.
func TestSetMessage_UpdatesBothRenderPaths(t *testing.T) {
	const replacement = "Service temporarily unavailable"

	t.Run("openai-shaped error", func(t *testing.T) {
		err := WithOpenAIError(OpenAIError{
			Message: "internal pool detail",
			Type:    "server_error",
			Code:    ErrorCodeBadResponseStatusCode,
		}, http.StatusServiceUnavailable)

		err.SetMessage(replacement)

		assert.Equal(t, replacement, err.ToOpenAIError().Message)
		assert.Equal(t, replacement, err.ToClaudeError().Message)
		assert.Equal(t, replacement, err.Error())
	})

	t.Run("claude-shaped error", func(t *testing.T) {
		err := WithClaudeError(ClaudeError{
			Message: "internal pool detail",
			Type:    "overloaded_error",
		}, http.StatusServiceUnavailable)

		err.SetMessage(replacement)

		assert.Equal(t, replacement, err.ToClaudeError().Message)
		assert.Equal(t, replacement, err.ToOpenAIError().Message)
		assert.Equal(t, replacement, err.Error())
	})

	t.Run("plain error without a relay payload", func(t *testing.T) {
		err := NewError(errors.New("internal pool detail"), ErrorCodeBadResponse)

		err.SetMessage(replacement)

		assert.Equal(t, replacement, err.Error())
		assert.Equal(t, replacement, err.ToOpenAIError().Message)
		assert.Equal(t, replacement, err.ToClaudeError().Message)
	})

	t.Run("preserves fields other than the message", func(t *testing.T) {
		err := WithOpenAIError(OpenAIError{
			Message: "original",
			Type:    "server_error",
			Param:   "model",
			Code:    ErrorCodeBadResponseStatusCode,
		}, http.StatusServiceUnavailable)

		err.SetMessage(replacement)
		rendered := err.ToOpenAIError()

		assert.Equal(t, "server_error", rendered.Type)
		assert.Equal(t, "model", rendered.Param)
		assert.Equal(t, ErrorCodeBadResponseStatusCode, rendered.Code)
		assert.Equal(t, http.StatusServiceUnavailable, err.StatusCode)
	})

	t.Run("nil receiver is safe", func(t *testing.T) {
		var err *NewAPIError
		assert.NotPanics(t, func() { err.SetMessage(replacement) })
	})
}
