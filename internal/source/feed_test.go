package source

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const base = "https://news.example"

func load(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "feeds", name))
	if err != nil {
		t.Fatal(err)
	}
	return []byte(strings.ReplaceAll(string(b), "{{BASE}}", base))
}

func TestParseRSS(t *testing.T) {
	items, err := ParseFeed(load(t, "rss.xml"), base+"/rss.xml")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 {
		t.Fatalf("got %d items", len(items))
	}
	it := items[0]
	if it.Title != "台積電第三季營收創新高" {
		t.Errorf("title = %q", it.Title)
	}
	if it.Link != base+"/news/tsmc-q3.html?utm_source=rss&utm_medium=feed" {
		t.Errorf("link = %q", it.Link)
	}
	if it.Published == nil || it.Published.UTC().Format(time.RFC3339) != "2026-10-18T00:00:00Z" {
		t.Errorf("published = %v", it.Published)
	}
	if !strings.Contains(it.Content, "第三季營收") || it.Language != "zh-TW" {
		t.Errorf("content = %q, lang = %q", it.Content, it.Language)
	}
}

func TestParseAtom(t *testing.T) {
	items, err := ParseFeed(load(t, "atom.xml"), base+"/atom.xml")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items", len(items))
	}
	if !strings.Contains(items[0].Content, "<b>TSMC</b>") {
		t.Errorf("content = %q", items[0].Content)
	}
	if items[1].Content != "DRAM contract prices dropped again." {
		t.Errorf("summary fallback = %q", items[1].Content)
	}
	if items[1].Published == nil {
		t.Error("updated date not used as fallback")
	}
}

func TestRelativeLinks(t *testing.T) {
	feed := `<rss version="2.0"><channel><title>x</title>
<item><title>a</title><link>/post/1</link></item>
<item><title>no link</title></item>
</channel></rss>`
	items, err := ParseFeed([]byte(feed), "https://blog.example/feed/")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Link != "https://blog.example/post/1" {
		t.Errorf("items = %+v", items)
	}
}

func TestParseError(t *testing.T) {
	if _, err := ParseFeed([]byte("<html>not a feed</html>"), base); err == nil {
		t.Error("expected error")
	}
}
