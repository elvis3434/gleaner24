package extract

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const base = "https://news.example"

func page(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "pages", name))
	if err != nil {
		t.Fatal(err)
	}
	return []byte(strings.ReplaceAll(string(b), "{{BASE}}", base))
}

func TestPage(t *testing.T) {
	u, _ := url.Parse(base + "/news/tsmc-q3.html?utm_source=rss")
	art, err := Page(page(t, "tsmc-q3.html"), "text/html; charset=utf-8", u)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(art.Title, "台積電第三季營收創新高") {
		t.Errorf("title = %q", art.Title)
	}
	if art.Canonical != base+"/news/tsmc-q3.html" {
		t.Errorf("canonical = %q", art.Canonical)
	}
	if art.Lang != "zh-Hant-TW" {
		t.Errorf("lang = %q", art.Lang)
	}
	if art.Published == nil || !art.Published.Equal(time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("published = %v", art.Published)
	}
	if !strings.Contains(art.Text, "單季營收達新台幣七千五百億元") {
		t.Errorf("text missing body: %q", art.Text)
	}
	for _, junk := range []string{"限時優惠", "登入", "版權所有"} {
		if strings.Contains(art.Text, junk) || strings.Contains(art.Markdown, junk) {
			t.Errorf("boilerplate %q not removed", junk)
		}
	}
	if !strings.Contains(art.Markdown, "先進製程需求強勁") {
		t.Errorf("markdown = %q", art.Markdown)
	}
}

func TestPageDeterministic(t *testing.T) {
	u, _ := url.Parse(base + "/news/tsmc-q3.html")
	a1, err1 := Page(page(t, "tsmc-q3.html"), "text/html", u)
	a2, err2 := Page(page(t, "tsmc-q3.html"), "text/html", u)
	if err1 != nil || err2 != nil {
		t.Fatal(err1, err2)
	}
	if a1.Text != a2.Text || a1.Markdown != a2.Markdown {
		t.Error("extraction is not deterministic")
	}
}

func TestPageBig5(t *testing.T) {
	// "中文" in Big5 is A4 A4 A4 E5.
	body := append([]byte(`<html><head><meta charset="big5"><title>t</title></head><body><article><p>`),
		0xA4, 0xA4, 0xA4, 0xE5)
	body = append(body, []byte(`</p></article></body></html>`)...)
	art, err := Page(body, "text/html", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(art.Text, "中文") {
		t.Errorf("text = %q", art.Text)
	}
}

func TestPageNoContent(t *testing.T) {
	_, err := Page([]byte(`<html><body></body></html>`), "text/html", nil)
	if err == nil {
		t.Error("expected error for empty page")
	}
}

func TestFragment(t *testing.T) {
	u, _ := url.Parse(base + "/en/tsmc")
	art, err := Fragment(`<p><b>TSMC</b> reported <a href="/en/report">revenue</a>.</p><p>Second</p><script>var x="hidden"</script>`, u)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(art.Text, "TSMC reported revenue.") || strings.Contains(art.Text, "hidden") {
		t.Errorf("text = %q", art.Text)
	}
	if strings.Contains(art.Text, "/en/report") {
		t.Error("link URL leaked into match text")
	}
	if !strings.Contains(art.Text, "revenue.\n") {
		t.Errorf("paragraphs not separated: %q", art.Text)
	}
	if !strings.Contains(art.Markdown, "**TSMC**") || !strings.Contains(art.Markdown, "("+base+"/en/report)") {
		t.Errorf("markdown = %q", art.Markdown)
	}
}

func TestFragmentPlainText(t *testing.T) {
	art, err := Fragment("DRAM prices dropped.", nil)
	if err != nil {
		t.Fatal(err)
	}
	if art.Text != "DRAM prices dropped." {
		t.Errorf("text = %q", art.Text)
	}
	if _, err := Fragment("   ", nil); !errors.Is(err, ErrNoContent) {
		t.Errorf("empty fragment err = %v", err)
	}
}
