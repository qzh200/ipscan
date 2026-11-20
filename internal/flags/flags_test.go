package flags

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestImage(t *testing.T) {
	for _, code := range []string{"am", "AU", "hk", "gb"} {
		src := Image(code)
		if src == nil {
			t.Fatalf("Image(%q) = nil", code)
		}
		if w, h := ui.ImageSize(src); w <= 0 || h <= 0 {
			t.Fatalf("Image(%q) has size %v×%v", code, w, h)
		}
	}
	if got := Image("zz"); got != nil {
		t.Fatalf("Image(zz) = %v, want nil", got)
	}
	if got := Image(""); got != nil {
		t.Fatalf("Image(\"\") = %v, want nil", got)
	}
	if Image("am") != Image("AM") {
		t.Fatal("the cache should answer both spellings with the same picture")
	}
}
