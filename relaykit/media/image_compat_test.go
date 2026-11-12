package media

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
)

func testImageRGBA() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(0, 0, color.RGBA{R: 200, G: 10, B: 30, A: 255})
	img.Set(1, 0, color.RGBA{R: 10, G: 200, B: 30, A: 255})
	img.Set(0, 1, color.RGBA{R: 10, G: 30, B: 200, A: 128})
	return img
}

func encodeTestImage(t *testing.T, format string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	var err error
	switch format {
	case "jpeg":
		err = jpeg.Encode(&buffer, testImageRGBA(), nil)
	case "png":
		err = png.Encode(&buffer, testImageRGBA())
	case "gif":
		err = gif.Encode(&buffer, testImageRGBA(), nil)
	case "bmp":
		err = bmp.Encode(&buffer, testImageRGBA())
	case "tiff":
		err = tiff.Encode(&buffer, testImageRGBA(), nil)
	default:
		t.Fatalf("unsupported test image format %q", format)
	}
	require.NoError(t, err)
	return buffer.Bytes()
}

func TestCanonicalClaudeImageMediaType(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   string
		wantOK bool
	}{
		{name: "jpeg", input: "image/jpeg", want: MediaTypeJPEG, wantOK: true},
		{name: "jpeg alias", input: "image/jpg", want: MediaTypeJPEG, wantOK: true},
		{name: "jpeg pjpeg alias", input: "image/pjpeg", want: MediaTypeJPEG, wantOK: true},
		{name: "uppercase and parameters", input: "IMAGE/JPEG; charset=binary", want: MediaTypeJPEG, wantOK: true},
		{name: "data uri prefix", input: "data:image/png;base64", want: MediaTypePNG, wantOK: true},
		{name: "png alias", input: "image/x-png", want: MediaTypePNG, wantOK: true},
		{name: "gif", input: "image/gif", want: MediaTypeGIF, wantOK: true},
		{name: "webp", input: "image/webp", want: MediaTypeWebP, wantOK: true},
		{name: "heic", input: "image/heic", wantOK: false},
		{name: "avif", input: "image/avif", wantOK: false},
		{name: "bmp", input: "image/bmp", wantOK: false},
		{name: "octet stream", input: "application/octet-stream", wantOK: false},
		{name: "empty", input: "  ", wantOK: false},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got, ok := CanonicalClaudeImageMediaType(testCase.input)
			assert.Equal(t, testCase.wantOK, ok)
			assert.Equal(t, testCase.want, got)
		})
	}
}

func TestSniffImageMediaType(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
		want string
	}{
		{name: "jpeg", raw: encodeTestImage(t, "jpeg"), want: MediaTypeJPEG},
		{name: "png", raw: encodeTestImage(t, "png"), want: MediaTypePNG},
		{name: "gif", raw: encodeTestImage(t, "gif"), want: MediaTypeGIF},
		{name: "bmp", raw: encodeTestImage(t, "bmp"), want: mediaTypeBMP},
		{name: "tiff", raw: encodeTestImage(t, "tiff"), want: mediaTypeTIFF},
		{name: "webp", raw: []byte("RIFF\x24\x00\x00\x00WEBPVP8 "), want: MediaTypeWebP},
		{name: "avif", raw: append([]byte{0x00, 0x00, 0x00, 0x1C}, []byte("ftypavif")...), want: mediaTypeAVIF},
		{name: "heic", raw: append([]byte{0x00, 0x00, 0x00, 0x1C}, []byte("ftypheic")...), want: mediaTypeHEIC},
		{name: "svg", raw: []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"></svg>`), want: mediaTypeSVG},
		{name: "unknown", raw: []byte("plain text payload"), want: ""},
		{name: "empty", raw: nil, want: ""},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.want, SniffImageMediaType(testCase.raw))
		})
	}
}

func TestPrepareClaudeImageData(t *testing.T) {
	jpegBytes := encodeTestImage(t, "jpeg")
	pngBytes := encodeTestImage(t, "png")
	bmpBytes := encodeTestImage(t, "bmp")
	tiffBytes := encodeTestImage(t, "tiff")
	avifBytes := append([]byte{0x00, 0x00, 0x00, 0x1C}, []byte("ftypavif")...)
	hugeBMPBytes := hugeBMPHeader(100000, 100000)

	tests := []struct {
		name          string
		raw           []byte
		declared      string
		wantMediaType string
		wantDropped   bool
		wantNote      string
		wantSameBytes bool
	}{
		{
			name:          "jpeg alias",
			raw:           jpegBytes,
			declared:      "image/jpg",
			wantMediaType: MediaTypeJPEG,
			wantSameBytes: true,
		},
		{
			name:          "supported declaration is kept",
			raw:           pngBytes,
			declared:      "image/png",
			wantMediaType: MediaTypePNG,
			wantSameBytes: true,
		},
		{
			name:          "unsupported declaration but supported payload",
			raw:           pngBytes,
			declared:      "image/avif",
			wantMediaType: MediaTypePNG,
			wantSameBytes: true,
		},
		{
			name:          "mismatched declaration is repaired",
			raw:           pngBytes,
			declared:      "image/jpeg",
			wantMediaType: MediaTypePNG,
			wantNote:      "mismatch",
			wantSameBytes: true,
		},
		{
			name:          "missing declaration",
			raw:           pngBytes,
			declared:      "",
			wantMediaType: MediaTypePNG,
			wantSameBytes: true,
		},
		{
			name:          "bmp is transcoded",
			raw:           bmpBytes,
			declared:      "image/bmp",
			wantMediaType: MediaTypePNG,
			wantNote:      "transcoded",
		},
		{
			name:          "tiff is transcoded",
			raw:           tiffBytes,
			declared:      "image/tiff",
			wantMediaType: MediaTypePNG,
			wantNote:      "transcoded",
		},
		{
			name:          "avif is dropped",
			raw:           avifBytes,
			declared:      "image/avif",
			wantMediaType: "image/avif",
			wantDropped:   true,
			wantNote:      "omitted",
			wantSameBytes: true,
		},
		{
			name:          "oversized bmp header is dropped instead of decoded",
			raw:           hugeBMPBytes,
			declared:      "image/bmp",
			wantMediaType: "image/bmp",
			wantDropped:   true,
			wantNote:      "too large",
			wantSameBytes: true,
		},
		{
			name:          "unknown binary is dropped",
			raw:           []byte("not an image"),
			declared:      "application/octet-stream",
			wantMediaType: "application/octet-stream",
			wantDropped:   true,
			wantNote:      "omitted",
			wantSameBytes: true,
		},
		{
			name:          "empty payload is dropped",
			raw:           nil,
			declared:      "image/png",
			wantMediaType: "image/png",
			wantDropped:   true,
			wantNote:      "omitted",
			wantSameBytes: true,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			data, mediaType, dropped, note := PrepareClaudeImageData(testCase.raw, testCase.declared)
			assert.Equal(t, testCase.wantMediaType, mediaType)
			assert.Equal(t, testCase.wantDropped, dropped)
			assert.Contains(t, note, testCase.wantNote)
			if testCase.wantSameBytes {
				assert.Equal(t, testCase.raw, data)
			}
			if !testCase.wantSameBytes && !testCase.wantDropped {
				assert.Equal(t, MediaTypePNG, SniffImageMediaType(data))
				_, format, err := image.DecodeConfig(bytes.NewReader(data))
				require.NoError(t, err)
				assert.Equal(t, "png", format)
			}
		})
	}
}

func TestPrepareClaudeImageBase64(t *testing.T) {
	jpegBase64 := base64.StdEncoding.EncodeToString(encodeTestImage(t, "jpeg"))
	bmpBase64 := base64.StdEncoding.EncodeToString(encodeTestImage(t, "bmp"))
	avifBase64 := base64.StdEncoding.EncodeToString(append([]byte{0x00, 0x00, 0x00, 0x1C}, []byte("ftypavif")...))

	t.Run("alias rewrite keeps payload string", func(t *testing.T) {
		data, mediaType, dropped, note := PrepareClaudeImageBase64(jpegBase64, "image/jpg")
		assert.False(t, dropped)
		assert.Empty(t, note)
		assert.Equal(t, MediaTypeJPEG, mediaType)
		assert.Equal(t, jpegBase64, data)
	})

	t.Run("transcode returns png base64", func(t *testing.T) {
		data, mediaType, dropped, note := PrepareClaudeImageBase64(bmpBase64, "image/bmp")
		assert.False(t, dropped)
		assert.Contains(t, note, "transcoded")
		assert.Equal(t, MediaTypePNG, mediaType)
		raw, err := base64.StdEncoding.DecodeString(data)
		require.NoError(t, err)
		assert.Equal(t, MediaTypePNG, SniffImageMediaType(raw))
	})

	t.Run("unsupported payload is dropped", func(t *testing.T) {
		_, _, dropped, note := PrepareClaudeImageBase64(avifBase64, "image/avif")
		assert.True(t, dropped)
		assert.Contains(t, note, "omitted")
	})

	t.Run("invalid base64 with accepted declaration is passed through", func(t *testing.T) {
		// Invalid base64 is the upstream's business; we do not pay a full decode
		// just to detect it when the declaration is already acceptable.
		data, mediaType, dropped, note := PrepareClaudeImageBase64("!!!notbase64!!!", "image/png")
		assert.False(t, dropped)
		assert.Empty(t, note)
		assert.Equal(t, MediaTypePNG, mediaType)
		assert.Equal(t, "!!!notbase64!!!", data)
	})

	t.Run("invalid base64 with unsupported declaration is dropped", func(t *testing.T) {
		_, _, dropped, note := PrepareClaudeImageBase64("!!!notbase64!!!", "image/avif")
		assert.True(t, dropped)
		assert.Contains(t, note, "base64")
	})

	t.Run("data uri input is canonicalized", func(t *testing.T) {
		data, mediaType, dropped, _ := PrepareClaudeImageBase64("data:image/jpg;base64,"+jpegBase64, "image/jpg")
		assert.False(t, dropped)
		assert.Equal(t, MediaTypeJPEG, mediaType)
		assert.Equal(t, jpegBase64, data)
	})
}

func TestNormalizeClaudeRequestImagesTypedBlocks(t *testing.T) {
	jpegBase64 := base64.StdEncoding.EncodeToString(encodeTestImage(t, "jpeg"))

	request := &dto.ClaudeRequest{
		Messages: []dto.ClaudeMessage{
			{
				Role: "user",
				Content: []dto.ClaudeMediaMessage{
					{Type: "text", Text: stringPointer("describe this")},
					{Type: "image", Source: &dto.ClaudeMessageSource{Type: "base64", MediaType: "image/jpg", Data: jpegBase64}},
				},
			},
		},
	}

	notes := NormalizeClaudeRequestImages(request)
	assert.Empty(t, notes)

	blocks, ok := request.Messages[0].Content.([]dto.ClaudeMediaMessage)
	require.True(t, ok)
	require.Len(t, blocks, 2)
	assert.Equal(t, MediaTypeJPEG, blocks[1].Source.MediaType)
	assert.Equal(t, jpegBase64, blocks[1].Source.Data)

	// Running the pass again must be a no-op.
	assert.Empty(t, NormalizeClaudeRequestImages(request))
}

func TestNormalizeClaudeRequestImagesDropsUnsupportedTypedBlock(t *testing.T) {
	avifBase64 := base64.StdEncoding.EncodeToString(append([]byte{0x00, 0x00, 0x00, 0x1C}, []byte("ftypavif")...))
	request := &dto.ClaudeRequest{
		Messages: []dto.ClaudeMessage{
			{
				Role: "user",
				Content: []dto.ClaudeMediaMessage{
					{Type: "image", Source: &dto.ClaudeMessageSource{Type: "base64", MediaType: "image/avif", Data: avifBase64}},
				},
			},
		},
	}

	notes := NormalizeClaudeRequestImages(request)
	require.Len(t, notes, 1)
	assert.Contains(t, notes[0], "omitted")

	blocks, ok := request.Messages[0].Content.([]dto.ClaudeMediaMessage)
	require.True(t, ok)
	require.Len(t, blocks, 1)
	assert.Equal(t, "text", blocks[0].Type)
	require.NotNil(t, blocks[0].Text)
	assert.Contains(t, *blocks[0].Text, "image omitted")
}

func TestNormalizeClaudeRequestImagesRawBlocksKeepUnknownFields(t *testing.T) {
	jpegBase64 := base64.StdEncoding.EncodeToString(encodeTestImage(t, "jpeg"))
	cacheControl := map[string]any{"type": "ephemeral"}
	imageBlock := map[string]any{
		"type": "image",
		"source": map[string]any{
			"type":       "base64",
			"media_type": "image/jpg",
			"data":       jpegBase64,
		},
		"cache_control": cacheControl,
	}
	request := &dto.ClaudeRequest{
		Messages: []dto.ClaudeMessage{
			{
				Role: "user",
				Content: []any{
					map[string]any{"type": "text", "text": "look"},
					imageBlock,
				},
			},
		},
	}

	notes := NormalizeClaudeRequestImages(request)
	assert.Empty(t, notes)

	content, ok := request.Messages[0].Content.([]any)
	require.True(t, ok)
	normalizedBlock, ok := content[1].(map[string]any)
	require.True(t, ok)
	source := normalizedBlock["source"].(map[string]any)
	assert.Equal(t, MediaTypeJPEG, source["media_type"])
	assert.Equal(t, jpegBase64, source["data"])
	assert.Equal(t, cacheControl, normalizedBlock["cache_control"])
}

func TestNormalizeClaudeRequestImagesNormalizesNestedToolResult(t *testing.T) {
	jpegBase64 := base64.StdEncoding.EncodeToString(encodeTestImage(t, "jpeg"))
	request := &dto.ClaudeRequest{
		Messages: []dto.ClaudeMessage{
			{
				Role: "user",
				Content: []any{
					map[string]any{
						"type":        "tool_result",
						"tool_use_id": "toolu_1",
						"content": []any{
							map[string]any{
								"type": "image",
								"source": map[string]any{
									"type":       "base64",
									"media_type": "image/jpg",
									"data":       jpegBase64,
								},
							},
						},
					},
				},
			},
		},
	}

	assert.Empty(t, NormalizeClaudeRequestImages(request))

	content := request.Messages[0].Content.([]any)
	toolResult := content[0].(map[string]any)
	nested := toolResult["content"].([]any)
	source := nested[0].(map[string]any)["source"].(map[string]any)
	assert.Equal(t, MediaTypeJPEG, source["media_type"])
}

func TestNormalizeClaudeRequestImagesLeavesURLSource(t *testing.T) {
	request := &dto.ClaudeRequest{
		Messages: []dto.ClaudeMessage{
			{
				Role: "user",
				Content: []any{
					map[string]any{
						"type": "image",
						"source": map[string]any{
							"type": "url",
							"url":  "https://example.com/picture.avif",
						},
					},
				},
			},
		},
	}

	assert.Empty(t, NormalizeClaudeRequestImages(request))
	content := request.Messages[0].Content.([]any)
	block := content[0].(map[string]any)
	assert.Equal(t, "url", block["source"].(map[string]any)["type"])
}

func TestNormalizeClaudeRequestImagesLeavesNonImagePayloads(t *testing.T) {
	request := &dto.ClaudeRequest{
		Messages: []dto.ClaudeMessage{
			{
				Role: "user",
				Content: []any{
					map[string]any{
						"type": "image",
						"source": map[string]any{
							"type":       "base64",
							"media_type": "audio/mpeg",
							"data":       "SUQzBAAAAAAAI1RTU0UAAAAPAAADTGF2ZjU4",
						},
					},
				},
			},
		},
	}

	assert.Empty(t, NormalizeClaudeRequestImages(request))
	content := request.Messages[0].Content.([]any)
	source := content[0].(map[string]any)["source"].(map[string]any)
	assert.Equal(t, "audio/mpeg", source["media_type"])
}

func TestNormalizeClaudeRequestImagesIgnoresStringAndNil(t *testing.T) {
	assert.Nil(t, NormalizeClaudeRequestImages(nil))
	request := &dto.ClaudeRequest{Messages: []dto.ClaudeMessage{{Role: "user", Content: "hello"}}}
	assert.Empty(t, NormalizeClaudeRequestImages(request))
	assert.Equal(t, "hello", request.Messages[0].Content)
}

func hugeBMPHeader(width, height int32) []byte {
	raw := make([]byte, 54)
	copy(raw[0:2], "BM")
	binary.LittleEndian.PutUint32(raw[2:6], 54)   // file size
	binary.LittleEndian.PutUint32(raw[10:14], 54) // pixel data offset
	binary.LittleEndian.PutUint32(raw[14:18], 40) // DIB header size
	binary.LittleEndian.PutUint32(raw[18:22], uint32(width))
	binary.LittleEndian.PutUint32(raw[22:26], uint32(height))
	binary.LittleEndian.PutUint16(raw[26:28], 1)  // planes
	binary.LittleEndian.PutUint16(raw[28:30], 24) // bits per pixel
	return raw
}

func TestPrepareClaudeImageDataRejectsOversizedTIFF(t *testing.T) {
	// A tiny TIFF header whose IFD claims an enormous image must not be decoded.
	raw := make([]byte, 8)
	copy(raw, []byte{'I', 'I', 0x2A, 0x00})
	_, _, dropped, _ := PrepareClaudeImageData(raw, "image/tiff")
	assert.True(t, dropped)
}

func TestNormalizeClaudeRequestImagesDropsEmptyPayload(t *testing.T) {
	request := &dto.ClaudeRequest{
		Messages: []dto.ClaudeMessage{
			{
				Role: "user",
				Content: []any{
					map[string]any{
						"type": "image",
						"source": map[string]any{
							"type":       "base64",
							"media_type": "image/png",
							"data":       "",
						},
					},
				},
			},
		},
	}

	notes := NormalizeClaudeRequestImages(request)
	require.Len(t, notes, 1)
	assert.Contains(t, notes[0], "empty image payload")

	content := request.Messages[0].Content.([]any)
	block := content[0].(map[string]any)
	assert.Equal(t, "text", block["type"])
	assert.Contains(t, block["text"], "image omitted")
	assert.NotContains(t, block, "source")
}

func TestNormalizeClaudeRequestImagesPreservesCacheControlOnDrop(t *testing.T) {
	avifBase64 := base64.StdEncoding.EncodeToString(append([]byte{0x00, 0x00, 0x00, 0x1C}, []byte("ftypavif")...))
	request := &dto.ClaudeRequest{
		Messages: []dto.ClaudeMessage{
			{
				Role: "user",
				Content: []any{
					map[string]any{
						"type": "image",
						"source": map[string]any{
							"type":       "base64",
							"media_type": "image/avif",
							"data":       avifBase64,
						},
						"cache_control": map[string]any{"type": "ephemeral"},
					},
				},
			},
		},
	}

	NormalizeClaudeRequestImages(request)

	block := request.Messages[0].Content.([]any)[0].(map[string]any)
	assert.Equal(t, "text", block["type"])
	assert.Contains(t, block["text"], "image omitted")
	assert.Equal(t, map[string]any{"type": "ephemeral"}, block["cache_control"])
}

func TestNormalizeClaudeRequestImagesLeavesSupportedFormatsUntouched(t *testing.T) {
	gifImage := encodeTestImage(t, "gif")
	gifBase64 := base64.StdEncoding.EncodeToString(gifImage)
	webpBytes := []byte("RIFF\x24\x00\x00\x00WEBPVP8 ")
	webpBase64 := base64.StdEncoding.EncodeToString(webpBytes)

	data, mediaType, dropped, note := PrepareClaudeImageBase64(gifBase64, "image/gif")
	assert.False(t, dropped)
	assert.Empty(t, note)
	assert.Equal(t, MediaTypeGIF, mediaType)
	assert.Equal(t, gifBase64, data)

	data, mediaType, dropped, note = PrepareClaudeImageBase64(webpBase64, "image/webp")
	assert.False(t, dropped)
	assert.Empty(t, note)
	assert.Equal(t, MediaTypeWebP, mediaType)
	assert.Equal(t, webpBase64, data)
}

func TestNormalizeClaudeRequestImagesAcceptsBinaryOctetStreamDeclaration(t *testing.T) {
	pngBase64 := base64.StdEncoding.EncodeToString(encodeTestImage(t, "png"))
	request := &dto.ClaudeRequest{
		Messages: []dto.ClaudeMessage{
			{
				Role: "user",
				Content: []any{
					map[string]any{
						"type": "image",
						"source": map[string]any{
							"type":       "base64",
							"media_type": "binary/octet-stream",
							"data":       pngBase64,
						},
					},
				},
			},
		},
	}

	assert.Empty(t, NormalizeClaudeRequestImages(request))

	block := request.Messages[0].Content.([]any)[0].(map[string]any)
	assert.Equal(t, MediaTypePNG, block["source"].(map[string]any)["media_type"])
}

func TestNormalizeClaudeRequestImagesNormalizesSystemContent(t *testing.T) {
	jpegBase64 := base64.StdEncoding.EncodeToString(encodeTestImage(t, "jpeg"))
	request := &dto.ClaudeRequest{
		System: []any{
			map[string]any{
				"type": "image",
				"source": map[string]any{
					"type":       "base64",
					"media_type": "image/jpg",
					"data":       jpegBase64,
				},
			},
		},
	}

	assert.Empty(t, NormalizeClaudeRequestImages(request))

	system := request.System.([]any)
	block := system[0].(map[string]any)
	assert.Equal(t, MediaTypeJPEG, block["source"].(map[string]any)["media_type"])
}

func TestNormalizeClaudeRequestImagesCapsNotes(t *testing.T) {
	avifBase64 := base64.StdEncoding.EncodeToString(append([]byte{0x00, 0x00, 0x00, 0x1C}, []byte("ftypavif")...))
	blocks := make([]any, 0, maxImageCompatNotes+2)
	for index := 0; index < maxImageCompatNotes+2; index++ {
		blocks = append(blocks, map[string]any{
			"type": "image",
			"source": map[string]any{
				"type":       "base64",
				"media_type": "image/avif",
				"data":       avifBase64,
			},
		})
	}
	request := &dto.ClaudeRequest{
		Messages: []dto.ClaudeMessage{{Role: "user", Content: blocks}},
	}

	notes := NormalizeClaudeRequestImages(request)
	require.Len(t, notes, maxImageCompatNotes+1)
	for _, block := range request.Messages[0].Content.([]any) {
		assert.Equal(t, "text", block.(map[string]any)["type"])
	}
}

func TestPrepareClaudeImageBase64RewritesURLSafeBase64(t *testing.T) {
	raw := encodeTestImage(t, "png")
	urlSafe := base64.RawURLEncoding.EncodeToString(raw)
	// Force URL-safe characters where possible; the round trip must still be
	// canonical standard base64 so upstream can decode it.
	data, mediaType, dropped, _ := PrepareClaudeImageBase64(urlSafe, "image/png")
	assert.False(t, dropped)
	assert.Equal(t, MediaTypePNG, mediaType)
	decoded, err := base64.StdEncoding.DecodeString(data)
	require.NoError(t, err)
	assert.Equal(t, raw, decoded)
}

func TestSanitizeLogValue(t *testing.T) {
	assert.Equal(t, "image/png", sanitizeLogValue("image/png"))
	assert.Equal(t, "image?png", sanitizeLogValue("image\npng"))
	long := strings.Repeat("a", 100)
	sanitized := sanitizeLogValue(long)
	assert.Len(t, sanitized, 67)
	assert.True(t, strings.HasSuffix(sanitized, "..."))
}

func stringPointer(value string) *string {
	return &value
}
