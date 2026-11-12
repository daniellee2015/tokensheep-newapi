package oaichat

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	relaymedia "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/media"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIChatRequestToClaudeMessagesNormalizesToolInputSchema(t *testing.T) {
	tests := []struct {
		name       string
		parameters any
		wantSchema map[string]any
	}{
		{
			name:       "omitted parameters",
			parameters: nil,
			wantSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			name: "missing type and properties",
			parameters: map[string]any{
				"additionalProperties": false,
			},
			wantSchema: map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": false,
			},
		},
		{
			name: "non-string type",
			parameters: map[string]any{
				"type":       123,
				"properties": map[string]any{},
			},
			wantSchema: map[string]any{
				"type":       123,
				"properties": map[string]any{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			maxTokens := uint(1024)
			got, err := OpenAIChatRequestToClaudeMessages(context.Background(), nil, dto.GeneralOpenAIRequest{
				Model:     "claude-test",
				MaxTokens: &maxTokens,
				Messages: []dto.Message{
					{Role: "user", Content: "Call the tool."},
				},
				Tools: []dto.ToolCallRequest{
					{
						Type: "function",
						Function: dto.FunctionRequest{
							Name:        "get_current_time",
							Description: "Get the current time",
							Parameters:  tt.parameters,
						},
					},
				},
			})

			require.NoError(t, err)
			tools, ok := got.Tools.([]any)
			require.True(t, ok)
			require.Len(t, tools, 1)
			tool, ok := tools[0].(*dto.Tool)
			require.True(t, ok)
			assert.Equal(t, "get_current_time", tool.Name)
			assert.Equal(t, tt.wantSchema, tool.InputSchema)
		})
	}
}

func TestOpenAIChatRequestToClaudeMessagesNormalizesClaudeImageMediaType(t *testing.T) {
	jpegBase64 := base64.StdEncoding.EncodeToString([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00})
	relaymedia.SetMediaResolver(relaymedia.MediaResolver{
		GetBase64Data: func(context.Context, types.FileSource, ...string) (string, string, error) {
			return jpegBase64, "image/jpg", nil
		},
		DecodeBase64FileData: func(string) (string, string, error) {
			return jpegBase64, "image/jpg", nil
		},
	})
	t.Cleanup(func() { relaymedia.SetMediaResolver(relaymedia.MediaResolver{}) })

	maxTokens := uint(1024)
	got, err := OpenAIChatRequestToClaudeMessages(context.Background(), nil, dto.GeneralOpenAIRequest{
		Model:     "claude-test",
		MaxTokens: &maxTokens,
		Messages: []dto.Message{
			{
				Role: "user",
				Content: []any{
					map[string]any{"type": "text", "text": "look"},
					map[string]any{
						"type":      "image_url",
						"image_url": map[string]any{"url": "https://example.com/picture.jpg"},
					},
				},
			},
		},
	})

	require.NoError(t, err)
	content, ok := got.Messages[0].Content.([]dto.ClaudeMediaMessage)
	require.True(t, ok)
	require.Len(t, content, 2)
	require.Equal(t, "image", content[1].Type)
	require.NotNil(t, content[1].Source)
	require.Equal(t, "image/jpeg", content[1].Source.MediaType)
	require.Equal(t, jpegBase64, content[1].Source.Data)
}

func TestOpenAIChatRequestToClaudeMessagesDegradesUnsupportedImage(t *testing.T) {
	avifBase64 := base64.StdEncoding.EncodeToString(append([]byte{0x00, 0x00, 0x00, 0x1C}, []byte("ftypavif")...))
	relaymedia.SetMediaResolver(relaymedia.MediaResolver{
		GetBase64Data: func(context.Context, types.FileSource, ...string) (string, string, error) {
			return avifBase64, "image/avif", nil
		},
		DecodeBase64FileData: func(string) (string, string, error) {
			return avifBase64, "image/avif", nil
		},
	})
	t.Cleanup(func() { relaymedia.SetMediaResolver(relaymedia.MediaResolver{}) })

	maxTokens := uint(1024)
	got, err := OpenAIChatRequestToClaudeMessages(context.Background(), nil, dto.GeneralOpenAIRequest{
		Model:     "claude-test",
		MaxTokens: &maxTokens,
		Messages: []dto.Message{
			{
				Role: "user",
				Content: []any{
					map[string]any{
						"type":      "image_url",
						"image_url": map[string]any{"url": "https://example.com/picture.avif"},
					},
				},
			},
		},
	})

	require.NoError(t, err)
	content, ok := got.Messages[0].Content.([]dto.ClaudeMediaMessage)
	require.True(t, ok)
	require.Len(t, content, 1)
	require.Equal(t, "text", content[0].Type)
	require.NotNil(t, content[0].Text)
	require.Contains(t, *content[0].Text, "image omitted")
}
