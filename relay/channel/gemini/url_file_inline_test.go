package gemini

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testInlinePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 10, G: 200, B: 30, A: 255})
	var buffer bytes.Buffer
	require.NoError(t, png.Encode(&buffer, img))
	return buffer.Bytes()
}

func newGeminiURLTestRequest(fileUri string) *dto.GeminiChatRequest {
	return &dto.GeminiChatRequest{
		Contents: []dto.GeminiChatContent{
			{
				Parts: []dto.GeminiPart{
					{FileData: &dto.GeminiFileData{FileUri: fileUri}},
				},
			},
		},
	}
}

// disableFetchSSRF switches the global fetch setting off for the duration of a
// test so httptest's loopback URL can be downloaded. InitHttpClient is required
// because tests bypass the server bootstrap.
func disableFetchSSRF(t *testing.T) {
	t.Helper()
	service.InitHttpClient()
	fetchSetting := system_setting.GetFetchSetting()
	previousFetchSetting := *fetchSetting
	fetchSetting.EnableSSRFProtection = false
	previousMaxFileDownloadMB := constant.MaxFileDownloadMB
	if constant.MaxFileDownloadMB <= 0 {
		constant.MaxFileDownloadMB = 64
	}
	t.Cleanup(func() {
		*fetchSetting = previousFetchSetting
		constant.MaxFileDownloadMB = previousMaxFileDownloadMB
	})
}

func TestConvertGeminiRequestInlinesPublicImageURL(t *testing.T) {
	disableFetchSSRF(t)
	gin.SetMode(gin.TestMode)
	pngBytes := testInlinePNG(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "image/png")
		_, _ = writer.Write(pngBytes)
	}))
	t.Cleanup(server.Close)

	request := newGeminiURLTestRequest(server.URL + "/picture.png")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini:generateContent", nil)
	_, err := (&Adaptor{}).ConvertGeminiRequest(c, nil, request)

	require.NoError(t, err)
	part := request.Contents[0].Parts[0]
	require.NotNil(t, part.InlineData)
	require.Nil(t, part.FileData)
	assert.Equal(t, "image/png", part.InlineData.MimeType)
	assert.Equal(t, base64.StdEncoding.EncodeToString(pngBytes), part.InlineData.Data)
}

func TestConvertGeminiRequestKeepsGoogleHostedFileUri(t *testing.T) {
	disableFetchSSRF(t)
	gin.SetMode(gin.TestMode)
	request := newGeminiURLTestRequest("https://generativelanguage.googleapis.com/v1beta/files/abc123")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini:generateContent", nil)

	_, err := (&Adaptor{}).ConvertGeminiRequest(c, nil, request)

	require.NoError(t, err)
	part := request.Contents[0].Parts[0]
	require.Nil(t, part.InlineData)
	require.NotNil(t, part.FileData)
	assert.Equal(t, "https://generativelanguage.googleapis.com/v1beta/files/abc123", part.FileData.FileUri)
}

func TestConvertGeminiRequestKeepsFileUriWhenDownloadFails(t *testing.T) {
	disableFetchSSRF(t)
	gin.SetMode(gin.TestMode)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	request := newGeminiURLTestRequest(server.URL + "/missing.png")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini:generateContent", nil)

	_, err := (&Adaptor{}).ConvertGeminiRequest(c, nil, request)

	require.NoError(t, err)
	part := request.Contents[0].Parts[0]
	require.Nil(t, part.InlineData)
	require.NotNil(t, part.FileData)
}

func TestConvertGeminiRequestKeepsYouTubeFileUri(t *testing.T) {
	disableFetchSSRF(t)
	gin.SetMode(gin.TestMode)
	request := newGeminiURLTestRequest("https://www.youtube.com/watch?v=abc123")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini:generateContent", nil)

	_, err := (&Adaptor{}).ConvertGeminiRequest(c, nil, request)

	require.NoError(t, err)
	part := request.Contents[0].Parts[0]
	require.Nil(t, part.InlineData)
	require.NotNil(t, part.FileData)
}

func TestInlineGeminiFileURLPartsSkipsOversizedImage(t *testing.T) {
	disableFetchSSRF(t)
	gin.SetMode(gin.TestMode)
	pngBytes := testInlinePNG(t)
	encodedLen := int64(base64.StdEncoding.EncodedLen(len(pngBytes)))
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "image/png")
		_, _ = writer.Write(pngBytes)
	}))
	t.Cleanup(server.Close)

	request := newGeminiURLTestRequest(server.URL + "/picture.png")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini:generateContent", nil)

	inlineGeminiFileURLPartsWithLimits(c, request, geminiInlineLimits{
		perImageBase64Bytes:   encodedLen - 1,
		perRequestBase64Bytes: encodedLen * 2,
	})

	part := request.Contents[0].Parts[0]
	assert.Nil(t, part.InlineData)
	assert.NotNil(t, part.FileData)
}

func TestInlineGeminiFileURLPartsRespectsRequestBudget(t *testing.T) {
	disableFetchSSRF(t)
	gin.SetMode(gin.TestMode)
	pngBytes := testInlinePNG(t)
	encodedLen := int64(base64.StdEncoding.EncodedLen(len(pngBytes)))
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "image/png")
		_, _ = writer.Write(pngBytes)
	}))
	t.Cleanup(server.Close)

	fileUri := server.URL + "/picture.png"
	request := &dto.GeminiChatRequest{
		Contents: []dto.GeminiChatContent{
			{
				Parts: []dto.GeminiPart{
					{FileData: &dto.GeminiFileData{FileUri: fileUri}},
					{FileData: &dto.GeminiFileData{FileUri: fileUri}},
				},
			},
		},
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini:generateContent", nil)

	inlineGeminiFileURLPartsWithLimits(c, request, geminiInlineLimits{
		perImageBase64Bytes:   encodedLen,
		perRequestBase64Bytes: encodedLen*2 - 1,
	})

	first := request.Contents[0].Parts[0]
	second := request.Contents[0].Parts[1]
	require.NotNil(t, first.InlineData)
	assert.Nil(t, first.FileData)
	assert.Nil(t, second.InlineData)
	assert.NotNil(t, second.FileData)
}

func TestInlineGeminiFileURLPartsDeductsExistingInlineData(t *testing.T) {
	disableFetchSSRF(t)
	gin.SetMode(gin.TestMode)
	pngBytes := testInlinePNG(t)
	encodedLen := int64(base64.StdEncoding.EncodedLen(len(pngBytes)))
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "image/png")
		_, _ = writer.Write(pngBytes)
	}))
	t.Cleanup(server.Close)

	request := &dto.GeminiChatRequest{
		Contents: []dto.GeminiChatContent{
			{
				Parts: []dto.GeminiPart{
					{InlineData: &dto.GeminiInlineData{MimeType: "image/png", Data: string(make([]byte, encodedLen-1))}},
					{FileData: &dto.GeminiFileData{FileUri: server.URL + "/picture.png"}},
				},
			},
		},
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini:generateContent", nil)

	inlineGeminiFileURLPartsWithLimits(c, request, geminiInlineLimits{
		perImageBase64Bytes:   encodedLen,
		perRequestBase64Bytes: encodedLen + 1,
	})

	// Existing inline data already consumes almost the whole budget.
	assert.Nil(t, request.Contents[0].Parts[1].InlineData)
	assert.NotNil(t, request.Contents[0].Parts[1].FileData)
}

func testNoisePNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	random := rand.New(rand.NewSource(42))
	for offset := 0; offset < len(img.Pix); offset += 4 {
		img.Pix[offset] = uint8(random.Intn(256))
		img.Pix[offset+1] = uint8(random.Intn(256))
		img.Pix[offset+2] = uint8(random.Intn(256))
		img.Pix[offset+3] = 255
	}
	var buffer bytes.Buffer
	require.NoError(t, png.Encode(&buffer, img))
	return buffer.Bytes()
}

func TestShrinkGeminiInlineBase64ProducesJPEG(t *testing.T) {
	pngBytes := testNoisePNG(t, 600, 400)
	base64Data := base64.StdEncoding.EncodeToString(pngBytes)
	limit := int64(base64.StdEncoding.EncodedLen(len(pngBytes))) - 1
	if limit < 64*1024 {
		limit = 64 * 1024
	}

	shrunk, ok := shrinkGeminiInlineBase64(base64Data, limit)

	require.True(t, ok)
	require.LessOrEqual(t, int64(len(shrunk)), limit)
	decoded, err := base64.StdEncoding.DecodeString(shrunk)
	require.NoError(t, err)
	config, format, err := image.DecodeConfig(bytes.NewReader(decoded))
	require.NoError(t, err)
	assert.Equal(t, "jpeg", format)
	assert.LessOrEqual(t, config.Width, 1536)
	assert.LessOrEqual(t, config.Height, 1536)
}

func TestShrinkGeminiInlineBase64RejectsUndecodablePayload(t *testing.T) {
	shrunk, ok := shrinkGeminiInlineBase64(base64.StdEncoding.EncodeToString([]byte("not an image at all")), 1024)
	assert.False(t, ok)
	assert.Empty(t, shrunk)
}

func TestInlineGeminiFileURLPartsShrinksOversizedImage(t *testing.T) {
	disableFetchSSRF(t)
	gin.SetMode(gin.TestMode)
	pngBytes := testNoisePNG(t, 1200, 900)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "image/png")
		_, _ = writer.Write(pngBytes)
	}))
	t.Cleanup(server.Close)

	request := newGeminiURLTestRequest(server.URL + "/big.png")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini:generateContent", nil)
	limit := int64(300 * 1024)

	inlineGeminiFileURLPartsWithLimits(c, request, geminiInlineLimits{
		perImageBase64Bytes:   limit,
		perRequestBase64Bytes: limit * 2,
	})

	part := request.Contents[0].Parts[0]
	require.NotNil(t, part.InlineData, "oversized image should be shrunk and inlined")
	assert.Equal(t, "image/jpeg", part.InlineData.MimeType)
	assert.LessOrEqual(t, int64(len(part.InlineData.Data)), limit)
	assert.Nil(t, part.FileData)
}

func TestInlineGeminiFileURLPartsKeepsUndecodableImageURL(t *testing.T) {
	disableFetchSSRF(t)
	gin.SetMode(gin.TestMode)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "image/heic")
		_, _ = writer.Write([]byte("pretend heic bytes that Go cannot decode"))
	}))
	t.Cleanup(server.Close)

	request := newGeminiURLTestRequest(server.URL + "/picture.heic")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini:generateContent", nil)

	inlineGeminiFileURLPartsWithLimits(c, request, geminiInlineLimits{
		perImageBase64Bytes:   16,
		perRequestBase64Bytes: 32,
	})

	part := request.Contents[0].Parts[0]
	assert.Nil(t, part.InlineData)
	assert.NotNil(t, part.FileData)
}
