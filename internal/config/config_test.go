package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const minimal = `
[[sources.feeds]]
name = "f"
url  = "https://example.com/feed.xml"

[[topics]]
name  = "t"
query = "a OR b"
`

func TestExampleConfigIsValid(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "..", "config.example.toml"))
	if err != nil {
		t.Fatalf("config.example.toml: %v", err)
	}
	if len(cfg.Topics) != 1 || cfg.Topics[0].Compiled == nil {
		t.Fatal("topic query not compiled")
	}
	if ok, _ := cfg.Topics[0].Compiled.Match("台積電公布營收"); !ok {
		t.Error("example query should match")
	}
}

func TestDefaults(t *testing.T) {
	cfg, err := Parse(minimal)
	if err != nil {
		t.Fatal(err)
	}
	def := Default()
	if !reflect.DeepEqual(cfg.Output, def.Output) {
		t.Errorf("output = %+v, want %+v", cfg.Output, def.Output)
	}
	if !reflect.DeepEqual(cfg.Crawl, def.Crawl) {
		t.Errorf("crawl = %+v, want %+v", cfg.Crawl, def.Crawl)
	}
	if cfg.Crawl.PerHostDelay.Duration != 2*time.Second || cfg.Crawl.Timeout.Duration != 20*time.Second {
		t.Errorf("durations = %v, %v", cfg.Crawl.PerHostDelay, cfg.Crawl.Timeout)
	}
	tp := cfg.Topics[0]
	if !reflect.DeepEqual(tp.MatchIn, []string{"title", "body"}) || !tp.CNVariantMerge {
		t.Errorf("topic defaults = %+v", tp)
	}
	if !tp.Images.Enabled || tp.Images.MinWidth != 300 || tp.Images.MaxPerPage != 10 {
		t.Errorf("image defaults = %+v", tp.Images)
	}
}

func TestOverrides(t *testing.T) {
	cfg, err := Parse(`
[output]
archive_dir = "A"
log_dir = "L"
db = "D"
on_update = "overwrite"

[crawl]
per_host_delay = "500ms"
retries = 0

[[sources.feeds]]
name = "f"
url  = "https://example.com/feed.xml"

[[topics]]
name = "one"
query = "x"
match_in = ["title"]
cn_variant_merge = false
  [topics.images]
  enabled = false
  min_width = 0

[[topics]]
name = "two"
query = "y"
sites = ["f"]
`)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Output.OnUpdate != Overwrite || cfg.Output.ArchiveDir != "A" {
		t.Errorf("output = %+v", cfg.Output)
	}
	if cfg.Crawl.PerHostDelay.Duration != 500*time.Millisecond || cfg.Crawl.Retries != 0 {
		t.Errorf("crawl = %+v", cfg.Crawl)
	}
	if cfg.Crawl.Timeout.Duration != 20*time.Second {
		t.Error("unset crawl field lost its default")
	}
	one, two := cfg.Topics[0], cfg.Topics[1]
	if !reflect.DeepEqual(one.MatchIn, []string{"title"}) || one.CNVariantMerge || one.Images.Enabled || one.Images.MinWidth != 0 {
		t.Errorf("topic one = %+v", one)
	}
	if one.Images.MaxPerPage != 10 {
		t.Errorf("topic one unset image field = %d, want default 10", one.Images.MaxPerPage)
	}
	// Settings of the first topic must not leak into the second.
	if !reflect.DeepEqual(two.MatchIn, []string{"title", "body"}) || !two.CNVariantMerge || !two.Images.Enabled || two.Images.MinWidth != 300 {
		t.Errorf("topic two = %+v", two)
	}
	if !two.AppliesTo("f") || two.AppliesTo("other") || !one.AppliesTo("anything") {
		t.Error("AppliesTo wrong")
	}
}

func TestValidationErrors(t *testing.T) {
	_, err := Parse(`
[output]
on_update = "replace"

[crawl]
timeout = "0s"
concurrency = 0

[[sources.feeds]]
name = "dup"
url  = "ftp://example.com/feed"
languages = ["fr"]

[[sources.feeds]]
name = "dup"
url  = "https://example.com/feed"

[[topics]]
name = "CON"
query = "(a OR b"
match_in = ["summary"]
sites = ["nope"]

[[topics]]
name = "x"
query = "a"
unknown_key = 1
`)
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v, want *ValidationError", err)
	}
	wantFields := []string{
		"topics.unknown_key: unknown field",
		"output.on_update",
		"crawl.timeout",
		"crawl.concurrency",
		"sources.feeds[0].url",
		"sources.feeds[0].languages",
		"sources.feeds[1].name: duplicate",
		"topics[0].name",
		"topics[0].query: column 1",
		"topics[0].match_in",
		`topics[0].sites: unknown source name "nope"`,
	}
	msg := err.Error()
	for _, w := range wantFields {
		if !strings.Contains(msg, w) {
			t.Errorf("error does not mention %q:\n%s", w, msg)
		}
	}
}

func TestMissingSourcesAndTopics(t *testing.T) {
	_, err := Parse(``)
	if err == nil {
		t.Fatal("expected error")
	}
	for _, w := range []string{"sources:", "topics:"} {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("error does not mention %q: %v", w, err)
		}
	}
}

func TestBadDurationAndSyntax(t *testing.T) {
	if _, err := Parse("[crawl]\ntimeout = \"soon\"\n"); err == nil {
		t.Error("bad duration accepted")
	}
	if _, err := Parse("[output\n"); err == nil {
		t.Error("bad TOML accepted")
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.toml"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("err = %v, want not-exist", err)
	}
}
