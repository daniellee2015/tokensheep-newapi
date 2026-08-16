package relay

import (
	"encoding/base64"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestShouldPreserveMappedEffortVariantForCPA(t *testing.T) {
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	info.IsModelMapped = true
	info.ChannelBaseUrl = "https://cpa.muxpay.xyz"

	require.True(t, shouldPreserveMappedEffortVariant(info))
}

func TestShouldNotPreserveUnmappedEffortVariant(t *testing.T) {
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	info.IsModelMapped = false
	info.ChannelBaseUrl = "https://api.anthropic.com"

	require.False(t, shouldPreserveMappedEffortVariant(info))
}

func TestNormalizeNativeClaudeRequestAddsOutputFormatSystemInstruction(t *testing.T) {
	request := &dto.ClaudeRequest{
		Model: "claude-opus-5",
		OutputFormat: []byte(`{
			"type":"json_schema",
			"schema":{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"]}
		}`),
		Messages: []dto.ClaudeMessage{{Role: "user", Content: "return json"}},
	}

	normalizeNativeClaudeRequest(request)

	system := request.ParseSystem()
	require.Len(t, system, 1)
	require.NotNil(t, system[0].Text)
	require.Contains(t, *system[0].Text, "valid JSON object only")
	require.Contains(t, *system[0].Text, `"required":["answer"]`)
}

func TestNormalizeNativeClaudeRequestAddsPDFTextFallback(t *testing.T) {
	pdfData := base64.StdEncoding.EncodeToString([]byte("%PDF-1.4\nBT /F1 24 Tf 72 720 Td (MAGIC_PDF_WORD: BANANA123) Tj ET"))
	request := &dto.ClaudeRequest{
		Model: "claude-opus-5",
		Messages: []dto.ClaudeMessage{
			{
				Role: "user",
				Content: []dto.ClaudeMediaMessage{
					{
						Type: "document",
						Source: &dto.ClaudeMessageSource{
							Type:      "base64",
							MediaType: "application/pdf",
							Data:      pdfData,
						},
					},
				},
			},
		},
	}

	normalizeNativeClaudeRequest(request)

	content, err := request.Messages[0].ParseContent()
	require.NoError(t, err)
	require.Len(t, content, 2)
	require.Equal(t, "document", content[0].Type)
	require.Equal(t, "text", content[1].Type)
	require.NotNil(t, content[1].Text)
	require.Contains(t, *content[1].Text, "MAGIC_PDF_WORD: BANANA123")
}
