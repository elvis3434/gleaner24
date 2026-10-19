package lang

import "testing"

func TestCode(t *testing.T) {
	tests := map[string]string{
		"en": "en", "en-US": "en", "EN_gb": "en",
		"ja": "ja", "ja-JP": "ja",
		"zh-TW": "zh-TW", "zh-Hant": "zh-TW", "zh-Hant-TW": "zh-TW", "zh-HK": "zh-TW",
		"zh-CN": "zh-CN", "zh-Hans": "zh-CN", "zh-Hans-CN": "zh-CN", "zh_SG": "zh-CN",
		"zh": "", "fr": "", "": "", " ja ": "ja",
	}
	for in, want := range tests {
		if got := Code(in); got != want {
			t.Errorf("Code(%q) = %q, want %q", in, got, want)
		}
	}
}
