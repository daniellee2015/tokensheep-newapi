package oairesponses

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	relaymedia "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/media"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/stretchr/testify/require"
)

func newResponsesImageRequest(input string) *dto.OpenAIResponsesRequest {
	maxOutputTokens := uint(1024)
	return &dto.OpenAIResponsesRequest{
		Model:           "claude-test",
		MaxOutputTokens: &maxOutputTokens,
		Input:           []byte(input),
	}
}

func TestOpenAIResponsesRequestToClaudeMessagesNormalizesClaudeImageMediaType(t *testing.T) {
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

	got, err := OpenAIResponsesRequestToClaudeMessages(context.Background(), nil, newResponsesImageRequest(
		`[{"type":"message","role":"user","content":[{"type":"input_text","text":"look"},`+
			`{"type":"input_image","image_url":"https://example.com/picture.jpg"}]}]`,
	))

	require.NoError(t, err)
	require.Len(t, got.Messages, 1)
	content, ok := got.Messages[0].Content.([]dto.ClaudeMediaMessage)
	require.True(t, ok)
	require.Len(t, content, 2)
	require.Equal(t, "image", content[1].Type)
	require.NotNil(t, content[1].Source)
	require.Equal(t, "image/jpeg", content[1].Source.MediaType)
	require.Equal(t, jpegBase64, content[1].Source.Data)
}

func TestOpenAIResponsesRequestToClaudeMessagesDegradesUnsupportedImage(t *testing.T) {
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

	got, err := OpenAIResponsesRequestToClaudeMessages(context.Background(), nil, newResponsesImageRequest(
		`[{"type":"message","role":"user","content":[`+
			`{"type":"input_image","image_url":"https://example.com/picture.avif"}]}]`,
	))

	require.NoError(t, err)
	require.Len(t, got.Messages, 1)
	content, ok := got.Messages[0].Content.([]dto.ClaudeMediaMessage)
	require.True(t, ok)
	require.Len(t, content, 1)
	require.Equal(t, "text", content[0].Type)
	require.NotNil(t, content[0].Text)
	require.Contains(t, *content[0].Text, "image omitted")
}
