package archive

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var taipei = time.FixedZone("CST", 8*3600)

func TestRelPath(t *testing.T) {
	at := time.Date(2026, 10, 19, 9, 0, 0, 0, taipei)
	p := RelPath(at, "tsmc", "台積電：第三季營收創新高", "https://news.example/a", 1)
	want := filepath.Join("2026", "10", "19", "tsmc", "台積電-第三季營收創新高-"+Slug("", "https://news.example/a")+".md")
	if p != want {
		t.Errorf("RelPath = %q, want %q", p, want)
	}
	if v2 := RelPath(at, "tsmc", "x", "u", 2); !strings.HasSuffix(v2, "_v2.md") {
		t.Errorf("v2 path = %q", v2)
	}
}

func TestSlug(t *testing.T) {
	a, b := Slug("Same Title", "https://a.example/1"), Slug("Same Title", "https://a.example/2")
	if a == b {
		t.Error("different URLs produced the same slug")
	}
	if !strings.HasPrefix(a, "same-title-") || len(a) != len("same-title-")+8 {
		t.Errorf("slug = %q", a)
	}
	if s := Slug("???", "u"); len(s) != 8 {
		t.Errorf("empty-title slug = %q", s)
	}
	long := Slug(strings.Repeat("長", 300), "u")
	if n := len([]rune(long)); n != MaxSlugRunes+9 {
		t.Errorf("long slug has %d runes", n)
	}
}

func TestRender(t *testing.T) {
	pub := time.Date(2026, 10, 17, 20, 0, 0, 0, time.UTC) // 2026-10-18 in Taipei
	d := Doc{
		Title:       `台積電: "營收" #1`,
		SourceURL:   "https://news.example/a",
		Site:        "feed",
		CollectedAt: time.Date(2026, 10, 19, 9, 0, 0, 0, taipei),
		PublishedAt: &pub,
		Language:    "zh-TW",
		Topics:      []string{"tsmc"},
		Keywords:    []string{"台積電", "營收"},
		Version:     1,
		Body:        "\n正文\n\n第二段\n",
	}
	got := string(Render(d))
	want := `---
title: "台積電: \"營收\" #1"
source_url: "https://news.example/a"
site: "feed"
collected_at: 2026-10-19T09:00:00+08:00
published_at: 2026-10-18
language: "zh-TW"
topics: ["tsmc"]
matched_keywords: ["台積電", "營收"]
version: 1
images: []
---

正文

第二段
`
	if got != want {
		t.Errorf("Render:\n%s\nwant:\n%s", got, want)
	}

	d.PublishedAt, d.Language = nil, ""
	got = string(Render(d))
	if strings.Contains(got, "published_at") || strings.Contains(got, "language") {
		t.Errorf("optional fields rendered:\n%s", got)
	}
}

func TestWriteAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "2026", "10", "19", "t", "a.md")
	d := Doc{Title: "a", CollectedAt: time.Now(), Version: 1, Body: "one"}
	if err := Write(path, d); err != nil {
		t.Fatal(err)
	}
	d.Body = "two"
	if err := Write(path, d); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(b), "\ntwo\n") {
		t.Errorf("content = %q", b)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("leftover files: %v", entries)
	}
}
