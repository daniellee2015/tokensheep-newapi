package service

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestResponseOpenAI2ClaudeNormalizesMessageID(t *testing.T) {
	resp := &dto.OpenAITextResponse{
		Id: "chatcmpl-abc",
		Choices: []dto.OpenAITextResponseChoice{
			{
				FinishReason: "stop",
				Message: dto.Message{
					Role: "assistant",
				},
			},
		},
	}
	resp.Choices[0].Message.SetStringContent("ok")

	got := ResponseOpenAI2Claude(resp, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}})

	require.True(t, strings.HasPrefix(got.Id, "msg_"))
	require.NotContains(t, got.Id, "-")
	require.NotEqual(t, "chatcmpl-abc", got.Id)
}

func TestClaudeToOpenAIRequestMapsOutputFormatToJSONSchema(t *testing.T) {
	req := dto.ClaudeRequest{
		Model: "claude-opus-5",
		OutputFormat: []byte(`{
			"type":"json_schema",
			"name":"answer",
			"description":"strict answer",
			"strict":true,
			"schema":{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"]}
		}`),
		Messages: []dto.ClaudeMessage{{Role: "user", Content: "return json"}},
	}

	got, err := ClaudeToOpenAIRequest(req, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}})

	require.NoError(t, err)
	require.NotNil(t, got.ResponseFormat)
	require.Equal(t, "json_schema", got.ResponseFormat.Type)
	require.JSONEq(t, `{
		"name":"answer",
		"description":"strict answer",
		"strict":true,
		"schema":{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"]}
	}`, string(got.ResponseFormat.JsonSchema))
}
