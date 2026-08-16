package relay

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/reasoning"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func ClaudeHelper(c *gin.Context, info *relaycommon.RelayInfo) (newAPIError *types.NewAPIError) {

	info.InitChannelMeta(c)

	claudeReq, ok := info.Request.(*dto.ClaudeRequest)

	if !ok {
		return types.NewErrorWithStatusCode(fmt.Errorf("invalid request type, expected *dto.ClaudeRequest, got %T", info.Request), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	request, err := common.DeepCopy(claudeReq)
	if err != nil {
		return types.NewError(fmt.Errorf("failed to copy request to ClaudeRequest: %w", err), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}

	err = helper.ModelMappedHelper(c, info, request)
	if err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}

	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		return types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)

	if request.MaxTokens == nil || *request.MaxTokens == 0 {
		defaultMaxTokens := uint(model_setting.GetClaudeSettings().GetDefaultMaxTokens(request.Model))
		request.MaxTokens = &defaultMaxTokens
	}
	applyCursorProxyNativeToolFallback(info, request)
	normalizeNativeClaudeRequest(request)

	preserveMappedEffortVariant := shouldPreserveMappedEffortVariant(info)
	if baseModel, effortLevel, ok := reasoning.TrimEffortSuffix(request.Model); ok && effortLevel != "" &&
		reasoning.SupportsClaudeAdaptiveEffortSuffix(request.Model) &&
		!preserveMappedEffortVariant {
		request.Model = baseModel
		request.Thinking = &dto.Thinking{
			Type: "adaptive",
		}
		request.OutputConfig = json.RawMessage(fmt.Sprintf(`{"effort":"%s"}`, effortLevel))
		if !reasoning.ShouldUseClaudeLegacyAdaptiveSampling(request.Model) {
			request.Thinking.Display = "summarized"
			request.Temperature = nil
			request.TopP = nil
			request.TopK = nil
		} else {
			request.Temperature = common.GetPointer[float64](1.0)
		}
		info.UpstreamModelName = request.Model
	} else if model_setting.GetClaudeSettings().ThinkingAdapterEnabled &&
		strings.HasSuffix(request.Model, "-thinking") {
		if request.Thinking == nil {
			baseModel := strings.TrimSuffix(request.Model, "-thinking")
			if reasoning.SupportsClaudeAdaptiveThinkingAlias(baseModel) && !reasoning.ShouldUseClaudeLegacyAdaptiveSampling(baseModel) {
				request.Thinking = &dto.Thinking{Type: "adaptive", Display: "summarized"}
				request.OutputConfig = json.RawMessage(`{"effort":"high"}`)
				request.Temperature = nil
				request.TopP = nil
				request.TopK = nil
			} else {
				// 因为BudgetTokens 必须大于1024
				if request.MaxTokens == nil || *request.MaxTokens < 1280 {
					request.MaxTokens = common.GetPointer[uint](1280)
				}

				// BudgetTokens 为 max_tokens 的 80%
				request.Thinking = &dto.Thinking{
					Type:         "enabled",
					BudgetTokens: common.GetPointer[int](int(float64(*request.MaxTokens) * model_setting.GetClaudeSettings().ThinkingAdapterBudgetTokensPercentage)),
				}
				// TODO: 临时处理
				// https://docs.anthropic.com/en/docs/build-with-claude/extended-thinking#important-considerations-when-using-extended-thinking
				request.Temperature = common.GetPointer[float64](1.0)
			}
		}
		if !model_setting.ShouldPreserveThinkingSuffix(info.OriginModelName) {
			request.Model = strings.TrimSuffix(request.Model, "-thinking")
		}
		info.UpstreamModelName = request.Model
	}

	if info.ChannelSetting.SystemPrompt != "" {
		if request.System == nil {
			request.SetStringSystem(info.ChannelSetting.SystemPrompt)
		} else if info.ChannelSetting.SystemPromptOverride {
			common.SetContextKey(c, constant.ContextKeySystemPromptOverride, true)
			if request.IsStringSystem() {
				existing := strings.TrimSpace(request.GetStringSystem())
				if existing == "" {
					request.SetStringSystem(info.ChannelSetting.SystemPrompt)
				} else {
					request.SetStringSystem(info.ChannelSetting.SystemPrompt + "\n" + existing)
				}
			} else {
				systemContents := request.ParseSystem()
				newSystem := dto.ClaudeMediaMessage{Type: dto.ContentTypeText}
				newSystem.SetText(info.ChannelSetting.SystemPrompt)
				if len(systemContents) == 0 {
					request.System = []dto.ClaudeMediaMessage{newSystem}
				} else {
					request.System = append([]dto.ClaudeMediaMessage{newSystem}, systemContents...)
				}
			}
		}
	}

	if !model_setting.GetGlobalSettings().PassThroughRequestEnabled &&
		!info.ChannelSetting.PassThroughBodyEnabled &&
		service.ShouldChatCompletionsUseResponsesGlobal(info.ChannelId, info.ChannelType, info.OriginModelName) {
		openAIRequest, convErr := service.ClaudeToOpenAIRequest(*request, info)
		if convErr != nil {
			return types.NewError(convErr, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}

		usage, newApiErr := chatCompletionsViaResponses(c, info, adaptor, openAIRequest)
		if newApiErr != nil {
			return newApiErr
		}

		service.PostTextConsumeQuota(c, info, usage, nil)
		return nil
	}

	var requestBody io.Reader
	if model_setting.GetGlobalSettings().PassThroughRequestEnabled || info.ChannelSetting.PassThroughBodyEnabled {
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		info.UpstreamRequestBodySize = storage.Size()
		requestBody = common.ReaderOnly(storage)
	} else {
		convertedRequest, err := adaptor.ConvertClaudeRequest(c, info, request)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		relaycommon.AppendRequestConversionFromRequest(info, convertedRequest)
		jsonData, err := common.Marshal(convertedRequest)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}

		// remove disabled fields for Claude API
		jsonData, err = relaycommon.RemoveDisabledFields(jsonData, info.ChannelOtherSettings, info.ChannelSetting.PassThroughBodyEnabled)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}

		// apply param override
		if len(info.ParamOverride) > 0 {
			jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
			if err != nil {
				return newAPIErrorFromParamOverride(err)
			}
		}

		logger.LogDebug(c, "requestBody: %s", jsonData)
		body, size, closer, err := relaycommon.NewOutboundJSONBody(jsonData)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		defer closer.Close()
		jsonData = nil
		info.UpstreamRequestBodySize = size
		requestBody = body
	}

	statusCodeMappingStr := c.GetString("status_code_mapping")
	var httpResp *http.Response
	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}

	if resp != nil {
		httpResp = resp.(*http.Response)
		info.IsStream = info.IsStream || strings.HasPrefix(httpResp.Header.Get("Content-Type"), "text/event-stream")
		if httpResp.StatusCode != http.StatusOK {
			newAPIError = service.RelayErrorHandler(c.Request.Context(), httpResp, false)
			// reset status code 重置状态码
			service.ResetStatusCode(newAPIError, statusCodeMappingStr)
			return newAPIError
		}
	}

	usage, newAPIError := adaptor.DoResponse(c, httpResp, info)
	if newAPIError != nil {
		// reset status code 重置状态码
		service.ResetStatusCode(newAPIError, statusCodeMappingStr)
		return newAPIError
	}

	service.PostTextConsumeQuota(c, info, usage.(*dto.Usage), nil)
	return nil
}

func shouldPreserveMappedEffortVariant(info *relaycommon.RelayInfo) bool {
	if info == nil || !info.IsModelMapped || info.ChannelMeta == nil {
		return false
	}
	baseURL := strings.ToLower(strings.TrimSpace(info.ChannelMeta.ChannelBaseUrl))
	return strings.Contains(baseURL, "cpa.muxpay.xyz") || strings.Contains(baseURL, "cli-proxy-api")
}

func applyCursorProxyNativeToolFallback(info *relaycommon.RelayInfo, request *dto.ClaudeRequest) bool {
	if info == nil || request == nil {
		return false
	}
	if request.ToolChoice != nil && !isCursorProxyClaudeChannel(info) {
		return false
	}
	if !isCursorProxyClaudeChannel(info) {
		return false
	}
	toolName, ok := forcedClaudeToolName(request.ToolChoice)
	if !ok {
		return false
	}
	tool, ok := findClaudeTool(request.Tools, toolName)
	if !ok || len(tool.InputSchema) == 0 {
		return false
	}
	outputFormat, err := common.Marshal(map[string]any{
		"type":   "json_schema",
		"schema": tool.InputSchema,
	})
	if err != nil {
		return false
	}
	request.OutputFormat = outputFormat
	request.Tools = nil
	request.ToolChoice = nil
	sanitizeCursorProxyNativeToolPrompt(request, tool.Name)
	if info.ClaudeConvertInfo == nil {
		info.ClaudeConvertInfo = &relaycommon.ClaudeConvertInfo{}
	}
	info.NativeToolFallbackName = tool.Name
	info.NativeToolFallbackId = "toolu_" + common.GetRandomString(24)
	return true
}

func sanitizeCursorProxyNativeToolPrompt(request *dto.ClaudeRequest, toolName string) {
	if request == nil || strings.TrimSpace(toolName) == "" {
		return
	}
	for msgIndex := range request.Messages {
		if request.Messages[msgIndex].Role != "user" {
			continue
		}
		switch content := request.Messages[msgIndex].Content.(type) {
		case string:
			request.Messages[msgIndex].Content = sanitizeCursorProxyNativeToolText(content, toolName)
		case []any:
			for itemIndex := range content {
				item, ok := content[itemIndex].(map[string]any)
				if !ok || item["type"] != dto.ContentTypeText {
					continue
				}
				if text, ok := item["text"].(string); ok {
					item["text"] = sanitizeCursorProxyNativeToolText(text, toolName)
				}
			}
			request.Messages[msgIndex].Content = content
		case []dto.ClaudeMediaMessage:
			for itemIndex := range content {
				if content[itemIndex].Type == dto.ContentTypeText && content[itemIndex].Text != nil {
					sanitized := sanitizeCursorProxyNativeToolText(*content[itemIndex].Text, toolName)
					content[itemIndex].Text = &sanitized
				}
			}
			request.Messages[msgIndex].Content = content
		}
	}
}

func sanitizeCursorProxyNativeToolText(text string, toolName string) string {
	pattern := regexp.MustCompile(`(?i)\b(call|invoke|use)\s+` + regexp.QuoteMeta(toolName) + `\s*(with|using)?`)
	sanitized := pattern.ReplaceAllString(text, "Return JSON object with")
	if sanitized == text {
		return text
	}
	return sanitized
}

func isCursorProxyClaudeChannel(info *relaycommon.RelayInfo) bool {
	if info == nil || info.ChannelMeta == nil {
		return false
	}
	baseURL := strings.ToLower(strings.TrimSpace(info.ChannelMeta.ChannelBaseUrl))
	return strings.Contains(baseURL, "cpa") ||
		strings.Contains(baseURL, "cli-proxy-api") ||
		strings.Contains(baseURL, "cursor")
}

func forcedClaudeToolName(toolChoice any) (string, bool) {
	switch choice := toolChoice.(type) {
	case dto.ClaudeToolChoice:
		return strings.TrimSpace(choice.Name), choice.Type == "tool" && strings.TrimSpace(choice.Name) != ""
	case *dto.ClaudeToolChoice:
		if choice == nil {
			return "", false
		}
		return strings.TrimSpace(choice.Name), choice.Type == "tool" && strings.TrimSpace(choice.Name) != ""
	case map[string]any:
		choiceType, _ := choice["type"].(string)
		name, _ := choice["name"].(string)
		return strings.TrimSpace(name), choiceType == "tool" && strings.TrimSpace(name) != ""
	default:
		return "", false
	}
}

func findClaudeTool(rawTools any, name string) (*dto.Tool, bool) {
	trimmedName := strings.TrimSpace(name)
	if rawTools == nil || trimmedName == "" {
		return nil, false
	}
	var tools []dto.Tool
	data, err := common.Marshal(rawTools)
	if err != nil {
		return nil, false
	}
	if err := common.Unmarshal(data, &tools); err != nil {
		return nil, false
	}
	for i := range tools {
		if tools[i].Name == trimmedName {
			return &tools[i], true
		}
	}
	return nil, false
}

func normalizeNativeClaudeRequest(request *dto.ClaudeRequest) {
	if request == nil {
		return
	}
	if instruction := buildClaudeNativeOutputFormatInstruction(request.OutputFormat); instruction != "" {
		prependClaudeSystemText(request, instruction)
	}
	for msgIndex := range request.Messages {
		if request.Messages[msgIndex].IsStringContent() {
			continue
		}
		contents, err := request.Messages[msgIndex].ParseContent()
		if err != nil || len(contents) == 0 {
			continue
		}
		expanded := make([]dto.ClaudeMediaMessage, 0, len(contents)+1)
		for _, content := range contents {
			expanded = append(expanded, content)
			if content.Type != "document" || content.Source == nil {
				continue
			}
			if !strings.HasPrefix(content.Source.MediaType, "application/pdf") {
				continue
			}
			base64Data := common.Interface2String(content.Source.Data)
			if base64Data == "" {
				continue
			}
			extractedText := service.ExtractSimplePDFText(base64Data)
			if extractedText == "" {
				continue
			}
			expanded = append(expanded, dto.ClaudeMediaMessage{
				Type: "text",
				Text: common.GetPointer[string]("Extracted PDF text:\n" + extractedText),
			})
		}
		request.Messages[msgIndex].Content = expanded
	}
}

func prependClaudeSystemText(request *dto.ClaudeRequest, text string) {
	newSystem := dto.ClaudeMediaMessage{Type: dto.ContentTypeText}
	newSystem.SetText(text)
	if request.System == nil {
		request.System = []dto.ClaudeMediaMessage{newSystem}
		return
	}
	if request.IsStringSystem() {
		existing := strings.TrimSpace(request.GetStringSystem())
		if existing == "" {
			request.System = []dto.ClaudeMediaMessage{newSystem}
		} else {
			existingSystem := dto.ClaudeMediaMessage{Type: dto.ContentTypeText}
			existingSystem.SetText(existing)
			request.System = []dto.ClaudeMediaMessage{newSystem, existingSystem}
		}
		return
	}
	systemContents := request.ParseSystem()
	request.System = append([]dto.ClaudeMediaMessage{newSystem}, systemContents...)
}

func buildClaudeNativeOutputFormatInstruction(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var outputFormat map[string]any
	if err := common.Unmarshal(raw, &outputFormat); err != nil {
		return ""
	}
	formatType, _ := outputFormat["type"].(string)
	if formatType != "json_schema" && formatType != "json_object" {
		if _, ok := outputFormat["schema"]; !ok {
			return ""
		}
	}
	instruction := "You must respond with a valid JSON object only. Do not include markdown, code fences, or explanatory text."
	if schema, ok := outputFormat["schema"]; ok {
		if schemaBytes, err := common.Marshal(schema); err == nil {
			instruction += " The JSON object must conform to this JSON Schema: " + string(schemaBytes)
		}
	}
	return instruction
}
