package gemini

import (
	"bytes"
	"encoding/base64"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"math"

	"golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

// Gemini inline limits are counted in base64 characters, so image shrinking has
// to hit a base64 budget, not a raw-byte budget.
var (
	// Long-edge candidates tried in order. Gemini downsamples images to roughly
	// this range anyway, so keeping the longest edge at 1536 is enough.
	geminiInlineShrinkEdges     = []int{1536, 1024, 768, 512, 384}
	geminiInlineShrinkQualities = []int{85, 75, 65}
)

// shrinkGeminiInlineBase64 decodes an oversized inline image and re-encodes it
// as JPEG until its base64 representation fits maxBase64Bytes. ok=false means
// the format is not decodable with pure Go (HEIC/AVIF/...) or cannot be shrunk
// far enough; the caller then keeps the original fileData URL.
func shrinkGeminiInlineBase64(base64Data string, maxBase64Bytes int64) (string, bool) {
	raw, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return "", false
	}
	decoded, err := decodeGeminiInlineImage(raw)
	if err != nil {
		return "", false
	}
	for _, longEdge := range geminiInlineShrinkEdges {
		scaled := scaleImageToLongEdge(decoded, longEdge)
		for _, quality := range geminiInlineShrinkQualities {
			var buffer bytes.Buffer
			if err := jpeg.Encode(&buffer, scaled, &jpeg.Options{Quality: quality}); err != nil {
				return "", false
			}
			if int64(base64.StdEncoding.EncodedLen(buffer.Len())) > maxBase64Bytes {
				continue
			}
			return base64.StdEncoding.EncodeToString(buffer.Bytes()), true
		}
	}
	return "", false
}

func decodeGeminiInlineImage(raw []byte) (image.Image, error) {
	if decoded, _, err := image.Decode(bytes.NewReader(raw)); err == nil {
		return decoded, nil
	}
	return webp.Decode(bytes.NewReader(raw))
}

// scaleImageToLongEdge scales the image down to longEdge, never up, and
// flattens transparency onto white so JPEG encoding loses no content.
func scaleImageToLongEdge(source image.Image, longEdge int) image.Image {
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 {
		return source
	}
	scale := 1.0
	if width >= height && width > longEdge {
		scale = float64(longEdge) / float64(width)
	} else if height > width && height > longEdge {
		scale = float64(longEdge) / float64(height)
	}
	targetWidth := int(math.Round(float64(width) * scale))
	targetHeight := int(math.Round(float64(height) * scale))
	if targetWidth < 1 {
		targetWidth = 1
	}
	if targetHeight < 1 {
		targetHeight = 1
	}
	scaled := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))
	draw.Draw(scaled, scaled.Bounds(), image.White, image.Point{}, draw.Src)
	draw.CatmullRom.Scale(scaled, scaled.Bounds(), source, bounds, draw.Over, nil)
	return scaled
}
