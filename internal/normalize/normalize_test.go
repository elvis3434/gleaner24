package normalize

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestURL(t *testing.T) {
	tests := []struct{ in, want string }{
		{"HTTPS://Example.COM/News/A?b=2&a=1#top", "https://example.com/News/A?a=1&b=2"},
		{"https://example.com/a?utm_source=x&utm_medium=y&id=5", "https://example.com/a?id=5"},
		{"https://example.com/a?fbclid=abc&gclid=def", "https://example.com/a"},
		{"https://example.com", "https://example.com/"},
		{"http://example.com:80/x", "http://example.com/x"},
		{"https://example.com:443/x", "https://example.com/x"},
		{"https://example.com:8443/x", "https://example.com:8443/x"},
		{"https://user:pw@example.com/x", "https://example.com/x"},
		{"https://example.com/x?", "https://example.com/x"},
		{"  https://example.com/x  ", "https://example.com/x"},
		{"https://example.com/%E5%8F%B0", "https://example.com/%E5%8F%B0"},
		{"http://[::1]:8080/x", "http://[::1]:8080/x"},
		{"http://[::1]:80/x", "http://[::1]/x"},
		{"https://[2001:DB8::1]/x", "https://[2001:db8::1]/x"},
	}
	for _, tt := range tests {
		got, err := URL(tt.in)
		if err != nil {
			t.Errorf("URL(%q) error: %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("URL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestURLErrors(t *testing.T) {
	for _, in := range []string{"ftp://example.com/", "/relative/path", "https://", "mailto:a@b.c"} {
		if _, err := URL(in); err == nil {
			t.Errorf("URL(%q) expected error", in)
		}
	}
}

func TestText(t *testing.T) {
	got := Text("  Ｈｅｌｌｏ\n\n\t世界  ok ")
	if got != "Hello 世界 ok" {
		t.Errorf("Text = %q", got)
	}
}

func TestHash(t *testing.T) {
	if Hash("a") != "ca978112ca1bbdcafac231b39a23dc4da786eff8147c4e72b9807785afee48bb" {
		t.Error("unexpected sha256")
	}
	if Hash("a") == Hash("b") {
		t.Error("hash collision")
	}
}

func TestCheckFileName(t *testing.T) {
	good := []string{"tsmc", "台積電", "news-2026", "a.b"}
	bad := []string{"", ".", "..", "a/b", `a\b`, "a:b", "a*b", "a?", "a<b", "x.", "x ", "CON", "con", "nul.txt", "COM1", "lpt9", "a\tb"}
	for _, n := range good {
		if err := CheckFileName(n); err != nil {
			t.Errorf("CheckFileName(%q) = %v, want nil", n, err)
		}
	}
	for _, n := range bad {
		if err := CheckFileName(n); err == nil {
			t.Errorf("CheckFileName(%q) = nil, want error", n)
		}
	}
}

func TestSlug(t *testing.T) {
	tests := []struct {
		in   string
		max  int
		want string
	}{
		{"Hello, World!", 80, "hello-world"},
		{"台積電：第三季營收創新高", 80, "台積電-第三季營收創新高"},
		{"  --a//b??  ", 80, "a-b"},
		{"CON", 80, "con"},
		{"abcdef", 3, "abc"},
		{"ab cd", 3, "ab"},
		{"!!!", 80, ""},
	}
	for _, tt := range tests {
		if got := Slug(tt.in, tt.max); got != tt.want {
			t.Errorf("Slug(%q, %d) = %q, want %q", tt.in, tt.max, got, tt.want)
		}
	}
	long := Slug(strings.Repeat("字", 200), 80)
	if utf8.RuneCountInString(long) != 80 {
		t.Errorf("long slug length = %d", utf8.RuneCountInString(long))
	}
	if strings.HasSuffix(Slug(strings.Repeat("ab ", 50), 80), "-") {
		t.Error("slug ends with hyphen")
	}
}
