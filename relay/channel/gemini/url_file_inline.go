package gemini

import (
	"strings"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// Gemini's fileData.fileUri is meant for Google-hosted references (File API,
// GCS, YouTube). A plain public image URL is not fetched reliably by Google, so
// the gateway downloads it itself and inlines the bytes instead. Inlined base64
// is bounded both per image and per request: Gemini rejects inline requests
// over 20MB, and an unbounded multi-image request would also blow up gateway
// memory. Oversized payloads keep the original fileData untouched.
type geminiInlineLimits struct {
	perImageBase64Bytes   int64
	perRequestBase64Bytes int64
}

var defaultGeminiInlineLimits = geminiInlineLimits{
	// Keep a single inlined image small (~750KB of image bytes). Gemini
	// downscales inputs anyway, so a bigger inline payload only burns gateway
	// memory and request budget. Raise these two values if operators need to
	// inline larger images.
	perImageBase64Bytes: 1 * 1024 * 1024,
	// At most 2MB of inline image data per request, leaving plenty of headroom
	// for text, tools and JSON overhead under Gemini's 20MB inline limit.
	perRequestBase64Bytes: 2 * 1024 * 1024,
}

// geminiInlineImageMimeTypes lists the image formats Google documents for
// inline data. Anything else is left for the upstream to fetch (or reject).
var geminiInlineImageMimeTypes = map[string]struct{}{
	"image/png":  {},
	"image/jpeg": {},
	"image/jpg":  {},
	"image/webp": {},
	"image/heic": {},
	"image/heif": {},
}

// inlineGeminiFileURLParts converts http(s) fileData references into inlineData
// so Google receives the actual bytes. Google-hosted references, YouTube links,
// audio/video URLs, oversized files and download failures keep the original
// fileData and therefore keep the previous behavior.
func inlineGeminiFileURLParts(c *gin.Context, request *dto.GeminiChatRequest) {
	inlineGeminiFileURLPartsWithLimits(c, request, defaultGeminiInlineLimits)
}

func inlineGeminiFileURLPartsWithLimits(c *gin.Context, request *dto.GeminiChatRequest, limits geminiInlineLimits) {
	if request == nil {
		return
	}
	budget := limits.perRequestBase64Bytes - geminiInlineBase64Size(request)
	if budget < 0 {
		budget = 0
	}
	for i := range request.Contents {
		inlineGeminiParts(c, request.Contents[i].Parts, limits, &budget)
	}
	if request.SystemInstructions != nil {
		inlineGeminiParts(c, request.SystemInstructions.Parts, limits, &budget)
	}
}

func inlineGeminiParts(c *gin.Context, parts []dto.GeminiPart, limits geminiInlineLimits, budget *int64) {
	for i := range parts {
		part := &parts[i]
		if part.FileData == nil || part.InlineData != nil {
			continue
		}
		fileUri := strings.TrimSpace(part.FileData.FileUri)
		lowerFileUri := strings.ToLower(fileUri)
		if !strings.HasPrefix(lowerFileUri, "http://") && !strings.HasPrefix(lowerFileUri, "https://") {
			continue
		}
		if isGoogleHostedFileUri(lowerFileUri) || isYouTubeFileUri(lowerFileUri) {
			continue
		}
		if declaredMimeType := strings.ToLower(strings.TrimSpace(part.FileData.MimeType)); strings.HasPrefix(declaredMimeType, "video/") ||
			strings.HasPrefix(declaredMimeType, "audio/") {
			continue
		}

		base64Data, mimeType, err := service.GetBase64Data(c, types.NewURLFileSource(fileUri), "formatting Gemini file URL")
		if err != nil {
			logger.LogDebug(c, "gemini fileData url inlining skipped: "+err.Error())
			continue
		}
		mimeType = strings.ToLower(strings.TrimSpace(mimeType))
		if !isGeminiInlinableMimeType(mimeType) {
			continue
		}
		if int64(len(base64Data)) > limits.perImageBase64Bytes || int64(len(base64Data)) > *budget {
			// Oversized image: shrink and re-encode instead of dropping the
			// picture. PDFs and formats Go cannot decode (HEIC/AVIF) keep the
			// original fileData.
			if !strings.HasPrefix(mimeType, "image/") {
				logger.LogDebug(c, "gemini fileData url inlining skipped: payload exceeds inline limit")
				continue
			}
			target := limits.perImageBase64Bytes
			if *budget < target {
				target = *budget
			}
			shrunkData, shrunk := shrinkGeminiInlineBase64(base64Data, target)
			if !shrunk {
				logger.LogDebug(c, "gemini fileData url inlining skipped: payload exceeds inline limit")
				continue
			}
			base64Data = shrunkData
			mimeType = "image/jpeg"
		}

		part.InlineData = &dto.GeminiInlineData{MimeType: mimeType, Data: base64Data}
		part.FileData = nil
		*budget -= int64(len(base64Data))
	}
}

// geminiInlineBase64Size counts the inline payload already present in the
// request so it is deducted from the inlining budget.
func geminiInlineBase64Size(request *dto.GeminiChatRequest) int64 {
	var size int64
	for _, content := range request.Contents {
		size += geminiPartsInlineBase64Size(content.Parts)
	}
	if request.SystemInstructions != nil {
		size += geminiPartsInlineBase64Size(request.SystemInstructions.Parts)
	}
	return size
}

func geminiPartsInlineBase64Size(parts []dto.GeminiPart) int64 {
	var size int64
	for _, part := range parts {
		if part.InlineData != nil {
			size += int64(len(part.InlineData.Data))
		}
	}
	return size
}

func isGoogleHostedFileUri(lowerFileUri string) bool {
	return strings.Contains(lowerFileUri, "generativelanguage.googleapis.com") ||
		strings.Contains(lowerFileUri, "aiplatform.googleapis.com")
}

func isYouTubeFileUri(lowerFileUri string) bool {
	return strings.Contains(lowerFileUri, "youtube.com") || strings.Contains(lowerFileUri, "youtu.be")
}

func isGeminiInlinableMimeType(mimeType string) bool {
	if mimeType == "application/pdf" {
		return true
	}
	_, ok := geminiInlineImageMimeTypes[mimeType]
	return ok
}
