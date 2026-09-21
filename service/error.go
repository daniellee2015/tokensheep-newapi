package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
)

func MidjourneyErrorWrapper(code int, desc string) *taskdto.MidjourneyResponse {
	return &taskdto.MidjourneyResponse{
		Code:        code,
		Description: desc,
	}
}

func MidjourneyErrorWithStatusCodeWrapper(code int, desc string, statusCode int) *taskdto.MidjourneyResponseWithStatusCode {
	return &taskdto.MidjourneyResponseWithStatusCode{
		StatusCode: statusCode,
		Response:   *MidjourneyErrorWrapper(code, desc),
	}
}

//// OpenAIErrorWrapper wraps an error into an OpenAIErrorWithStatusCode
//func OpenAIErrorWrapper(err error, code string, statusCode int) *dto.OpenAIErrorWithStatusCode {
//	text := err.Error()
//	lowerText := strings.ToLower(text)
//	if !strings.HasPrefix(lowerText, "get file base64 from url") && !strings.HasPrefix(lowerText, "mime type is not supported") {
//		if strings.Contains(lowerText, "post") || strings.Contains(lowerText, "dial") || strings.Contains(lowerText, "http") {
//			common.SysLog(fmt.Sprintf("error: %s", text))
//			text = "请求上游地址失败"
//		}
//	}
//	openAIError := dto.OpenAIError{
//		Message: text,
//		Type:    "new_api_error",
//		Code:    code,
//	}
//	return &dto.OpenAIErrorWithStatusCode{
//		Error:      openAIError,
//		StatusCode: statusCode,
//	}
//}
//
//func OpenAIErrorWrapperLocal(err error, code string, statusCode int) *dto.OpenAIErrorWithStatusCode {
//	openaiErr := OpenAIErrorWrapper(err, code, statusCode)
//	openaiErr.LocalError = true
//	return openaiErr
//}

func ClaudeErrorWrapper(err error, code string, statusCode int) *dto.ClaudeErrorWithStatusCode {
	text := err.Error()
	lowerText := strings.ToLower(text)
	if !strings.HasPrefix(lowerText, "get file base64 from url") {
		if strings.Contains(lowerText, "post") || strings.Contains(lowerText, "dial") || strings.Contains(lowerText, "http") {
			common.SysLog(fmt.Sprintf("error: %s", text))
			text = "请求上游地址失败"
		}
	}
	errType := "invalid_request_error"
	switch statusCode {
	case http.StatusNotFound:
		errType = "not_found_error"
	case http.StatusUnauthorized:
		errType = "authentication_error"
	case http.StatusForbidden:
		errType = "permission_error"
	case http.StatusTooManyRequests:
		errType = "rate_limit_error"
	case http.StatusServiceUnavailable:
		errType = "overloaded_error"
	case http.StatusBadRequest:
		errType = "invalid_request_error"
	default:
		if statusCode >= 500 {
			errType = "api_error"
		}
	}
	claudeError := types.ClaudeError{
		Message: text,
		Type:    errType,
	}
	return &dto.ClaudeErrorWithStatusCode{
		Error:      claudeError,
		StatusCode: statusCode,
	}
}

func ClaudeErrorWrapperLocal(err error, code string, statusCode int) *dto.ClaudeErrorWithStatusCode {
	claudeErr := ClaudeErrorWrapper(err, code, statusCode)
	claudeErr.LocalError = true
	return claudeErr
}

func RelayErrorHandler(ctx context.Context, resp *http.Response, showBodyWhenFail bool) (newApiErr *types.NewAPIError) {
	return relayErrorHandlerAt(ctx, resp, showBodyWhenFail, time.Now())
}

func relayErrorHandlerAt(ctx context.Context, resp *http.Response, showBodyWhenFail bool, now time.Time) (newApiErr *types.NewAPIError) {
	retryAfter := parseUpstreamRetryAfter(resp.Header.Get("Retry-After"), now)
	defer func() {
		if newApiErr != nil {
			newApiErr.SetRetryAfter(retryAfter)
		}
	}()
	newApiErr = types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, resp.StatusCode)

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}
	CloseResponseBodyGracefully(resp)
	var errResponse dto.GeneralErrorResponse
	responseBodyText := string(responseBody)
	responseBodyPreview := common.LocalLogPreview(responseBodyText)
	if retryAfter <= 0 && resp.StatusCode == http.StatusServiceUnavailable {
		lowerBody := strings.ToLower(responseBodyText)
		if strings.Contains(lowerBody, "model_capacity_exhausted") ||
			strings.Contains(lowerBody, "no capacity available for model") ||
			(strings.Contains(lowerBody, "temporarily unavailable") && strings.Contains(lowerBody, "retry in 1s")) {
			retryAfter = time.Second
		}
	}
	buildErrWithBody := func(message string) error {
		if message == "" {
			return fmt.Errorf("bad response status code %d, body: %s", resp.StatusCode, responseBodyText)
		}
		return fmt.Errorf("bad response status code %d, message: %s, body: %s", resp.StatusCode, message, responseBodyText)
	}

	err = common.Unmarshal(responseBody, &errResponse)
	if err != nil {
		if showBodyWhenFail {
			newApiErr.Err = buildErrWithBody("")
		} else {
			logger.LogError(ctx, fmt.Sprintf("bad response status code %d, body: %s", resp.StatusCode, responseBodyPreview))
			newApiErr.Err = fmt.Errorf("bad response status code %d", resp.StatusCode)
		}
		return
	}

	if common.GetJsonType(errResponse.Error) == "object" {
		// General format error (OpenAI, Anthropic, Gemini, etc.)
		oaiError := errResponse.TryToOpenAIError()
		if oaiError != nil {
			newApiErr = types.WithOpenAIError(*oaiError, resp.StatusCode)
			if showBodyWhenFail {
				newApiErr.Err = buildErrWithBody(newApiErr.Error())
			}
			return
		}
	}
	message := errResponse.ToMessage()
	if message == "" {
		// The body parsed as JSON but carried no usable error message; log the
		// raw body so the upstream failure remains diagnosable.
		logger.LogError(ctx, fmt.Sprintf("bad response status code %d with empty error message, body: %s", resp.StatusCode, responseBodyPreview))
	}
	newApiErr = types.NewOpenAIError(errors.New(message), types.ErrorCodeBadResponseStatusCode, resp.StatusCode)
	if showBodyWhenFail {
		newApiErr.Err = buildErrWithBody(newApiErr.Error())
	}
	return
}

func parseUpstreamRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, errParse := strconv.ParseInt(value, 10, 64); errParse == nil {
		if seconds <= 0 || seconds > int64((30*24*time.Hour)/time.Second) {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	retryAt, errParse := http.ParseTime(value)
	if errParse != nil {
		return 0
	}
	retryAfter := retryAt.Sub(now)
	if retryAfter <= 0 || retryAfter > 30*24*time.Hour {
		return 0
	}
	return retryAfter
}

const (
	publicInvalidRequestMessage     = "Invalid request"
	publicRateLimitMessage          = "Rate limit exceeded. Retry later."
	publicServiceUnavailableMessage = "Service temporarily unavailable"
)

// PublicUpstreamError replaces every upstream-controlled field with a value
// defined by this gateway. Retry, channel health and operator diagnostics must
// use the original error before calling this function.
func PublicUpstreamError(upstreamErr *types.NewAPIError) *types.NewAPIError {
	if upstreamErr == nil {
		return nil
	}

	statusCode, message, code := publicUpstreamErrorPolicy(upstreamErr.StatusCode)
	publicErr := types.NewOpenAIError(errors.New(message), code, statusCode, types.ErrOptionWithSkipRetry())
	publicErr.SetRetryAfter(upstreamErr.RetryAfter())
	return publicErr
}

// PublicUpstreamTaskError applies the same trust boundary to task endpoints.
func PublicUpstreamTaskError(upstreamErr *taskdto.TaskError) *taskdto.TaskError {
	if upstreamErr == nil {
		return nil
	}

	statusCode, message, code := publicUpstreamErrorPolicy(upstreamErr.StatusCode)
	return &taskdto.TaskError{
		Code:       string(code),
		Message:    message,
		StatusCode: statusCode,
		Error:      errors.New(message),
	}
}

// EmbeddedUpstreamError detects an error envelope returned with a successful
// HTTP status. Callers must invoke it before copying a JSON-capable upstream
// body to the downstream writer.
func EmbeddedUpstreamError(responseBody []byte) *types.NewAPIError {
	var errResponse dto.GeneralErrorResponse
	if err := common.Unmarshal(responseBody, &errResponse); err != nil {
		return nil
	}
	if openAIError := errResponse.TryToOpenAIError(); openAIError != nil {
		return types.WithOpenAIError(*openAIError, http.StatusInternalServerError)
	}
	message := errResponse.ToMessage()
	if message == "" {
		return nil
	}
	return types.NewOpenAIError(errors.New(message), types.ErrorCodeBadResponse, http.StatusInternalServerError)
}

func publicUpstreamErrorPolicy(statusCode int) (int, string, types.ErrorCode) {
	switch statusCode {
	case http.StatusBadRequest,
		http.StatusNotFound,
		http.StatusMethodNotAllowed,
		http.StatusConflict,
		http.StatusGone,
		http.StatusRequestEntityTooLarge,
		http.StatusUnsupportedMediaType,
		http.StatusUnprocessableEntity:
		return http.StatusBadRequest, publicInvalidRequestMessage, types.ErrorCodeInvalidRequest
	case http.StatusTooManyRequests:
		return http.StatusTooManyRequests, publicRateLimitMessage, types.ErrorCodeRateLimitExceeded
	default:
		return http.StatusServiceUnavailable, publicServiceUnavailableMessage, types.ErrorCodeServiceUnavailable
	}
}

func ResetStatusCode(newApiErr *types.NewAPIError, statusCodeMappingStr string) {
	if newApiErr == nil {
		return
	}
	if statusCodeMappingStr == "" || statusCodeMappingStr == "{}" {
		return
	}
	statusCodeMapping := make(map[string]any)
	err := common.Unmarshal([]byte(statusCodeMappingStr), &statusCodeMapping)
	if err != nil {
		return
	}
	if newApiErr.StatusCode == http.StatusOK {
		return
	}
	codeStr := strconv.Itoa(newApiErr.StatusCode)
	if value, ok := statusCodeMapping[codeStr]; ok {
		intCode, ok := parseStatusCodeMappingValue(value)
		if !ok {
			return
		}
		newApiErr.StatusCode = intCode
	}
}

func parseStatusCodeMappingValue(value any) (int, bool) {
	switch v := value.(type) {
	case string:
		if v == "" {
			return 0, false
		}
		statusCode, err := strconv.Atoi(v)
		if err != nil {
			return 0, false
		}
		return statusCode, true
	case float64:
		if v != math.Trunc(v) {
			return 0, false
		}
		return int(v), true
	case int:
		return v, true
	case json.Number:
		statusCode, err := strconv.Atoi(v.String())
		if err != nil {
			return 0, false
		}
		return statusCode, true
	default:
		return 0, false
	}
}

func TaskErrorWrapperLocal(err error, code string, statusCode int) *taskdto.TaskError {
	openaiErr := TaskErrorWrapper(err, code, statusCode)
	openaiErr.LocalError = true
	return openaiErr
}

func TaskErrorWrapper(err error, code string, statusCode int) *taskdto.TaskError {
	text := err.Error()
	text = common.StripNestedRequestIDs(text)
	text = ApplyGlobalErrorMask(text)
	lowerText := strings.ToLower(text)
	if strings.Contains(lowerText, "post") || strings.Contains(lowerText, "dial") || strings.Contains(lowerText, "http") {
		common.SysLog(fmt.Sprintf("error: %s", text))
		//text = "请求上游地址失败"
		text = common.MaskSensitiveInfo(text)
	}
	//避免暴露内部错误
	taskError := &taskdto.TaskError{
		Code:       code,
		Message:    text,
		StatusCode: statusCode,
		Error:      err,
	}

	return taskError
}

// TaskErrorFromAPIError 将 PreConsumeBilling 返回的 NewAPIError 转换为 TaskError。
func TaskErrorFromAPIError(apiErr *types.NewAPIError) *taskdto.TaskError {
	if apiErr == nil {
		return nil
	}
	return &taskdto.TaskError{
		Code:       string(apiErr.GetErrorCode()),
		Message:    apiErr.Err.Error(),
		StatusCode: apiErr.StatusCode,
		Error:      apiErr.Err,
	}
}
