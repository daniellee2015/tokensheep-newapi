package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relaykit/dto"
	relaymedia "github.com/QuantumNous/new-api/relaykit/media"

	"github.com/gin-gonic/gin"
)

// NormalizeClaudeRequestImages applies the Claude image compatibility layer to
// a Claude-format request and logs every rewrite.
//
// Claude only accepts image/jpeg, image/png, image/gif and image/webp. Images
// that reach us through a URL download or a client-provided data URI come with
// whatever MIME type the source advertised (image/jpg, image/avif, image/bmp,
// ...), which upstream rejects with `source.base64.media_type: Input should be
// 'image/jpeg', 'image/png', 'image/gif' or 'image/webp'`. The relaykit pass
// repairs the declaration, transcodes decodable formats and degrades the rest
// to a text placeholder instead of failing the whole request.
func NormalizeClaudeRequestImages(c *gin.Context, request *dto.ClaudeRequest) {
	for _, note := range relaymedia.NormalizeClaudeRequestImages(request) {
		message := "claude image media compatibility: " + note
		if c == nil {
			common.SysLog(message)
			continue
		}
		logger.LogWarn(c, message)
	}
}
