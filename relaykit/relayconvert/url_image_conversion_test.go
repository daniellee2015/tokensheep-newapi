package relayconvert

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The test media resolver is installed in TestMain (golden_test.go) and returns
// a fixed base64 payload with image/png for every resolved file source.
func TestClaudeURLImageSurvivesGeminiConversion(t *testing.T) {
	request := &dto.ClaudeRequest{
		Model: "claude-test",
		Messages: []dto.ClaudeMessage{
			{
				Role: "user",
				Content: []dto.ClaudeMediaMessage{
					{Type: "image", Source: &dto.ClaudeMessageSource{Type: "url", Url: "https://example.com/picture.png"}},
				},
			},
		},
	}

	result, err := ConvertRequest(context.Background(), nil, types.RelayFormatGemini, request)

	require.NoError(t, err)
	geminiRequest, ok := result.Value.(*dto.GeminiChatRequest)
	require.True(t, ok)
	require.NotEmpty(t, geminiRequest.Contents)
	var inlineParts []dto.GeminiPart
	for _, content := range geminiRequest.Contents {
		for _, part := range content.Parts {
			if part.InlineData != nil {
				inlineParts = append(inlineParts, part)
			}
		}
	}
	require.Len(t, inlineParts, 1)
	assert.Equal(t, "image/png", inlineParts[0].InlineData.MimeType)
	assert.Equal(t, "aGVsbG8=", inlineParts[0].InlineData.Data)
}
