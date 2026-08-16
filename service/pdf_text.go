package service

import (
	"bytes"
	"encoding/base64"
	"sort"
	"strings"
	"unicode/utf8"

	"rsc.io/pdf"
)

func ExtractSimplePDFText(base64Data string) string {
	data, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		data, err = base64.RawStdEncoding.DecodeString(base64Data)
	}
	if err != nil || !strings.HasPrefix(string(data), "%PDF-") {
		return ""
	}
	if text := extractStructuredPDFText(data); text != "" {
		return text
	}
	return extractPDFLiteralText(data)
}

func extractStructuredPDFText(data []byte) (text string) {
	defer func() {
		if recover() != nil {
			text = ""
		}
	}()
	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return ""
	}
	var out strings.Builder
	for pageIndex := 1; pageIndex <= reader.NumPage() && out.Len() < 20000; pageIndex++ {
		items := reader.Page(pageIndex).Content().Text
		sort.Sort(pdf.TextVertical(items))
		var previous *pdf.Text
		for itemIndex := range items {
			item := &items[itemIndex]
			if item.S == "" || !utf8.ValidString(item.S) {
				continue
			}
			if previous != nil {
				fontSize := maxPDFTextSize(item.FontSize, previous.FontSize)
				if absPDFTextPosition(item.Y-previous.Y) > fontSize*0.5 {
					out.WriteByte('\n')
				} else if item.X-(previous.X+previous.W) > fontSize*0.2 {
					out.WriteByte(' ')
				}
			}
			out.WriteString(item.S)
			previous = item
			if out.Len() >= 20000 {
				break
			}
		}
		if out.Len() > 0 && pageIndex < reader.NumPage() {
			out.WriteByte('\n')
		}
	}
	return trimPDFText(out.String())
}

func extractPDFLiteralText(data []byte) string {
	var out strings.Builder
	for i := 0; i < len(data); i++ {
		if data[i] != '(' {
			continue
		}
		text, next := readPDFLiteralString(data, i+1)
		if text == "" {
			i = next
			continue
		}
		if out.Len() > 0 {
			out.WriteByte(' ')
		}
		out.WriteString(text)
		if out.Len() >= 20000 {
			break
		}
		i = next
	}
	return trimPDFText(out.String())
}

func trimPDFText(text string) string {
	text = strings.TrimSpace(text)
	if len(text) > 20000 {
		return text[:20000]
	}
	return text
}

func absPDFTextPosition(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}

func maxPDFTextSize(first, second float64) float64 {
	if first > second {
		return first
	}
	return second
}

func readPDFLiteralString(data []byte, start int) (string, int) {
	var out strings.Builder
	escaped := false
	nesting := 1
	for i := start; i < len(data); i++ {
		ch := data[i]
		if escaped {
			switch ch {
			case 'n':
				out.WriteByte('\n')
			case 'r':
				out.WriteByte('\r')
			case 't':
				out.WriteByte('\t')
			case 'b':
				out.WriteByte('\b')
			case 'f':
				out.WriteByte('\f')
			case '\n':
			case '\r':
				if i+1 < len(data) && data[i+1] == '\n' {
					i++
				}
			default:
				if ch >= '0' && ch <= '7' {
					value := int(ch - '0')
					for count := 0; count < 2 && i+1 < len(data) && data[i+1] >= '0' && data[i+1] <= '7'; count++ {
						i++
						value = value*8 + int(data[i]-'0')
					}
					out.WriteByte(byte(value))
				} else {
					out.WriteByte(ch)
				}
			}
			escaped = false
			continue
		}
		switch ch {
		case '\\':
			escaped = true
		case '(':
			nesting++
			out.WriteByte(ch)
		case ')':
			nesting--
			if nesting == 0 {
				return strings.TrimSpace(out.String()), i
			}
			out.WriteByte(ch)
		default:
			if ch == 0 {
				continue
			}
			out.WriteByte(ch)
		}
	}
	return strings.TrimSpace(out.String()), len(data)
}
