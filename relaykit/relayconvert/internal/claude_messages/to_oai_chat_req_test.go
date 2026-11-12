package claudemessages

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func claudeText(value string) *string {
	return &value
}

func TestClaudeMessagesRequestToOpenAIChatPreservesURLImage(t *testing.T) {
	request := dto.ClaudeRequest{
		Model: "claude-test",
		Messages: []dto.ClaudeMessage{
			{
				Role: "user",
				Content: []dto.ClaudeMediaMessage{
					{Type: "text", Text: claudeText("look")},
					{Type: "image", Source: &dto.ClaudeMessageSource{Type: "url", Url: "https://example.com/picture.jpg"}},
				},
			},
		},
	}

	converted, err := ClaudeMessagesRequestToOpenAIChat(request, nil)

	require.NoError(t, err)
	require.Len(t, converted.Messages, 1)
	content := converted.Messages[0].ParseContent()
	require.Len(t, content, 2)
	image := content[1].GetImageMedia()
	require.NotNil(t, image)
	assert.Equal(t, "https://example.com/picture.jpg", image.Url)
	assert.NotContains(t, image.Url, "base64")
}

func TestClaudeMessagesRequestToOpenAIChatKeepsBase64Image(t *testing.T) {
	request := dto.ClaudeRequest{
		Model: "claude-test",
		Messages: []dto.ClaudeMessage{
			{
				Role: "user",
				Content: []dto.ClaudeMediaMessage{
					{Type: "image", Source: &dto.ClaudeMessageSource{Type: "base64", MediaType: "image/png", Data: "aGVsbG8="}},
				},
			},
		},
	}

	converted, err := ClaudeMessagesRequestToOpenAIChat(request, nil)

	require.NoError(t, err)
	require.Len(t, converted.Messages, 1)
	content := converted.Messages[0].ParseContent()
	require.Len(t, content, 1)
	image := content[0].GetImageMedia()
	require.NotNil(t, image)
	assert.Equal(t, "data:image/png;base64,aGVsbG8=", image.Url)
}

func TestClaudeMessagesRequestToOpenAIChatSkipsEmptyImages(t *testing.T) {
	request := dto.ClaudeRequest{
		Model: "claude-test",
		Messages: []dto.ClaudeMessage{
			{
				Role: "user",
				Content: []dto.ClaudeMediaMessage{
					{Type: "text", Text: claudeText("only text survives")},
					{Type: "image", Source: &dto.ClaudeMessageSource{Type: "base64", MediaType: "image/png"}},
					{Type: "image"},
				},
			},
		},
	}

	converted, err := ClaudeMessagesRequestToOpenAIChat(request, nil)

	require.NoError(t, err)
	require.Len(t, converted.Messages, 1)
	content := converted.Messages[0].ParseContent()
	require.Len(t, content, 1)
	assert.Equal(t, "only text survives", content[0].Text)
	for _, part := range content {
		if image := part.GetImageMedia(); image != nil {
			assert.NotContains(t, image.Url, "data:;base64")
		}
	}
}
