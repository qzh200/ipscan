// Package flags embeds the small country flags the node list draws in front of
// each name. The artwork comes from flagcdn.com, built from the MIT-licensed
// lipis/flag-icons set, and is embedded so the app needs no network for it.
package flags

import (
	"embed"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"
)

//go:embed png/*.png
var files embed.FS

var (
	mu    sync.Mutex
	cache = map[string]ui.ImageSource{}
)

// Image returns the flag of an ISO 3166-1 alpha-2 code, or nil when the code
// has no embedded picture. Decoded pictures are kept, so redrawing the list
// costs no decoding.
func Image(code string) ui.ImageSource {
	code = strings.ToLower(strings.TrimSpace(code))
	if code == "" {
		return nil
	}
	mu.Lock()
	if src, ok := cache[code]; ok {
		mu.Unlock()
		return src
	}
	mu.Unlock()

	var src ui.ImageSource
	if data, err := files.ReadFile("png/" + code + ".png"); err == nil {
		if bmp, err := ui.DecodeBitmap(data); err == nil {
			src = bmp
		}
	}

	mu.Lock()
	cache[code] = src
	mu.Unlock()
	return src
}
