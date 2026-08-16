package service

import (
	"encoding/base64"
	"strings"
)

func ExtractSimplePDFText(base64Data string) string {
	data, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil || !strings.HasPrefix(string(data), "%PDF-") {
		return ""
	}
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
	text := strings.TrimSpace(out.String())
	if len(text) > 20000 {
		return text[:20000]
	}
	return text
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
