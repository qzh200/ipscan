package subscription

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// str returns the first present key as a trimmed string.
func str(m map[string]any, keys ...string) string {
	for _, k := range keys {
		v, ok := m[k]
		if !ok || v == nil {
			continue
		}
		switch t := v.(type) {
		case string:
			if s := strings.TrimSpace(t); s != "" {
				return s
			}
		case json.Number:
			return t.String()
		case float64:
			return strconv.FormatFloat(t, 'f', -1, 64)
		case int:
			return strconv.Itoa(t)
		case bool:
			if t {
				return "true"
			}
			return "false"
		case []any:
			parts := make([]string, 0, len(t))
			for _, e := range t {
				parts = append(parts, fmt.Sprint(e))
			}
			if len(parts) > 0 {
				return strings.Join(parts, ",")
			}
		}
	}
	return ""
}

// num returns the first present key as an int, or 0.
func num(m map[string]any, keys ...string) int {
	s := str(m, keys...)
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		f, ferr := strconv.ParseFloat(s, 64)
		if ferr != nil {
			return 0
		}
		n = int(f)
	}
	return n
}

// boolean reads a key that may be a bool, "true"/"1" string or a number.
func boolean(m map[string]any, keys ...string) bool {
	for _, k := range keys {
		v, ok := m[k]
		if !ok {
			continue
		}
		switch t := v.(type) {
		case bool:
			return t
		case json.Number:
			return t.String() != "0" && t.String() != ""
		case float64:
			return t != 0
		case string:
			if truthy(t) {
				return true
			}
		}
	}
	return false
}

func truthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// obj returns the first present key that is a mapping.
func obj(m map[string]any, keys ...string) map[string]any {
	for _, k := range keys {
		switch t := m[k].(type) {
		case map[string]any:
			return t
		case map[any]any:
			out := make(map[string]any, len(t))
			for kk, vv := range t {
				if ks, ok := kk.(string); ok {
					out[ks] = vv
				}
			}
			return out
		}
	}
	return nil
}

// list returns the first present key that is a sequence.
func list(m map[string]any, keys ...string) []any {
	for _, k := range keys {
		if l, ok := m[k].([]any); ok {
			return l
		}
	}
	return nil
}

// decodeBase64Bytes tries the four common alphabets plus URL-safe variants.
func decodeBase64Bytes(s string) ([]byte, bool) {
	s = strings.TrimSpace(s)
	encodings := []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding,
		base64.URLEncoding, base64.RawURLEncoding,
	}
	for _, enc := range encodings {
		if b, err := enc.DecodeString(s); err == nil {
			return b, true
		}
	}
	return nil, false
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
