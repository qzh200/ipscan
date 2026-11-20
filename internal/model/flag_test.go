package model

import "testing"

func TestCountryPrefix(t *testing.T) {
	cases := []struct {
		raw, code, rest string
		ok              bool
	}{
		{"amArmenia 電門 藕豆子", "AM", "Armenia 電門 藕豆子", true},
		{"auAustralia Sydney 🦘", "AU", "Australia Sydney 🦘", true},
		{"caCanada 神山 誠", "CA", "Canada 神山 誠", true},
		{"chSwitzerland 0.1x BitTorrent专用", "CH", "Switzerland 0.1x BitTorrent专用", true},
		{"gbUK London 伊地知", "GB", "UK London 伊地知", true},
		{"gb|UK London", "GB", "UK London", true},
		{"jp 東京 01", "JP", "東京 01", true},
		{"hkHong Kong ムラサメ", "HK", "Hong Kong ムラサメ", true},
		{"KR\u00a0韩国 01 [世宗电信 ISP]-test", "KR", "韩国 01 [世宗电信 ISP]-test", true},
		{"kr　韩国 02", "KR", "韩国 02", true},
		{"US |Los Angeles", "US", "Los Angeles", true},
		{"KR\u200b 韩国 03", "KR", "韩国 03", true},
		{"🇭🇰Hong Kong", "HK", "Hong Kong", true},
		{"🇭🇰 Hong Kong 02", "HK", "Hong Kong 02", true},
		// Not a country prefix.
		{"ss02 香港", "", "", false},
		{"is really slow", "", "", false},
		{"VMess 01", "", "", false},
		{"zzTest", "", "", false},
		{"日本 01", "", "", false},
		{"", "", "", false},
	}
	for _, tc := range cases {
		code, rest, ok := CountryPrefix(tc.raw)
		if ok != tc.ok || code != tc.code || (ok && rest != tc.rest) {
			t.Errorf("CountryPrefix(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tc.raw, code, rest, ok, tc.code, tc.rest, tc.ok)
		}
	}
}

func TestSortNameDropsPrefix(t *testing.T) {
	if got := SortName("hkHong Kong 01"); got != "Hong Kong 01" {
		t.Fatalf("SortName = %q", got)
	}
	if got := SortName("美国 01"); got != "美国 01" {
		t.Fatalf("SortName kept non-prefix name: %q", got)
	}
}
