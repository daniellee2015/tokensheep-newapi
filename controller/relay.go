package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func relayHandler(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	var err *types.NewAPIError
	switch info.RelayMode {
	case relayconstant.RelayModeImagesGenerations, relayconstant.RelayModeImagesEdits:
		err = relay.ImageHelper(c, info)
	case relayconstant.RelayModeAudioSpeech:
		fallthrough
	case relayconstant.RelayModeAudioTranslation:
		fallthrough
	case relayconstant.RelayModeAudioTranscription:
		err = relay.AudioHelper(c, info)
	case relayconstant.RelayModeRerank:
		err = relay.RerankHelper(c, info)
	case relayconstant.RelayModeEmbeddings:
		err = relay.EmbeddingHelper(c, info)
	case relayconstant.RelayModeResponses, relayconstant.RelayModeResponsesCompact:
		err = relay.ResponsesHelper(c, info)
	case relayconstant.RelayModeAlphaSearch:
		err = relay.AlphaSearchHelper(c, info)
	default:
		err = relay.TextHelper(c, info)
	}
	return err
}

func geminiRelayHandler(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	var err *types.NewAPIError
	if strings.Contains(c.Request.URL.Path, "embed") {
		err = relay.GeminiEmbeddingHandler(c, info)
	} else {
		err = relay.GeminiHelper(c, info)
	}
	return err
}

func Relay(c *gin.Context, relayFormat types.RelayFormat) {

	requestId := c.GetString(common.RequestIdKey)
	//group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	//originalModel := common.GetContextKeyString(c, constant.ContextKeyOriginalModel)

	var (
		newAPIError *types.NewAPIError
		ws          *websocket.Conn
	)

	if relayFormat == types.RelayFormatOpenAIRealtime {
		var err error
		ws, err = upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			helper.WssError(c, ws, types.NewError(err, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry()).ToOpenAIError())
			return
		}
		defer ws.Close()
	}

	defer func() {
		if newAPIError != nil {
			logger.LogError(c, fmt.Sprintf("relay error: %s", common.LocalLogPreview(newAPIError.Error())))
			message := common.StripNestedRequestIDs(newAPIError.Error())
			newAPIError.SetMessage(common.MessageWithRequestId(message, requestId))
			writeRelayError(c, ws, relayFormat, newAPIError)
		}
	}()

	request, err := helper.GetAndValidateRequest(c, relayFormat)
	if err != nil {
		// Map "request body too large" to 413 so clients can handle it correctly
		if common.IsRequestBodyTooLargeError(err) || errors.Is(err, common.ErrRequestBodyTooLarge) {
			newAPIError = types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
		} else {
			newAPIError = types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		return
	}

	relayInfo, err := relaycommon.GenRelayInfo(c, relayFormat, request, ws)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeGenRelayInfoFailed)
		return
	}

	needSensitiveCheck := setting.ShouldCheckPromptSensitive()
	needCountToken := constant.CountToken
	// Avoid building huge CombineText (strings.Join) when token counting and sensitive check are both disabled.
	var meta *types.TokenCountMeta
	if needSensitiveCheck || needCountToken {
		meta = request.GetTokenCountMeta()
	} else {
		meta = fastTokenCountMetaForPricing(request)
	}

	if needSensitiveCheck {
		// Scan only what the user typed in this request. Feeding CombineText
		// here (system prompt + full history + tool_result + tool defs) causes
		// permanent session poisoning as soon as the transcript contains a
		// keyword, which is trivially easy — the sensitive-word list itself
		// lives in setting/sensitive.go. See
		// service/risk_scan_text.go for the full rationale.
		riskScanText := service.ExtractRiskScanText(request)
		if contains, words := service.CheckSensitiveText(riskScanText); contains {
			joined := strings.Join(words, ", ")
			logger.LogWarn(c, fmt.Sprintf("user sensitive words detected: %s", joined))
			// Do NOT pass `err` here — GenRelayInfo above returned nil error
			// and NewErrorWithStatusCode would previously panic on
			// err.Error() when err is nil. We surface the matched words as
			// the message so it stays actionable in logs/UI.
			newAPIError = types.NewErrorWithStatusCode(
				fmt.Errorf("sensitive words detected: %s", joined),
				types.ErrorCodeSensitiveWordsDetected,
				http.StatusBadRequest,
				types.ErrOptionWithSkipRetry(),
			)
			return
		}
	}

	tokens, err := service.EstimateRequestToken(c, meta, relayInfo)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeCountTokenFailed)
		return
	}

	relayInfo.SetEstimatePromptTokens(tokens)

	priceData, err := helper.ModelPriceHelper(c, relayInfo, tokens, meta)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithStatusCode(http.StatusBadRequest))
		return
	}

	// common.SetContextKey(c, constant.ContextKeyTokenCountMeta, meta)

	if priceData.FreeModel {
		logger.LogInfo(c, fmt.Sprintf("模型 %s 免费，跳过预扣费", relayInfo.OriginModelName))
	} else {
		newAPIError = service.PreConsumeBilling(c, priceData.QuotaToPreConsume, relayInfo)
		if newAPIError != nil {
			return
		}
	}

	defer func() {
		// Only return quota if downstream failed and quota was actually pre-consumed
		if newAPIError != nil {
			newAPIError = service.NormalizeViolationFeeError(newAPIError)
			if relayInfo.Billing != nil {
				relayInfo.Billing.Refund(c)
			}
			feeError := newAPIError
			if relayInfo.LastError != nil {
				feeError = relayInfo.LastError
			}
			service.ChargeViolationFeeIfNeeded(c, relayInfo, feeError)
		}
	}()

	retryParam := &service.RetryParam{
		Ctx:         c,
		TokenGroup:  relayInfo.TokenGroup,
		ModelName:   relayInfo.OriginModelName,
		RequestPath: c.Request.URL.Path,
		Retry:       common.GetPointer(0),
	}
	var lastChannelError *types.ChannelError
	var lastRawChannelError *types.NewAPIError
	relayInfo.RetryIndex = 0
	relayInfo.LastError = nil

	for ; retryParam.GetRetry() <= common.RetryTimes; retryParam.IncreaseRetry() {
		relayInfo.RetryIndex = retryParam.GetRetry()
		channel, channelErr := getChannel(c, relayInfo, retryParam)
		if channelErr != nil {
			logger.LogError(c, channelErr.Error())
			newAPIError = service.PublicUpstreamError(channelErr)
			break
		}
		addUsedChannel(c, channel.Id)
		retryParam.ExcludeChannel(channel.Id)
		if billingErr := service.PrepareTieredBillingForSelectedGroup(c, relayInfo); billingErr != nil {
			newAPIError = billingErr
			break
		}

		bodyStorage, bodyErr := common.GetBodyStorage(c)
		if bodyErr != nil {
			// Ensure consistent 413 for oversized bodies even when error occurs later (e.g., retry path)
			if common.IsRequestBodyTooLargeError(bodyErr) || errors.Is(bodyErr, common.ErrRequestBodyTooLarge) {
				newAPIError = types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
			} else {
				newAPIError = types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
			}
			break
		}
		c.Request.Body = io.NopCloser(bodyStorage)

		channelOtherSettings, _ := common.GetContextKeyType[dto.ChannelOtherSettings](c, constant.ContextKeyChannelOtherSetting)
		releaseChannel, concurrencyErr := middleware.AcquireChannelConcurrency(
			c.Request.Context(),
			channel.Id,
			channelOtherSettings.ConcurrencyLimit,
		)
		if concurrencyErr != nil {
			rawChannelError := types.NewOpenAIError(
				fmt.Errorf("channel #%d concurrency limit %d unavailable: %w", channel.Id, channelOtherSettings.ConcurrencyLimit, concurrencyErr),
				types.ErrorCode("channel_concurrency_limit_exceeded"),
				http.StatusServiceUnavailable,
			)
			lastChannelError = types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey,
				common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan())
			lastRawChannelError = rawChannelError
			newAPIError = service.PublicUpstreamError(rawChannelError)
			logger.LogWarn(c, rawChannelError.Error())
			if c.Request.Context().Err() == nil && retryParam.GetRetry() < common.RetryTimes {
				continue
			}
			break
		}

		func() {
			defer releaseChannel()
			switch relayFormat {
			case types.RelayFormatOpenAIRealtime:
				newAPIError = relay.WssHelper(c, relayInfo)
			case types.RelayFormatClaude:
				newAPIError = relay.ClaudeHelper(c, relayInfo)
			case types.RelayFormatGemini:
				newAPIError = geminiRelayHandler(c, relayInfo)
			default:
				newAPIError = relayHandler(c, relayInfo)
			}
		}()

		if newAPIError == nil {
			relayInfo.LastError = nil
			return
		}

		newAPIError = service.NormalizeViolationFeeError(newAPIError)
		rawChannelError := newAPIError
		relayInfo.LastError = rawChannelError
		channelError := types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey, common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan())
		lastChannelError = channelError
		lastRawChannelError = rawChannelError

		willRetry := !c.Writer.Written() && shouldRetry(c, rawChannelError, common.RetryTimes-retryParam.GetRetry())
		processChannelError(c, *channelError, rawChannelError)
		newAPIError = service.PublicUpstreamError(rawChannelError)

		// A retry after any response bytes have been sent would concatenate two
		// independent streams and leave the client with a malformed HTTP 200.
		if c.Writer.Written() {
			break
		}
		if !willRetry {
			break
		}
		if errWait := waitBeforeChannelFallback(c.Request.Context(), rawChannelError); errWait != nil {
			break
		}
	}

	useChannel := c.GetStringSlice("use_channel")
	if len(useChannel) > 1 {
		retryLogStr := fmt.Sprintf("重试：%s", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(useChannel)), "->"), "[]"))
		logger.LogInfo(c, retryLogStr)
	}
	if newAPIError != nil {
		if lastChannelError != nil && lastRawChannelError != nil {
			recordChannelErrorLog(c, *lastChannelError, lastRawChannelError, newAPIError)
		}
		gopool.Go(func() {
			perfmetrics.RecordRelaySample(relayInfo, false, 0)
		})
	}
}

func writeRelayError(c *gin.Context, ws *websocket.Conn, relayFormat types.RelayFormat, newAPIError *types.NewAPIError) {
	// Channel-level rules also cover errors that never reached
	// processChannelError (local/validation failures). Applying them here is
	// idempotent: a rule that already ran no longer matches its own output.
	service.ApplyChannelErrorMask(c, newAPIError)
	if !c.Writer.Written() {
		for _, header := range []string{
			"Content-Length", "Content-Disposition", "Accept-Ranges", "Content-Range",
			"X-Reasoning-Included", "X-Codex-Turn-State",
		} {
			c.Writer.Header().Del(header)
		}
		c.Writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		if retryAfter := newAPIError.RetryAfter(); retryAfter > 0 {
			seconds := int64(retryAfter / time.Second)
			if retryAfter%time.Second != 0 {
				seconds++
			}
			if seconds > 0 {
				c.Writer.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
			}
		}
	}
	switch relayFormat {
	case types.RelayFormatOpenAIRealtime:
		helper.WssError(c, ws, newAPIError.ToOpenAIError())
	case types.RelayFormatClaude:
		if c.Writer.Written() && strings.HasPrefix(c.Writer.Header().Get("Content-Type"), "text/event-stream") {
			_ = helper.ClaudeData(c, dto.ClaudeResponse{
				Type:  "error",
				Error: newAPIError.ToClaudeError(),
			})
			return
		}
		c.JSON(newAPIError.StatusCode, gin.H{
			"type":  "error",
			"error": newAPIError.ToClaudeError(),
		})
	default:
		if c.Writer.Written() && strings.HasPrefix(c.Writer.Header().Get("Content-Type"), "text/event-stream") {
			if err := helper.ObjectData(c, gin.H{"error": newAPIError.ToOpenAIError()}); err != nil {
				logger.LogError(c, "failed to write relay SSE error: "+err.Error())
			}
			return
		}
		c.JSON(newAPIError.StatusCode, gin.H{
			"error": newAPIError.ToOpenAIError(),
		})
	}
}

var upgrader = websocket.Upgrader{
	Subprotocols: []string{"realtime"}, // WS 握手支持的协议，如果有使用 Sec-WebSocket-Protocol，则必须在此声明对应的 Protocol TODO add other protocol
	CheckOrigin: func(r *http.Request) bool {
		return true // 允许跨域
	},
}

func addUsedChannel(c *gin.Context, channelId int) {
	useChannel := c.GetStringSlice("use_channel")
	useChannel = append(useChannel, fmt.Sprintf("%d", channelId))
	c.Set("use_channel", useChannel)
}

func fastTokenCountMetaForPricing(request dto.Request) *types.TokenCountMeta {
	if request == nil {
		return &types.TokenCountMeta{}
	}
	meta := &types.TokenCountMeta{
		TokenType: types.TokenTypeTokenizer,
	}
	switch r := request.(type) {
	case *dto.GeneralOpenAIRequest:
		maxCompletionTokens := lo.FromPtrOr(r.MaxCompletionTokens, uint(0))
		maxTokens := lo.FromPtrOr(r.MaxTokens, uint(0))
		if maxCompletionTokens > maxTokens {
			meta.MaxTokens = int(maxCompletionTokens)
		} else {
			meta.MaxTokens = int(maxTokens)
		}
	case *dto.OpenAIResponsesRequest:
		meta.MaxTokens = int(lo.FromPtrOr(r.MaxOutputTokens, uint(0)))
	case *dto.ClaudeRequest:
		meta.MaxTokens = int(lo.FromPtr(r.MaxTokens))
	case *dto.ImageRequest:
		// Pricing for image requests depends on ImagePriceRatio; safe to compute even when CountToken is disabled.
		return r.GetTokenCountMeta()
	default:
		// Best-effort: leave CombineText empty to avoid large allocations.
	}
	return meta
}

func getChannel(c *gin.Context, info *relaycommon.RelayInfo, retryParam *service.RetryParam) (*model.Channel, *types.NewAPIError) {
	if info.ChannelMeta == nil {
		autoBan := c.GetBool("auto_ban")
		autoBanInt := 1
		if !autoBan {
			autoBanInt = 0
		}
		return &model.Channel{
			Id:      c.GetInt("channel_id"),
			Type:    c.GetInt("channel_type"),
			Name:    c.GetString("channel_name"),
			AutoBan: &autoBanInt,
		}, nil
	}
	channel, selectGroup, err := service.CacheGetRandomSatisfiedChannel(retryParam)
	if err != nil {
		return nil, types.NewError(fmt.Errorf("获取分组 %s 下模型 %s 的可用渠道失败（retry）: %s", selectGroup, info.OriginModelName, err.Error()), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	if channel == nil {
		return nil, types.NewError(fmt.Errorf("分组 %s 下模型 %s 的可用渠道不存在（retry）", selectGroup, info.OriginModelName), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}

	info.PriceData.GroupRatioInfo = helper.HandleGroupRatio(c, info)

	newAPIError := middleware.SetupContextForSelectedChannel(c, channel, info.OriginModelName)
	if newAPIError != nil {
		return nil, newAPIError
	}
	return channel, nil
}

func shouldRetry(c *gin.Context, openaiErr *types.NewAPIError, retryTimes int) bool {
	if openaiErr == nil {
		return false
	}
	if service.ShouldSkipRetryAfterChannelAffinityFailure(c) {
		return false
	}
	if types.IsChannelError(openaiErr) {
		return true
	}
	if types.IsSkipRetryError(openaiErr) {
		return false
	}
	if retryTimes <= 0 {
		return false
	}
	if _, ok := c.Get("specific_channel_id"); ok {
		return false
	}
	code := openaiErr.StatusCode
	if code >= 200 && code < 300 {
		return false
	}
	if code < 100 || code > 599 {
		return true
	}
	if operation_setting.IsAlwaysSkipRetryCode(openaiErr.GetErrorCode()) {
		return false
	}
	return operation_setting.ShouldRetryByStatusCode(code)
}

const maxChannelFallbackDelay = 5 * time.Second

func channelFallbackDelay(err *types.NewAPIError) time.Duration {
	if err == nil || (err.StatusCode != http.StatusTooManyRequests && err.StatusCode != http.StatusServiceUnavailable) {
		return 0
	}
	retryAfter := err.RetryAfter()
	if retryAfter <= 0 || retryAfter > maxChannelFallbackDelay {
		return 0
	}
	return retryAfter
}

func waitBeforeChannelFallback(ctx context.Context, err *types.NewAPIError) error {
	delay := channelFallbackDelay(err)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay + rand.N(delay))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func processChannelError(c *gin.Context, channelError types.ChannelError, err *types.NewAPIError) {
	logger.LogError(c, fmt.Sprintf("channel error (channel #%d, status code: %d): %s", channelError.ChannelId, err.StatusCode, common.LocalLogPreview(err.Error())))
	// 不要使用context获取渠道信息，异步处理时可能会出现渠道信息不一致的情况
	// do not use context to get channel info, there may be inconsistent channel info when processing asynchronously
	if service.ShouldDisableChannel(err) && channelError.AutoBan {
		gopool.Go(func() {
			service.DisableChannel(channelError, err.ErrorWithStatusCode())
		})
	}
}

func recordChannelErrorLog(c *gin.Context, channelError types.ChannelError, rawErr *types.NewAPIError, publicErr *types.NewAPIError) {
	if constant.ErrorLogEnabled && types.IsRecordErrorLog(rawErr) {
		// 保存错误日志到mysql中
		userId := c.GetInt("id")
		tokenName := c.GetString("token_name")
		modelName := c.GetString("original_model")
		tokenId := c.GetInt("token_id")
		userGroup := c.GetString("group")
		channelId := channelError.ChannelId
		other := make(map[string]interface{})
		if c.Request != nil && c.Request.URL != nil {
			other["request_path"] = c.Request.URL.Path
		}
		other["error_type"] = publicErr.GetErrorType()
		other["error_code"] = publicErr.GetErrorCode()
		other["status_code"] = publicErr.StatusCode
		adminInfo := make(map[string]interface{})
		adminInfo["use_channel"] = c.GetStringSlice("use_channel")
		adminInfo["channel_id"] = channelError.ChannelId
		adminInfo["channel_name"] = channelError.ChannelName
		adminInfo["channel_type"] = channelError.ChannelType
		if channelError.IsMultiKey {
			adminInfo["is_multi_key"] = true
			adminInfo["multi_key_index"] = common.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex)
		}
		service.AppendChannelAffinityAdminInfo(c, adminInfo)
		adminInfo["raw_upstream_error"] = common.LocalLogPreview(rawErr.ErrorWithStatusCode())
		adminInfo["upstream_error_type"] = rawErr.GetErrorType()
		adminInfo["upstream_error_code"] = rawErr.GetErrorCode()
		other["admin_info"] = adminInfo
		startTime := common.GetContextKeyTime(c, constant.ContextKeyRequestStartTime)
		if startTime.IsZero() {
			startTime = time.Now()
		}
		useTimeSeconds := int(time.Since(startTime).Seconds())
		model.RecordErrorLog(c, userId, channelId, modelName, tokenName, publicErr.MaskSensitiveErrorWithStatusCode(), tokenId, useTimeSeconds, common.GetContextKeyBool(c, constant.ContextKeyIsStream), userGroup, other)
	}
}

func RelayMidjourney(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatMjProxy, nil, nil)

	if err != nil {
		logger.LogError(c, "failed to generate Midjourney relay info: "+err.Error())
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"description": "Service temporarily unavailable",
			"type":        "service_unavailable",
			"code":        4,
		})
		return
	}

	var mjErr *taskdto.MidjourneyResponse
	switch relayInfo.RelayMode {
	case relayconstant.RelayModeMidjourneyNotify:
		mjErr = relay.RelayMidjourneyNotify(c)
	case relayconstant.RelayModeMidjourneyTaskFetch, relayconstant.RelayModeMidjourneyTaskFetchByCondition:
		mjErr = relay.RelayMidjourneyTask(c, relayInfo.RelayMode)
	case relayconstant.RelayModeMidjourneyTaskImageSeed:
		mjErr = relay.RelayMidjourneyTaskImageSeed(c)
	case relayconstant.RelayModeSwapFace:
		mjErr = relay.RelaySwapFace(c, relayInfo)
	default:
		mjErr = relay.RelayMidjourneySubmit(c, relayInfo)
	}
	//err = relayMidjourneySubmit(c, relayMode)
	log.Println(mjErr)
	if mjErr != nil {
		rawMessage := strings.TrimSpace(mjErr.Description + " " + mjErr.Result)
		statusCode := http.StatusServiceUnavailable
		message := "Service temporarily unavailable"
		errorType := "service_unavailable"
		if mjErr.Code == 23 || mjErr.Code == 30 {
			statusCode = http.StatusTooManyRequests
			message = "Rate limit exceeded. Retry later."
			errorType = "rate_limit_exceeded"
		}
		c.JSON(statusCode, gin.H{
			"description": message,
			"type":        errorType,
			"code":        4,
		})
		channelId := c.GetInt("channel_id")
		logger.LogError(c, fmt.Sprintf("relay error (channel #%d, status code %d): %s", channelId, statusCode, common.LocalLogPreview(rawMessage)))
	}
}

func RelayNotImplemented(c *gin.Context) {
	err := types.OpenAIError{
		Message: "API not implemented",
		Type:    "new_api_error",
		Param:   "",
		Code:    "api_not_implemented",
	}
	c.JSON(http.StatusNotImplemented, gin.H{
		"error": err,
	})
}

func RelayNotFound(c *gin.Context) {
	err := types.OpenAIError{
		Message: fmt.Sprintf("Invalid URL (%s %s)", c.Request.Method, c.Request.URL.Path),
		Type:    "invalid_request_error",
		Param:   "",
		Code:    "",
	}
	c.JSON(http.StatusNotFound, gin.H{
		"error": err,
	})
}

func RelayTaskFetch(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, &taskdto.TaskError{
			Code:       "gen_relay_info_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		})
		return
	}
	if taskErr := relay.RelayTaskFetch(c, relayInfo.RelayMode); taskErr != nil {
		respondTaskError(c, taskErr)
	}
}

func RelayTask(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, &taskdto.TaskError{
			Code:       "gen_relay_info_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		})
		return
	}

	if taskErr := relay.ResolveOriginTask(c, relayInfo); taskErr != nil {
		respondTaskError(c, taskErr)
		return
	}

	var result *relay.TaskSubmitResult
	var taskErr *taskdto.TaskError
	defer func() {
		if taskErr != nil && relayInfo.Billing != nil {
			relayInfo.Billing.Refund(c)
		}
	}()

	retryParam := &service.RetryParam{
		Ctx:         c,
		TokenGroup:  relayInfo.TokenGroup,
		ModelName:   relayInfo.OriginModelName,
		RequestPath: c.Request.URL.Path,
		Retry:       common.GetPointer(0),
	}
	var lastChannelError *types.ChannelError
	var lastRawChannelError *types.NewAPIError

	for ; retryParam.GetRetry() <= common.RetryTimes; retryParam.IncreaseRetry() {
		var channel *model.Channel

		if lockedCh, ok := relayInfo.LockedChannel.(*model.Channel); ok && lockedCh != nil {
			channel = lockedCh
			if retryParam.GetRetry() > 0 {
				if setupErr := middleware.SetupContextForSelectedChannel(c, channel, relayInfo.OriginModelName); setupErr != nil {
					logger.LogError(c, setupErr.Error())
					taskErr = service.TaskErrorWrapperLocal(errors.New("Service temporarily unavailable"), "service_unavailable", http.StatusServiceUnavailable)
					break
				}
			}
		} else {
			var channelErr *types.NewAPIError
			channel, channelErr = getChannel(c, relayInfo, retryParam)
			if channelErr != nil {
				logger.LogError(c, channelErr.Error())
				taskErr = service.TaskErrorWrapperLocal(errors.New("Service temporarily unavailable"), "service_unavailable", http.StatusServiceUnavailable)
				break
			}
		}

		addUsedChannel(c, channel.Id)
		retryParam.ExcludeChannel(channel.Id)
		bodyStorage, bodyErr := common.GetBodyStorage(c)
		if bodyErr != nil {
			if common.IsRequestBodyTooLargeError(bodyErr) || errors.Is(bodyErr, common.ErrRequestBodyTooLarge) {
				taskErr = service.TaskErrorWrapperLocal(bodyErr, "read_request_body_failed", http.StatusRequestEntityTooLarge)
			} else {
				taskErr = service.TaskErrorWrapperLocal(bodyErr, "read_request_body_failed", http.StatusBadRequest)
			}
			break
		}
		c.Request.Body = io.NopCloser(bodyStorage)

		channelOtherSettings, _ := common.GetContextKeyType[dto.ChannelOtherSettings](c, constant.ContextKeyChannelOtherSetting)
		releaseChannel, concurrencyErr := middleware.AcquireChannelConcurrency(
			c.Request.Context(),
			channel.Id,
			channelOtherSettings.ConcurrencyLimit,
		)
		if concurrencyErr != nil {
			rawChannelError := types.NewOpenAIError(
				fmt.Errorf("channel #%d concurrency limit %d unavailable: %w", channel.Id, channelOtherSettings.ConcurrencyLimit, concurrencyErr),
				types.ErrorCode("channel_concurrency_limit_exceeded"),
				http.StatusServiceUnavailable,
			)
			lastChannelError = types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey,
				common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan())
			lastRawChannelError = rawChannelError
			taskErr = service.TaskErrorWrapperLocal(errors.New("Service temporarily unavailable"), "service_unavailable", http.StatusServiceUnavailable)
			logger.LogWarn(c, rawChannelError.Error())
			if c.Request.Context().Err() == nil && retryParam.GetRetry() < common.RetryTimes {
				continue
			}
			break
		}
		func() {
			defer releaseChannel()
			result, taskErr = relay.RelayTaskSubmit(c, relayInfo)
		}()
		if taskErr == nil {
			break
		}

		if !taskErr.LocalError {
			rawChannelError := types.NewOpenAIError(taskErr.Error, types.ErrorCodeBadResponseStatusCode, taskErr.StatusCode)
			channelError := types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey,
				common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan())
			lastChannelError = channelError
			lastRawChannelError = rawChannelError
			willRetry := shouldRetryTaskRelay(c, channel.Id, taskErr, common.RetryTimes-retryParam.GetRetry())
			processChannelError(c, *channelError, rawChannelError)
			if willRetry {
				continue
			}
			taskErr = service.PublicUpstreamTaskError(taskErr)
			break
		}

		if !shouldRetryTaskRelay(c, channel.Id, taskErr, common.RetryTimes-retryParam.GetRetry()) {
			break
		}
	}

	useChannel := c.GetStringSlice("use_channel")
	if len(useChannel) > 1 {
		retryLogStr := fmt.Sprintf("重试：%s", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(useChannel)), "->"), "[]"))
		logger.LogInfo(c, retryLogStr)
	}

	// ── 成功：结算 + 日志 + 插入任务 ──
	if taskErr == nil {
		if settleErr := service.SettleBilling(c, relayInfo, result.Quota); settleErr != nil {
			common.SysError("settle task billing error: " + settleErr.Error())
		}
		service.LogTaskConsumption(c, relayInfo)

		task := model.InitTask(result.Platform, relayInfo)
		task.PrivateData.UpstreamTaskID = result.UpstreamTaskID
		task.PrivateData.BillingSource = relayInfo.BillingSource
		task.PrivateData.SubscriptionId = relayInfo.SubscriptionId
		task.PrivateData.TokenId = relayInfo.TokenId
		task.PrivateData.NodeName = common.NodeName
		task.PrivateData.BillingContext = &model.TaskBillingContext{
			ModelPrice:      relayInfo.PriceData.ModelPrice,
			GroupRatio:      relayInfo.PriceData.GroupRatioInfo.GroupRatio,
			ModelRatio:      relayInfo.PriceData.ModelRatio,
			OtherRatios:     relayInfo.PriceData.OtherRatios(),
			OriginModelName: relayInfo.OriginModelName,
			PerCallBilling:  common.StringsContains(constant.TaskPricePatches, relayInfo.OriginModelName) || relayInfo.PriceData.UsePrice,
		}
		task.Quota = result.Quota
		task.Data = result.TaskData
		task.Action = relayInfo.Action
		if insertErr := task.Insert(); insertErr != nil {
			common.SysError("insert task error: " + insertErr.Error())
		}
	}

	if taskErr != nil {
		if lastChannelError != nil && lastRawChannelError != nil {
			publicLogError := types.NewOpenAIError(errors.New(taskErr.Message), types.ErrorCode(taskErr.Code), taskErr.StatusCode)
			recordChannelErrorLog(c, *lastChannelError, lastRawChannelError, publicLogError)
		}
		respondTaskError(c, taskErr)
	}
}

// respondTaskError 统一输出 Task 错误响应（含 429 限流提示改写）
func respondTaskError(c *gin.Context, taskErr *taskdto.TaskError) {
	if !taskErr.LocalError {
		taskErr = service.PublicUpstreamTaskError(taskErr)
	}
	c.JSON(taskErr.StatusCode, taskErr)
}

func shouldRetryTaskRelay(c *gin.Context, channelId int, taskErr *taskdto.TaskError, retryTimes int) bool {
	if taskErr == nil {
		return false
	}
	if service.ShouldSkipRetryAfterChannelAffinityFailure(c) {
		return false
	}
	if retryTimes <= 0 {
		return false
	}
	if _, ok := c.Get("specific_channel_id"); ok {
		return false
	}
	if taskErr.StatusCode == http.StatusTooManyRequests {
		return true
	}
	if taskErr.StatusCode == 307 {
		return true
	}
	if taskErr.StatusCode/100 == 5 {
		// 超时不重试
		if operation_setting.IsAlwaysSkipRetryStatusCode(taskErr.StatusCode) {
			return false
		}
		return true
	}
	if taskErr.StatusCode == http.StatusBadRequest {
		return false
	}
	if taskErr.StatusCode == 408 {
		// azure处理超时不重试
		return false
	}
	if taskErr.LocalError {
		return false
	}
	if taskErr.StatusCode/100 == 2 {
		return false
	}
	return true
}
