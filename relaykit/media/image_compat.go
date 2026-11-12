// Package media provides protocol media compatibility helpers shared by the
// relay host and the protocol converters.
//
// It has no dependency on the host's file service or on Gin: callers pass the
// already-resolved payload, so the same normalization runs whether the bytes
// came from a URL download or from an inline base64 block.
//
// NormalizeClaudeRequestImages understands the content shapes a parsed Claude
// request can carry: plain strings, []dto.ClaudeMediaMessage built by the
// converters and []any decoded straight from client JSON. Other shapes (for
// example json.RawMessage content) must be converted by the caller first.
package media

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"

	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
)

// Media types accepted by the Claude Messages API for image blocks. Anything
// outside this set is rejected upstream with a 400 validation error.
const (
	MediaTypeJPEG = "image/jpeg"
	MediaTypePNG  = "image/png"
	MediaTypeGIF  = "image/gif"
	MediaTypeWebP = "image/webp"
)

// Formats the compatibility layer still recognizes by sniffing: Claude rejects
// them, but the value decides whether we transcode or degrade the block.
const (
	mediaTypeBMP  = "image/bmp"
	mediaTypeTIFF = "image/tiff"
	mediaTypeAVIF = "image/avif"
	mediaTypeHEIC = "image/heic"
	mediaTypeSVG  = "image/svg+xml"
)

const (
	// maxTranscodePixels bounds the image we are willing to decode when
	// transcoding BMP/TIFF. A tiny header can claim enormous dimensions
	// (bmp.DecodeConfig happily reports 100000x100000), so the limit is
	// enforced on the config before Decode allocates the pixel buffer. This is
	// the one place where the compatibility layer allocates from a
	// client-controlled header, so it must stay well below the gateway's
	// memory budget.
	maxTranscodePixels = 25_000_000
	// maxImageCompatNotes caps per-request log notes so a request full of
	// unusable images cannot flood the log or the placeholder text.
	maxImageCompatNotes = 5
	// base64SniffPrefixSize is enough base64 to decode the magic bytes of every
	// format SniffImageMediaType recognizes (12 bytes) with margin.
	base64SniffPrefixSize = 32
)

var errImageTooLarge = errors.New("image dimensions exceed the transcode limit")

// CanonicalClaudeImageMediaType normalizes mimeType and reports whether Claude
// accepts it. Tolerated inputs cover the aliases seen in the wild (image/jpg,
// image/pjpeg, image/x-png...), charset parameters and data URI prefixes.
func CanonicalClaudeImageMediaType(mimeType string) (string, bool) {
	normalized := normalizeMimeType(mimeType)
	switch normalized {
	case MediaTypeJPEG, "image/jpg", "image/pjpeg", "image/x-jpeg", "image/jpe":
		return MediaTypeJPEG, true
	case MediaTypePNG, "image/x-png":
		return MediaTypePNG, true
	case MediaTypeGIF:
		return MediaTypeGIF, true
	case MediaTypeWebP, "image/x-webp":
		return MediaTypeWebP, true
	}
	return "", false
}

// SniffImageMediaType recognizes an image format from its magic bytes. Claude
// supported formats are returned in canonical spelling; formats Claude rejects
// but we can still name (bmp/tiff/avif/heic/svg) are recognized as well so
// callers can decide between transcoding and degrading. An empty string means
// the payload is not a recognized image.
func SniffImageMediaType(raw []byte) string {
	switch {
	case len(raw) >= 3 && raw[0] == 0xFF && raw[1] == 0xD8 && raw[2] == 0xFF:
		return MediaTypeJPEG
	case len(raw) >= 8 && bytes.HasPrefix(raw, []byte("\x89PNG\r\n\x1a\n")):
		return MediaTypePNG
	case len(raw) >= 6 && (bytes.HasPrefix(raw, []byte("GIF87a")) || bytes.HasPrefix(raw, []byte("GIF89a"))):
		return MediaTypeGIF
	case len(raw) >= 12 && bytes.HasPrefix(raw, []byte("RIFF")) && bytes.Equal(raw[8:12], []byte("WEBP")):
		return MediaTypeWebP
	case len(raw) >= 2 && raw[0] == 'B' && raw[1] == 'M':
		return mediaTypeBMP
	case len(raw) >= 4 && (bytes.Equal(raw[:4], []byte{'I', 'I', 0x2A, 0x00}) || bytes.Equal(raw[:4], []byte{'M', 'M', 0x00, 0x2A})):
		return mediaTypeTIFF
	case len(raw) >= 12 && bytes.Equal(raw[4:8], []byte("ftyp")):
		brand := string(raw[8:12])
		switch brand {
		case "avif", "avis":
			return mediaTypeAVIF
		case "heic", "heix", "hevc", "hevx", "heim", "heis", "hevm", "hevs", "mif1", "msf1":
			return mediaTypeHEIC
		}
	case isSVGImage(raw):
		return mediaTypeSVG
	}
	return ""
}

// PrepareClaudeImageData rewrites an image payload so a Claude-format upstream
// accepts it. The returned mediaType is final: it is either a Claude-accepted
// value or, when dropped is true, the caller must replace the image block with
// a text placeholder built from note. note is also set for lossless rewrites
// worth logging (transcodes, declared/actual type mismatches).
func PrepareClaudeImageData(raw []byte, declaredMediaType string) (data []byte, mediaType string, dropped bool, note string) {
	if len(raw) == 0 {
		return raw, declaredMediaType, true, "image omitted: empty image payload"
	}

	declared, declaredAccepted := CanonicalClaudeImageMediaType(declaredMediaType)
	detected := SniffImageMediaType(raw)

	// Fast path: the declaration already matches what the bytes say.
	if declaredAccepted && (detected == "" || detected == declared) {
		return raw, declared, false, ""
	}

	// The payload is in a format Claude accepts; only the label needs fixing.
	if canonical, ok := CanonicalClaudeImageMediaType(detected); ok {
		note := ""
		if declaredAccepted {
			note = fmt.Sprintf("image media type mismatch: declared %s but payload is %s", declared, canonical)
		}
		return raw, canonical, false, note
	}

	// Claude rejects the declared/detected format, so transcode when Go can
	// decode it, otherwise degrade the block.
	format := describeImageFormat(detected, declaredMediaType)
	decoded, err := decodeImage(raw)
	if err != nil {
		if errors.Is(err, errImageTooLarge) {
			return raw, declaredMediaType, true, fmt.Sprintf("image omitted: %s image is too large to transcode for Claude", format)
		}
		return raw, declaredMediaType, true, fmt.Sprintf("image omitted: unsupported image format %s for Claude", format)
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, decoded); err != nil {
		return raw, declaredMediaType, true, fmt.Sprintf("image omitted: failed to transcode %s image to image/png", format)
	}
	return encoded.Bytes(), MediaTypePNG, false, fmt.Sprintf("transcoded %s image to image/png for Claude", format)
}

// PrepareClaudeImageBase64 is PrepareClaudeImageData for inline base64 payloads.
// The common case (declaration already canonical and matching the bytes) costs
// one prefix sniff and keeps the caller's string; the full payload is decoded
// only when the declaration has to be repaired.
func PrepareClaudeImageBase64(base64Data, declaredMediaType string) (data string, mediaType string, dropped bool, note string) {
	declared, declaredAccepted := CanonicalClaudeImageMediaType(declaredMediaType)
	plainBase64 := isPlainBase64Payload(base64Data)
	if declaredAccepted && plainBase64 {
		if detected := sniffBase64ImageMediaType(base64Data); detected == "" || detected == declared {
			return base64Data, declared, false, ""
		}
	}

	raw, err := decodeBase64Payload(base64Data)
	if err != nil {
		return base64Data, declaredMediaType, true, "image omitted: image payload is not valid base64"
	}
	prepared, mediaType, dropped, note := PrepareClaudeImageData(raw, declaredMediaType)
	if dropped {
		return base64Data, mediaType, true, note
	}
	// The payload did not change when PrepareClaudeImageData returned the same
	// underlying slice; keep the caller's string in that case, but re-encode
	// data URI / whitespace / URL-safe input into canonical base64.
	if plainBase64 && len(prepared) > 0 && len(prepared) == len(raw) && &prepared[0] == &raw[0] {
		return base64Data, mediaType, false, note
	}
	return base64.StdEncoding.EncodeToString(prepared), mediaType, false, note
}

// NormalizeClaudeRequestImages applies the compatibility layer to every inline
// image block of a Messages request, including images nested in tool_result
// content and images carried in the system field. Images that cannot be
// represented for Claude are replaced by a text placeholder so the request
// stays valid. The returned notes describe every rewrite for the caller to log.
func NormalizeClaudeRequestImages(request *dto.ClaudeRequest) []string {
	if request == nil {
		return nil
	}
	var notes []string
	for i := range request.Messages {
		content, changed := normalizeClaudeContent(request.Messages[i].Content, &notes)
		if changed {
			request.Messages[i].SetContent(content)
		}
	}
	if request.System != nil {
		system, changed := normalizeClaudeContent(request.System, &notes)
		if changed {
			request.System = system
		}
	}
	return notes
}

// normalizeClaudeContent handles the three shapes a Claude content field can
// take: typed media blocks built by our converters, raw JSON arrays from native
// requests, and plain strings (no images).
func normalizeClaudeContent(content any, notes *[]string) (any, bool) {
	switch value := content.(type) {
	case []dto.ClaudeMediaMessage:
		blocks, changed := normalizeClaudeMediaBlocks(value, notes)
		if !changed {
			return content, false
		}
		return blocks, true
	case []any:
		// Native requests keep raw maps; mutate them in place so fields we do
		// not model (cache_control, citations, ...) survive.
		changed := false
		for i, item := range value {
			switch typed := item.(type) {
			case map[string]any:
				if normalizeRawClaudeBlock(typed, notes) {
					changed = true
				}
			case dto.ClaudeMediaMessage:
				blocks := []dto.ClaudeMediaMessage{typed}
				normalized, blockChanged := normalizeClaudeMediaBlocks(blocks, notes)
				if blockChanged {
					value[i] = normalized[0]
					changed = true
				}
			}
		}
		return content, changed
	default:
		return content, false
	}
}

func normalizeClaudeMediaBlocks(blocks []dto.ClaudeMediaMessage, notes *[]string) ([]dto.ClaudeMediaMessage, bool) {
	changed := false
	for i := range blocks {
		block := &blocks[i]
		switch block.Type {
		case "image":
			if block.Source == nil || !isInlineImageSource(block.Source.Type, block.Source.Url) ||
				!shouldNormalizeImageSource(block.Source.MediaType) {
				continue
			}
			data := interfaceString(block.Source.Data)
			if data == "" {
				note := "image omitted: empty image payload"
				replaceClaudeBlockWithPlaceholder(block, note)
				appendImageCompatNote(notes, note)
				changed = true
				continue
			}
			newData, newMediaType, dropped, note := PrepareClaudeImageBase64(data, block.Source.MediaType)
			appendImageCompatNote(notes, note)
			if dropped {
				replaceClaudeBlockWithPlaceholder(block, note)
				changed = true
				continue
			}
			if newData != data || newMediaType != block.Source.MediaType {
				block.Source.Type = "base64"
				block.Source.Data = newData
				block.Source.MediaType = newMediaType
				changed = true
			}
		case "tool_result":
			nested, nestedChanged := normalizeClaudeContent(block.Content, notes)
			if nestedChanged {
				block.Content = nested
				changed = true
			}
		}
	}
	return blocks, changed
}

func normalizeRawClaudeBlock(block map[string]any, notes *[]string) bool {
	switch interfaceString(block["type"]) {
	case "image":
		source, ok := block["source"].(map[string]any)
		if !ok || !isInlineImageSource(interfaceString(source["type"]), interfaceString(source["url"])) {
			return false
		}
		declaredMediaType := interfaceString(source["media_type"])
		if !shouldNormalizeImageSource(declaredMediaType) {
			return false
		}
		data := interfaceString(source["data"])
		if data == "" {
			note := "image omitted: empty image payload"
			replaceRawClaudeBlockWithPlaceholder(block, note)
			appendImageCompatNote(notes, note)
			return true
		}
		newData, newMediaType, dropped, note := PrepareClaudeImageBase64(data, declaredMediaType)
		appendImageCompatNote(notes, note)
		if dropped {
			replaceRawClaudeBlockWithPlaceholder(block, note)
			return true
		}
		if newData == data && newMediaType == declaredMediaType {
			return false
		}
		source["type"] = "base64"
		source["data"] = newData
		source["media_type"] = newMediaType
		return true
	case "tool_result":
		content, ok := block["content"]
		if !ok {
			return false
		}
		nested, changed := normalizeClaudeContent(content, notes)
		if changed {
			block["content"] = nested
		}
		return changed
	}
	return false
}

// replaceClaudeBlockWithPlaceholder turns a typed image block into the text
// block that replaces it, keeping cache_control (and any other unrelated
// fields) intact.
func replaceClaudeBlockWithPlaceholder(block *dto.ClaudeMediaMessage, note string) {
	placeholder := "[" + note + "]"
	block.Type = "text"
	block.Text = &placeholder
	block.Source = nil
	block.Content = nil
}

func replaceRawClaudeBlockWithPlaceholder(block map[string]any, note string) {
	cacheControl, hasCacheControl := block["cache_control"]
	for key := range block {
		delete(block, key)
	}
	block["type"] = "text"
	block["text"] = "[" + note + "]"
	if hasCacheControl {
		block["cache_control"] = cacheControl
	}
}

func appendImageCompatNote(notes *[]string, note string) {
	if note == "" {
		return
	}
	if len(*notes) < maxImageCompatNotes {
		*notes = append(*notes, note)
		return
	}
	if len(*notes) == maxImageCompatNotes {
		*notes = append(*notes, "further image compatibility rewrites omitted from logs")
	}
}

// shouldNormalizeImageSource keeps the layer scoped to image payloads. A block
// whose declared type is clearly non-image (audio, video, PDF, ...) is left
// untouched so its previous upstream validation error is not replaced by an
// image placeholder.
func shouldNormalizeImageSource(declaredMediaType string) bool {
	normalized := normalizeMimeType(declaredMediaType)
	switch normalized {
	case "", "application/octet-stream", "binary/octet-stream", "application/binary",
		"application/unknown", "application/x-unknown":
		return true
	}
	return strings.HasPrefix(normalized, "image/")
}

func isInlineImageSource(sourceType, url string) bool {
	if strings.EqualFold(strings.TrimSpace(sourceType), "url") {
		return false
	}
	return strings.TrimSpace(url) == ""
}

func decodeImage(raw []byte) (image.Image, error) {
	if err := checkTranscodeDimensions(raw); err != nil {
		return nil, err
	}
	decoded, _, err := image.Decode(bytes.NewReader(raw))
	if err == nil {
		return decoded, nil
	}
	if decoded, bmpErr := bmp.Decode(bytes.NewReader(raw)); bmpErr == nil {
		return decoded, nil
	}
	if decoded, tiffErr := tiff.Decode(bytes.NewReader(raw)); tiffErr == nil {
		return decoded, nil
	}
	return nil, err
}

// checkTranscodeDimensions reads the image header without allocating the pixel
// buffer and rejects payloads whose dimensions exceed maxTranscodePixels.
func checkTranscodeDimensions(raw []byte) error {
	config, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		config, err = bmp.DecodeConfig(bytes.NewReader(raw))
	}
	if err != nil {
		config, err = tiff.DecodeConfig(bytes.NewReader(raw))
	}
	if err != nil {
		return err
	}
	if config.Width <= 0 || config.Height <= 0 {
		return errors.New("image has invalid dimensions")
	}
	if int64(config.Width)*int64(config.Height) > maxTranscodePixels {
		return errImageTooLarge
	}
	return nil
}

func decodeBase64Payload(base64Data string) ([]byte, error) {
	payload := strings.TrimSpace(base64Data)
	// The data URI scheme is case-insensitive.
	if len(payload) >= 5 && strings.EqualFold(payload[:5], "data:") {
		if comma := strings.IndexByte(payload, ','); comma >= 0 {
			payload = strings.TrimSpace(payload[comma+1:])
		}
	}
	// Base64 payloads copied from email/JSON sometimes carry line wraps.
	payload = strings.NewReplacer("\r", "", "\n", "", " ", "", "\t", "").Replace(payload)
	if payload == "" {
		return nil, errors.New("empty base64 payload")
	}
	if raw, err := base64.StdEncoding.DecodeString(payload); err == nil {
		return raw, nil
	}
	if raw, err := base64.RawStdEncoding.DecodeString(payload); err == nil {
		return raw, nil
	}
	// Some clients emit URL-safe base64; accept it and let the caller re-encode
	// to standard base64 when the payload has to be rewritten.
	if raw, err := base64.URLEncoding.DecodeString(payload); err == nil {
		return raw, nil
	}
	if raw, err := base64.RawURLEncoding.DecodeString(payload); err == nil {
		return raw, nil
	}
	return nil, errors.New("invalid base64 payload")
}

// isPlainBase64Payload reports whether the payload is bare standard base64, so
// the prefix sniff can run without stripping or re-encoding anything.
func isPlainBase64Payload(data string) bool {
	if len(data) < 4 {
		return false
	}
	if len(data) >= 5 && strings.EqualFold(data[:5], "data:") {
		return false
	}
	return !strings.ContainsAny(data, "\r\n\t \v\f-_")
}

// sniffBase64ImageMediaType decodes only the leading base64 characters, which
// is enough to read the magic bytes without touching the whole payload.
func sniffBase64ImageMediaType(base64Data string) string {
	prefix := base64Data
	if len(prefix) > base64SniffPrefixSize {
		prefix = prefix[:base64SniffPrefixSize]
	}
	prefix = prefix[:len(prefix)-len(prefix)%4]
	if prefix == "" {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(prefix)
	if err != nil {
		return ""
	}
	return SniffImageMediaType(raw)
}

func normalizeMimeType(mimeType string) string {
	normalized := strings.ToLower(strings.TrimSpace(mimeType))
	if strings.HasPrefix(normalized, "data:") {
		normalized = normalized[len("data:"):]
	}
	if separator := strings.IndexByte(normalized, ';'); separator >= 0 {
		normalized = strings.TrimSpace(normalized[:separator])
	}
	if comma := strings.IndexByte(normalized, ','); comma >= 0 {
		normalized = strings.TrimSpace(normalized[:comma])
	}
	return normalized
}

func isSVGImage(raw []byte) bool {
	trimmed := bytes.TrimLeft(raw, " \t\r\n")
	if len(trimmed) < 4 {
		return false
	}
	if !bytes.HasPrefix(trimmed, []byte("<svg")) && !bytes.HasPrefix(trimmed, []byte("<?xm")) {
		return false
	}
	return bytes.Contains(raw, []byte("<svg"))
}

func describeImageFormat(detected, declared string) string {
	format := detected
	if format == "" {
		format = declared
	}
	format = strings.TrimSpace(format)
	if format == "" {
		return "unknown"
	}
	return sanitizeLogValue(format)
}

// sanitizeLogValue keeps client-controlled values (media types, format names)
// from injecting newlines into logs or blowing up the placeholder text.
func sanitizeLogValue(value string) string {
	var builder strings.Builder
	for _, character := range value {
		if character < 0x20 || character == 0x7F {
			builder.WriteByte('?')
			continue
		}
		builder.WriteRune(character)
	}
	sanitized := builder.String()
	const maxLength = 64
	if len(sanitized) > maxLength {
		sanitized = sanitized[:maxLength] + "..."
	}
	return sanitized
}

func interfaceString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		return ""
	}
}
