// This file recognises the country prefix subscription generators put in
// front of a node name.
package model

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// isoCountryCodes holds every ISO 3166-1 alpha-2 code. Subscriptions that do
// not ship flag emoji usually prefix the node name with one of these codes, as
// in "usLos Angeles" or "jp|Tokyo".
const isoCountryCodes = "ad ae af ag ai al am ao aq ar as at au aw ax az " +
	"ba bb bd be bf bg bh bi bj bl bm bn bo bq br bs bt bv bw by bz " +
	"ca cc cd cf cg ch ci ck cl cm cn co cr cu cv cw cx cy cz " +
	"de dj dk dm do dz " +
	"ec ee eg eh er es et " +
	"fi fj fk fm fo fr " +
	"ga gb gd ge gf gg gh gi gl gm gn gp gq gr gs gt gu gw gy " +
	"hk hm hn hr ht hu " +
	"id ie il im in io iq ir is it " +
	"je jm jo jp " +
	"ke kg kh ki km kn kp kr kw ky kz " +
	"la lb lc li lk lr ls lt lu lv ly " +
	"ma mc md me mf mg mh mk ml mm mn mo mp mq mr ms mt mu mv mw mx my mz " +
	"na nc ne nf ng ni nl no np nr nu nz " +
	"om " +
	"pa pe pf pg ph pk pl pm pn pr ps pt pw py " +
	"qa " +
	"re ro rs ru rw " +
	"sa sb sc sd se sg sh si sj sk sl sm sn so sr ss st sv sx sy sz " +
	"tc td tf tg th tj tk tl tm tn to tr tt tv tw tz " +
	"ua ug um us uy uz " +
	"va vc ve vg vi vn vu " +
	"wf ws " +
	"ye yt " +
	"za zm zw"

var countryCodeSet = func() map[string]bool {
	set := make(map[string]bool, 256)
	for _, code := range strings.Fields(isoCountryCodes) {
		set[strings.ToUpper(code)] = true
	}
	set["UK"] = true // not ISO, but common in generated names
	return set
}()

// canonicalCode maps the aliases generated names use onto an ISO code.
func canonicalCode(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "UK" {
		return "GB"
	}
	return code
}

// CountryPrefix splits a name that carries a country, such as "amArmenia",
// "gb|UK London" or "🇭🇰Hong Kong", into its ISO code and the name without the
// prefix. ok is false when the name has no prefix ("ss02 香港", "is really
// slow", "日本 01", …). The separator between code and name is dropped.
func CountryPrefix(name string) (code, rest string, ok bool) {
	s := strings.TrimSpace(name)
	r := []rune(s)
	if len(r) < 3 {
		return "", s, false
	}
	if isRegionalIndicator(r[0]) && isRegionalIndicator(r[1]) {
		// A flag emoji already: recover its code and drop the pictograph.
		code = string([]rune{'A' + r[0] - 0x1F1E6, 'A' + r[1] - 0x1F1E6})
		if code = canonicalCode(code); !countryCodeSet[code] {
			return "", s, false
		}
		rest = trimSeparators(string(r[2:]))
		if rest == "" {
			return "", s, false
		}
		return code, rest, true
	}
	if r[0] > unicode.MaxASCII || r[1] > unicode.MaxASCII {
		return "", s, false
	}
	code = canonicalCode(string(r[:2]))
	if !countryCodeSet[code] {
		return "", s, false
	}
	rest = trimSeparators(string(r[2:]))
	if rest == "" {
		return "", s, false
	}
	// The remainder must read like a place name: a capitalised Latin word or a
	// non-Latin script. Digits or lower-case words mean the leading letters
	// were a protocol tag or an English word, not a country code.
	first, _ := utf8.DecodeRuneInString(rest)
	if !unicode.IsUpper(first) && first < utf8.RuneSelf {
		return "", s, false
	}
	return code, rest, true
}

func trimSeparators(s string) string {
	return strings.TrimLeft(s, " \t|-／/｜·:：")
}

// isRegionalIndicator reports whether r is part of a flag emoji
// (U+1F1E6–U+1F1FF).
func isRegionalIndicator(r rune) bool {
	return r >= 0x1F1E6 && r <= 0x1F1FF
}

// CountryCode reports the ISO code a name carries, or "".
func CountryCode(name string) string {
	code, _, ok := CountryPrefix(name)
	if !ok {
		return ""
	}
	return code
}

// SortName is the name without its country prefix, so names sort by the place
// rather than by the prefix.
func SortName(name string) string {
	_, rest, ok := CountryPrefix(name)
	if !ok {
		return strings.TrimSpace(name)
	}
	return rest
}
