package subscription

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"ipscan/internal/model"
)

// Document is the result of parsing one subscription payload.
type Document struct {
	// Format names what was detected: "URI 列表", "Clash YAML", "Xray JSON",
	// "sing-box JSON" or "base64 → …".
	Format string
	Nodes  []model.Node
	// Warnings lists entries that looked like nodes but could not be parsed,
	// so a broken entry never hides a good one.
	Warnings []string
}

// Parse detects the subscription format and parses every node in it. It
// understands a plain URI list, base64 of one, Clash/Mihomo YAML, Xray config
// JSON and sing-box JSON.
func Parse(raw []byte) (*Document, error) {
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	if len(raw) >= 2 && raw[0] == 0x1f && raw[1] == 0x8b {
		zr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, fmt.Errorf("解压订阅失败: %w", err)
		}
		plain, err := io.ReadAll(io.LimitReader(zr, MaxPayload))
		if err != nil {
			return nil, fmt.Errorf("解压订阅失败: %w", err)
		}
		raw = plain
	}
	s := strings.TrimSpace(string(raw))
	if s == "" {
		return nil, errors.New("订阅内容为空")
	}
	doc, err := parseText(s, 0)
	if err != nil {
		return nil, err
	}
	if len(doc.Nodes) == 0 {
		return nil, errors.New("没有解析到任何节点，请确认这是 Xray/Clash 订阅或节点链接")
	}
	return doc, nil
}

func parseText(s string, depth int) (*Document, error) {
	if depth > 2 {
		return nil, errors.New("订阅嵌套的解码层数过多")
	}

	if strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[") {
		if doc, err := parseConfigJSON([]byte(s)); err == nil && len(doc.Nodes) > 0 {
			return doc, nil
		}
	}

	if looksLikeClash(s) {
		if doc, err := parseClash([]byte(s)); err == nil && len(doc.Nodes) > 0 {
			return doc, nil
		}
	}

	if looksLikeBase64(s) {
		if decoded, ok := decodeBase64Text(s); ok {
			if doc, err := parseText(decoded, depth+1); err == nil {
				doc.Format = "base64 → " + doc.Format
				return doc, nil
			}
		}
	}

	if doc, err := parseURIList(s); err == nil && len(doc.Nodes) > 0 {
		return doc, nil
	}

	return nil, errors.New("无法识别的订阅格式（支持 Clash YAML、Xray/sing-box JSON、节点链接列表及其 base64）")
}

func looksLikeClash(s string) bool {
	for _, key := range []string{"proxies:", "proxy-groups:", "\nrules:", "mixed-port:", "port:"} {
		if strings.Contains(s, key) {
			return true
		}
	}
	return false
}

func looksLikeBase64(s string) bool {
	if strings.Contains(s, "://") || strings.Contains(s, "proxies:") {
		return false
	}
	if len(s) < 16 {
		return false
	}
	for _, r := range s {
		if unicode.IsSpace(r) {
			continue
		}
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '+' || r == '/' || r == '-' || r == '_' || r == '=') {
			return false
		}
	}
	return true
}

// decodeBase64Text decodes any of the four common base64 alphabets and only
// accepts the result when it is valid UTF-8 that looks like a node list.
func decodeBase64Text(s string) (string, bool) {
	s = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
	encodings := []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding,
		base64.URLEncoding, base64.RawURLEncoding,
	}
	for _, enc := range encodings {
		b, err := enc.DecodeString(s)
		if err != nil || len(b) == 0 || !utf8.Valid(b) {
			continue
		}
		text := string(b)
		if strings.Contains(text, "://") || looksLikeClash(text) || strings.HasPrefix(strings.TrimSpace(text), "{") {
			return text, true
		}
	}
	return "", false
}
