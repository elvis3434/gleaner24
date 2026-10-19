// Package lang handles language codes. Full language detection (SPEC F4)
// arrives in stage 2; for now this maps declared language tags to the
// codes gleaner uses: en, ja, zh-TW and zh-CN.
package lang

import "strings"

// Code maps a declared language tag such as "zh-Hant-TW", "en-US" or "ja"
// to one of the supported codes. It returns "" for unsupported or
// ambiguous tags (a bare "zh" could be either script).
func Code(tag string) string {
	t := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(tag), "_", "-"))
	parts := strings.Split(t, "-")
	switch parts[0] {
	case "en":
		return "en"
	case "ja":
		return "ja"
	case "zh":
		for _, p := range parts[1:] {
			switch p {
			case "hant", "tw", "hk", "mo":
				return "zh-TW"
			case "hans", "cn", "sg":
				return "zh-CN"
			}
		}
	}
	return ""
}
