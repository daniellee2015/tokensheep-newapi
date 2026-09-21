package oaichat

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIChatRequestToGeminiGenerateContentOmitsEmptySystemInstruction(t *testing.T) {
	request := dto.GeneralOpenAIRequest{
		Model: "gemini-test",
		Messages: []dto.Message{
			{Role: "system", Content: ""},
			{Role: "developer", Content: "  \n\t"},
			{Role: "user", Content: "hello"},
		},
	}

	converted, err := OpenAIChatRequestToGeminiGenerateContent(t.Context(), request, nil)
	require.NoError(t, err)
	assert.Nil(t, converted.SystemInstructions)

	encoded, err := kitutil.Marshal(converted)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "systemInstruction")
}

func TestOpenAIChatRequestToGeminiGenerateContentKeepsNonemptySystemInstruction(t *testing.T) {
	request := dto.GeneralOpenAIRequest{
		Model: "gemini-test",
		Messages: []dto.Message{
			{Role: "system", Content: ""},
			{Role: "developer", Content: "developer rules"},
			{Role: "user", Content: "hello"},
		},
	}

	converted, err := OpenAIChatRequestToGeminiGenerateContent(t.Context(), request, nil)
	require.NoError(t, err)
	require.NotNil(t, converted.SystemInstructions)
	require.Len(t, converted.SystemInstructions.Parts, 1)
	assert.Equal(t, "developer rules", converted.SystemInstructions.Parts[0].Text)
}
