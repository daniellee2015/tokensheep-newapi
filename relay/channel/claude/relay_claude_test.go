package claude

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func commonPointer[T any](value T) *T {
	return &value
}

func TestResponseOpenAI2ClaudeToolUseInputIsObject(t *testing.T) {
	tests := []struct {
		name string
		args string
		want map[string]interface{}
	}{
		{name: "object", args: `{"q":"x"}`, want: map[string]interface{}{"q": "x"}},
		{name: "empty", args: "", want: map[string]interface{}{}},
		{name: "invalid", args: "{", want: map[string]interface{}{}},
		{name: "null", args: "null", want: map[string]interface{}{}},
		{name: "array", args: `["x"]`, want: map[string]interface{}{}},
		{name: "string", args: `"x"`, want: map[string]interface{}{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := dto.Message{Role: "assistant"}
			msg.SetToolCalls([]dto.ToolCallRequest{
				{
					ID:   "call_1",
					Type: "function",
					Function: dto.FunctionRequest{
						Name:      "lookup",
						Arguments: tt.args,
					},
				},
			})
			resp := service.ResponseOpenAI2Claude(&dto.OpenAITextResponse{
				Id:    "chatcmpl_1",
				Model: "gpt-test",
				Choices: []dto.OpenAITextResponseChoice{
					{Message: msg, FinishReason: "tool_calls"},
				},
			}, nil)

			require.Len(t, resp.Content, 1)
			assert.Equal(t, "tool_use", resp.Content[0].Type)
			assert.Equal(t, tt.want, resp.Content[0].Input)
		})
	}
}

func TestAdaptorConvertOpenAIRequestUsesUpstreamModelName(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{
		Model: "claude-opus-4-8",
		Messages: []dto.Message{
			{
				Role: "user",
			},
		},
	}
	request.Messages[0].SetStringContent("hi")

	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	info.UpstreamModelName = "claude-opus-4-8-medium"

	converted, err := (&Adaptor{}).ConvertOpenAIRequest(nil, info, request)

	require.NoError(t, err)
	claudeRequest, ok := converted.(*dto.ClaudeRequest)
	require.True(t, ok)
	require.Equal(t, "claude-opus-4-8-medium", claudeRequest.Model)
}

func TestFormatClaudeResponseInfo_MessageStart(t *testing.T) {
	claudeInfo := &ClaudeResponseInfo{
		Usage: &dto.Usage{},
	}
	claudeResponse := &dto.ClaudeResponse{
		Type: "message_start",
		Message: &dto.ClaudeMediaMessage{
			Id:    "msg_123",
			Model: "claude-3-5-sonnet",
			Usage: &dto.ClaudeUsage{
				InputTokens:              100,
				OutputTokens:             1,
				CacheCreationInputTokens: 50,
				CacheReadInputTokens:     30,
			},
		},
	}

	ok := FormatClaudeResponseInfo(claudeResponse, nil, claudeInfo)
	if !ok {
		t.Fatal("expected true")
	}
	if claudeInfo.Usage.PromptTokens != 100 {
		t.Errorf("PromptTokens = %d, want 100", claudeInfo.Usage.PromptTokens)
	}
	if claudeInfo.Usage.PromptTokensDetails.CachedTokens != 30 {
		t.Errorf("CachedTokens = %d, want 30", claudeInfo.Usage.PromptTokensDetails.CachedTokens)
	}
	if claudeInfo.Usage.PromptTokensDetails.CachedCreationTokens != 50 {
		t.Errorf("CachedCreationTokens = %d, want 50", claudeInfo.Usage.PromptTokensDetails.CachedCreationTokens)
	}
	if claudeInfo.ResponseId != "msg_123" {
		t.Errorf("ResponseId = %s, want msg_123", claudeInfo.ResponseId)
	}
	if claudeInfo.Model != "claude-3-5-sonnet" {
		t.Errorf("Model = %s, want claude-3-5-sonnet", claudeInfo.Model)
	}
}

func TestNormalizeClaudeResponseMessageID(t *testing.T) {
	got := normalizeClaudeResponseMessageID("msg_a931edb0-1609-428a-a345-fd61e4113a0c")

	require.True(t, strings.HasPrefix(got, "msg_"))
	require.NotContains(t, got, "-")
	require.NotEqual(t, "msg_a931edb0-1609-428a-a345-fd61e4113a0c", got)
	require.Equal(t, "msg_valid", normalizeClaudeResponseMessageID("msg_valid"))
}

func TestFormatClaudeResponseInfo_MessageDelta_FullUsage(t *testing.T) {
	// message_start 先积累 usage
	claudeInfo := &ClaudeResponseInfo{
		Usage: &dto.Usage{
			PromptTokens: 100,
			PromptTokensDetails: dto.InputTokenDetails{
				CachedTokens:         30,
				CachedCreationTokens: 50,
			},
			CompletionTokens: 1,
		},
	}

	// message_delta 带完整 usage（原生 Anthropic 场景）
	claudeResponse := &dto.ClaudeResponse{
		Type: "message_delta",
		Usage: &dto.ClaudeUsage{
			InputTokens:              100,
			OutputTokens:             200,
			CacheCreationInputTokens: 50,
			CacheReadInputTokens:     30,
		},
	}

	ok := FormatClaudeResponseInfo(claudeResponse, nil, claudeInfo)
	if !ok {
		t.Fatal("expected true")
	}
	if claudeInfo.Usage.PromptTokens != 100 {
		t.Errorf("PromptTokens = %d, want 100", claudeInfo.Usage.PromptTokens)
	}
	if claudeInfo.Usage.CompletionTokens != 200 {
		t.Errorf("CompletionTokens = %d, want 200", claudeInfo.Usage.CompletionTokens)
	}
	if claudeInfo.Usage.TotalTokens != 300 {
		t.Errorf("TotalTokens = %d, want 300", claudeInfo.Usage.TotalTokens)
	}
	if !claudeInfo.Done {
		t.Error("expected Done = true")
	}
}

func TestFormatClaudeResponseInfo_MessageDelta_OnlyOutputTokens(t *testing.T) {
	// 模拟 Bedrock: message_start 已积累 usage
	claudeInfo := &ClaudeResponseInfo{
		Usage: &dto.Usage{
			PromptTokens: 100,
			PromptTokensDetails: dto.InputTokenDetails{
				CachedTokens:         30,
				CachedCreationTokens: 50,
			},
			CompletionTokens:            1,
			ClaudeCacheCreation5mTokens: 10,
			ClaudeCacheCreation1hTokens: 20,
		},
	}

	// Bedrock 的 message_delta 只有 output_tokens，缺少 input_tokens 和 cache 字段
	claudeResponse := &dto.ClaudeResponse{
		Type: "message_delta",
		Usage: &dto.ClaudeUsage{
			OutputTokens: 200,
			// InputTokens, CacheCreationInputTokens, CacheReadInputTokens 都是 0
		},
	}

	ok := FormatClaudeResponseInfo(claudeResponse, nil, claudeInfo)
	if !ok {
		t.Fatal("expected true")
	}
	// PromptTokens 应保持 message_start 的值（因为 message_delta 的 InputTokens=0，不更新）
	if claudeInfo.Usage.PromptTokens != 100 {
		t.Errorf("PromptTokens = %d, want 100", claudeInfo.Usage.PromptTokens)
	}
	if claudeInfo.Usage.CompletionTokens != 200 {
		t.Errorf("CompletionTokens = %d, want 200", claudeInfo.Usage.CompletionTokens)
	}
	if claudeInfo.Usage.TotalTokens != 300 {
		t.Errorf("TotalTokens = %d, want 300", claudeInfo.Usage.TotalTokens)
	}
	// cache 字段应保持 message_start 的值
	if claudeInfo.Usage.PromptTokensDetails.CachedTokens != 30 {
		t.Errorf("CachedTokens = %d, want 30", claudeInfo.Usage.PromptTokensDetails.CachedTokens)
	}
	if claudeInfo.Usage.PromptTokensDetails.CachedCreationTokens != 50 {
		t.Errorf("CachedCreationTokens = %d, want 50", claudeInfo.Usage.PromptTokensDetails.CachedCreationTokens)
	}
	if claudeInfo.Usage.ClaudeCacheCreation5mTokens != 10 {
		t.Errorf("ClaudeCacheCreation5mTokens = %d, want 10", claudeInfo.Usage.ClaudeCacheCreation5mTokens)
	}
	if claudeInfo.Usage.ClaudeCacheCreation1hTokens != 20 {
		t.Errorf("ClaudeCacheCreation1hTokens = %d, want 20", claudeInfo.Usage.ClaudeCacheCreation1hTokens)
	}
	if !claudeInfo.Done {
		t.Error("expected Done = true")
	}
}

func TestFormatClaudeResponseInfo_NilClaudeInfo(t *testing.T) {
	claudeResponse := &dto.ClaudeResponse{Type: "message_start"}
	ok := FormatClaudeResponseInfo(claudeResponse, nil, nil)
	if ok {
		t.Error("expected false for nil claudeInfo")
	}
}

func TestFormatClaudeResponseInfo_ContentBlockDelta(t *testing.T) {
	text := "hello"
	claudeInfo := &ClaudeResponseInfo{
		Usage:        &dto.Usage{},
		ResponseText: strings.Builder{},
	}
	claudeResponse := &dto.ClaudeResponse{
		Type: "content_block_delta",
		Delta: &dto.ClaudeMediaMessage{
			Text: &text,
		},
	}

	ok := FormatClaudeResponseInfo(claudeResponse, nil, claudeInfo)
	if !ok {
		t.Fatal("expected true")
	}
	if claudeInfo.ResponseText.String() != "hello" {
		t.Errorf("ResponseText = %q, want %q", claudeInfo.ResponseText.String(), "hello")
	}
}

func TestBuildOpenAIStyleUsageFromClaudeUsage(t *testing.T) {
	usage := &dto.Usage{
		PromptTokens:     100,
		CompletionTokens: 20,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens:         30,
			CachedCreationTokens: 50,
		},
		ClaudeCacheCreation5mTokens: 10,
		ClaudeCacheCreation1hTokens: 20,
		UsageSemantic:               "anthropic",
	}

	openAIUsage := buildOpenAIStyleUsageFromClaudeUsage(usage)

	if openAIUsage.PromptTokens != 180 {
		t.Fatalf("PromptTokens = %d, want 180", openAIUsage.PromptTokens)
	}
	if openAIUsage.InputTokens != 180 {
		t.Fatalf("InputTokens = %d, want 180", openAIUsage.InputTokens)
	}
	if openAIUsage.TotalTokens != 200 {
		t.Fatalf("TotalTokens = %d, want 200", openAIUsage.TotalTokens)
	}
	if openAIUsage.UsageSemantic != "openai" {
		t.Fatalf("UsageSemantic = %s, want openai", openAIUsage.UsageSemantic)
	}
	if openAIUsage.UsageSource != "anthropic" {
		t.Fatalf("UsageSource = %s, want anthropic", openAIUsage.UsageSource)
	}
}

func TestBuildOpenAIStyleUsageFromClaudeUsagePreservesCacheCreationRemainder(t *testing.T) {
	tests := []struct {
		name                    string
		cachedCreationTokens    int
		cacheCreationTokens5m   int
		cacheCreationTokens1h   int
		expectedTotalInputToken int
	}{
		{
			name:                    "prefers aggregate when it includes remainder",
			cachedCreationTokens:    50,
			cacheCreationTokens5m:   10,
			cacheCreationTokens1h:   20,
			expectedTotalInputToken: 180,
		},
		{
			name:                    "falls back to split tokens when aggregate missing",
			cachedCreationTokens:    0,
			cacheCreationTokens5m:   10,
			cacheCreationTokens1h:   20,
			expectedTotalInputToken: 160,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usage := &dto.Usage{
				PromptTokens:     100,
				CompletionTokens: 20,
				PromptTokensDetails: dto.InputTokenDetails{
					CachedTokens:         30,
					CachedCreationTokens: tt.cachedCreationTokens,
				},
				ClaudeCacheCreation5mTokens: tt.cacheCreationTokens5m,
				ClaudeCacheCreation1hTokens: tt.cacheCreationTokens1h,
				UsageSemantic:               "anthropic",
			}

			openAIUsage := buildOpenAIStyleUsageFromClaudeUsage(usage)

			if openAIUsage.PromptTokens != tt.expectedTotalInputToken {
				t.Fatalf("PromptTokens = %d, want %d", openAIUsage.PromptTokens, tt.expectedTotalInputToken)
			}
			if openAIUsage.InputTokens != tt.expectedTotalInputToken {
				t.Fatalf("InputTokens = %d, want %d", openAIUsage.InputTokens, tt.expectedTotalInputToken)
			}
		})
	}
}

func TestBuildOpenAIStyleUsageFromClaudeUsageDefaultsAggregateCacheCreationTo5m(t *testing.T) {
	usage := &dto.Usage{
		PromptTokens:     100,
		CompletionTokens: 20,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens:         30,
			CachedCreationTokens: 50,
		},
		UsageSemantic: "anthropic",
	}

	openAIUsage := buildOpenAIStyleUsageFromClaudeUsage(usage)

	require.Equal(t, 50, openAIUsage.ClaudeCacheCreation5mTokens)
	require.Equal(t, 0, openAIUsage.ClaudeCacheCreation1hTokens)
}

func TestShouldSkipDuplicateNativeTextDeltaForStructuredOutput(t *testing.T) {
	responseText := `{"answer":"ok"}`
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatClaude,
		Request: &dto.ClaudeRequest{
			OutputFormat: []byte(`{"type":"json_schema","schema":{"type":"object"}}`),
		},
	}
	claudeInfo := &ClaudeResponseInfo{}
	claudeInfo.ResponseText.WriteString(responseText)
	claudeResponse := &dto.ClaudeResponse{
		Type: "content_block_delta",
		Delta: &dto.ClaudeMediaMessage{
			Text: common.GetPointer[string](responseText),
		},
	}

	require.True(t, shouldSkipDuplicateNativeTextDelta(info, claudeInfo, claudeResponse))

	info.Request = &dto.ClaudeRequest{}
	require.False(t, shouldSkipDuplicateNativeTextDelta(info, claudeInfo, claudeResponse))
}

func TestShouldSkipDuplicateNativeTextDeltaForCursorProxyAdjacentDuplicate(t *testing.T) {
	index := 0
	text := "ok"
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatClaude,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: "https://cpa.example.com",
		},
		Request: &dto.ClaudeRequest{},
	}
	claudeInfo := &ClaudeResponseInfo{
		LastNativeDeltaText:  text,
		LastNativeDeltaBlock: index,
	}
	claudeResponse := &dto.ClaudeResponse{
		Type:  "content_block_delta",
		Index: &index,
		Delta: &dto.ClaudeMediaMessage{
			Text: common.GetPointer(text),
		},
	}

	require.True(t, shouldSkipDuplicateNativeTextDelta(info, claudeInfo, claudeResponse))

	info.ChannelMeta.ChannelBaseUrl = "https://api.anthropic.com"
	require.False(t, shouldSkipDuplicateNativeTextDelta(info, claudeInfo, claudeResponse))
}

func TestApplyNativeClaudeDisplayModelUsesRequestedMappedModel(t *testing.T) {
	info := &relaycommon.RelayInfo{
		OriginModelName: "claude-opus-4-8",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "claude-opus-4-8-medium",
			IsModelMapped:     true,
		},
	}
	claudeResponse := &dto.ClaudeResponse{
		Type: "message_start",
		Message: &dto.ClaudeMediaMessage{
			Model: "claude-opus-4-8-medium",
		},
	}

	applyNativeClaudeDisplayModel(info, claudeResponse)
	patchedData := patchNativeClaudeModelData(
		`{"type":"message_start","message":{"model":"claude-opus-4-8-medium"}}`,
		claudeResponse,
	)

	require.Equal(t, "claude-opus-4-8", claudeResponse.Message.Model)
	require.Equal(t, "claude-opus-4-8", gjson.Get(patchedData, "message.model").String())
}

func TestApplyNativeClaudeDisplayModelLeavesUnmappedModel(t *testing.T) {
	info := &relaycommon.RelayInfo{
		OriginModelName: "claude-opus-4-8",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "claude-opus-4-8",
			IsModelMapped:     false,
		},
	}
	claudeResponse := &dto.ClaudeResponse{
		Type:  "message",
		Model: "claude-opus-4-8",
	}

	applyNativeClaudeDisplayModel(info, claudeResponse)

	require.Equal(t, "claude-opus-4-8", claudeResponse.Model)
}

func TestApplyNativeClaudeToolFallbackConvertsTextDeltaToToolDelta(t *testing.T) {
	text := `{"name":"test"}`
	info := &relaycommon.RelayInfo{
		ClaudeConvertInfo: &relaycommon.ClaudeConvertInfo{
			NativeToolFallbackName: "emit_result",
			NativeToolFallbackId:   "toolu_test",
		},
	}
	startResponse := &dto.ClaudeResponse{
		Type: "content_block_start",
		ContentBlock: &dto.ClaudeMediaMessage{
			Type: "text",
			Text: common.GetPointer(""),
		},
	}
	deltaResponse := &dto.ClaudeResponse{
		Type: "content_block_delta",
		Delta: &dto.ClaudeMediaMessage{
			Type: "text_delta",
			Text: common.GetPointer(text),
		},
	}
	messageDeltaResponse := &dto.ClaudeResponse{
		Type:  "message_delta",
		Delta: &dto.ClaudeMediaMessage{},
	}

	require.True(t, applyNativeClaudeToolFallback(info, startResponse))
	require.Equal(t, "tool_use", startResponse.ContentBlock.Type)
	require.Equal(t, "toolu_test", startResponse.ContentBlock.Id)
	require.Equal(t, "emit_result", startResponse.ContentBlock.Name)

	require.True(t, applyNativeClaudeToolFallback(info, deltaResponse))
	require.Equal(t, "input_json_delta", deltaResponse.Delta.Type)
	require.NotNil(t, deltaResponse.Delta.PartialJson)
	require.Equal(t, text, *deltaResponse.Delta.PartialJson)
	require.Nil(t, deltaResponse.Delta.Text)

	require.True(t, applyNativeClaudeToolFallback(info, messageDeltaResponse))
	require.NotNil(t, messageDeltaResponse.Delta.StopReason)
	require.Equal(t, "tool_use", *messageDeltaResponse.Delta.StopReason)
}

func TestRequestOpenAI2ClaudeMessage_ClaudeOpus48HighUsesAdaptiveThinking(t *testing.T) {
	request := dto.GeneralOpenAIRequest{
		Model:       "claude-opus-4-8-high",
		Temperature: commonPointer(0.7),
		TopP:        commonPointer(0.9),
		TopK:        commonPointer(40),
		Messages: []dto.Message{
			{
				Role:    "user",
				Content: "hello",
			},
		},
	}

	claudeRequest, err := RequestOpenAI2ClaudeMessage(nil, request)
	require.NoError(t, err)
	require.Equal(t, "claude-opus-4-8", claudeRequest.Model)
	require.NotNil(t, claudeRequest.Thinking)
	require.Equal(t, "adaptive", claudeRequest.Thinking.Type)
	require.Equal(t, "summarized", claudeRequest.Thinking.Display)
	require.JSONEq(t, `{"effort":"high"}`, string(claudeRequest.OutputConfig))
	require.Nil(t, claudeRequest.Temperature)
	require.Nil(t, claudeRequest.TopP)
	require.Nil(t, claudeRequest.TopK)
}

func TestRequestOpenAI2ClaudeMessage_NewClaudeEffortSuffixUsesAdaptiveThinking(t *testing.T) {
	tests := []struct {
		name      string
		model     string
		wantModel string
	}{
		{name: "fable", model: "claude-fable-5-high", wantModel: "claude-fable-5"},
		{name: "new order", model: "claude-5-sonnet-medium", wantModel: "claude-5-sonnet"},
		{name: "dotted cursor order", model: "claude-4.6-sonnet-medium", wantModel: "claude-4.6-sonnet"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := dto.GeneralOpenAIRequest{
				Model:       test.model,
				Temperature: commonPointer(0.7),
				TopP:        commonPointer(0.9),
				TopK:        commonPointer(40),
				Messages: []dto.Message{
					{
						Role:    "user",
						Content: "hello",
					},
				},
			}

			claudeRequest, err := RequestOpenAI2ClaudeMessage(nil, request)

			require.NoError(t, err)
			require.Equal(t, test.wantModel, claudeRequest.Model)
			require.NotNil(t, claudeRequest.Thinking)
			require.Equal(t, "adaptive", claudeRequest.Thinking.Type)
			require.Equal(t, "summarized", claudeRequest.Thinking.Display)
			require.Nil(t, claudeRequest.Temperature)
			require.Nil(t, claudeRequest.TopP)
			require.Nil(t, claudeRequest.TopK)
		})
	}
}

func TestRequestOpenAI2ClaudeMessageSupportsPDFFilenameAlias(t *testing.T) {
	pdfData := base64.StdEncoding.EncodeToString([]byte("%PDF-1.4\nBT /F1 24 Tf 72 720 Td (MAGIC_PDF_WORD: BANANA123) Tj ET"))
	request := dto.GeneralOpenAIRequest{
		Model: "claude-opus-5",
		Messages: []dto.Message{
			{
				Role: "user",
				Content: []any{
					map[string]any{
						"type": "text",
						"text": "read pdf",
					},
					map[string]any{
						"type": "file",
						"file": map[string]any{
							"filename":  "sample.pdf",
							"file_data": pdfData,
						},
					},
				},
			},
		},
	}

	claudeRequest, err := RequestOpenAI2ClaudeMessage(nil, request)

	require.NoError(t, err)
	require.Len(t, claudeRequest.Messages, 1)
	content, ok := claudeRequest.Messages[0].Content.([]dto.ClaudeMediaMessage)
	require.True(t, ok)
	require.Len(t, content, 3)
	require.Equal(t, "document", content[1].Type)
	require.NotNil(t, content[1].Source)
	require.Equal(t, "application/pdf", content[1].Source.MediaType)
	require.Equal(t, pdfData, content[1].Source.Data)
	require.Equal(t, "text", content[2].Type)
	require.NotNil(t, content[2].Text)
	require.Contains(t, *content[2].Text, "MAGIC_PDF_WORD: BANANA123")
}

func TestExtractSimplePDFTextDecodesEscapedLiteralStrings(t *testing.T) {
	pdfData := base64.StdEncoding.EncodeToString([]byte("%PDF-1.4\nBT (hello\\040world \\(ok\\)) Tj ET"))

	text := service.ExtractSimplePDFText(pdfData)

	require.Contains(t, text, "hello world (ok)")
}

func TestRequestOpenAI2ClaudeMessageAddsJSONSchemaInstruction(t *testing.T) {
	request := dto.GeneralOpenAIRequest{
		Model: "claude-opus-5",
		ResponseFormat: &dto.ResponseFormat{
			Type:       "json_schema",
			JsonSchema: []byte(`{"name":"answer","schema":{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}}`),
		},
		Messages: []dto.Message{
			{
				Role:    "user",
				Content: "answer",
			},
		},
	}

	claudeRequest, err := RequestOpenAI2ClaudeMessage(nil, request)

	require.NoError(t, err)
	system, ok := claudeRequest.System.([]dto.ClaudeMediaMessage)
	require.True(t, ok)
	require.Len(t, system, 1)
	require.NotNil(t, system[0].Text)
	require.Contains(t, *system[0].Text, "valid JSON object only")
	require.Contains(t, *system[0].Text, `"required":["answer"]`)
	require.NotContains(t, *system[0].Text, `"name":"answer"`)
}

func TestRequestOpenAI2ClaudeMessage_ClaudeOpus48ThinkingUsesAdaptiveHighEffort(t *testing.T) {
	request := dto.GeneralOpenAIRequest{
		Model:       "claude-opus-4-8-thinking",
		Temperature: commonPointer(0.7),
		TopP:        commonPointer(0.9),
		TopK:        commonPointer(40),
		Messages: []dto.Message{
			{
				Role:    "user",
				Content: "hello",
			},
		},
	}

	claudeRequest, err := RequestOpenAI2ClaudeMessage(nil, request)
	require.NoError(t, err)
	require.Equal(t, "claude-opus-4-8", claudeRequest.Model)
	require.NotNil(t, claudeRequest.Thinking)
	require.Equal(t, "adaptive", claudeRequest.Thinking.Type)
	require.Equal(t, "summarized", claudeRequest.Thinking.Display)
	require.JSONEq(t, `{"effort":"high"}`, string(claudeRequest.OutputConfig))
	require.Nil(t, claudeRequest.Temperature)
	require.Nil(t, claudeRequest.TopP)
	require.Nil(t, claudeRequest.TopK)
}

func TestRequestOpenAI2ClaudeMessage_NewClaudeThinkingAliasUsesAdaptiveHighEffort(t *testing.T) {
	request := dto.GeneralOpenAIRequest{
		Model:       "claude-sonnet-5-thinking",
		Temperature: commonPointer(0.7),
		TopP:        commonPointer(0.9),
		TopK:        commonPointer(40),
		Messages: []dto.Message{
			{
				Role:    "user",
				Content: "hello",
			},
		},
	}

	claudeRequest, err := RequestOpenAI2ClaudeMessage(nil, request)

	require.NoError(t, err)
	require.Equal(t, "claude-sonnet-5", claudeRequest.Model)
	require.NotNil(t, claudeRequest.Thinking)
	require.Equal(t, "adaptive", claudeRequest.Thinking.Type)
	require.Equal(t, "summarized", claudeRequest.Thinking.Display)
	require.JSONEq(t, `{"effort":"high"}`, string(claudeRequest.OutputConfig))
	require.Nil(t, claudeRequest.Temperature)
	require.Nil(t, claudeRequest.TopP)
	require.Nil(t, claudeRequest.TopK)
}
