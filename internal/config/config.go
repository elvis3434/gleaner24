// Package config loads, defaults and validates the gleaner TOML config.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/elvis3434/gleaner24/internal/normalize"
	"github.com/elvis3434/gleaner24/internal/query"
)

// Supported language codes (SPEC F4).
var Languages = []string{"en", "ja", "zh-TW", "zh-CN"}

// Update strategies for output.on_update.
const (
	KeepVersions = "keep_versions"
	Overwrite    = "overwrite"
)

// Duration is a time.Duration written as a string such as "2s" in TOML.
type Duration struct{ time.Duration }

func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return err
	}
	d.Duration = v
	return nil
}

func (d Duration) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

type Config struct {
	Output  Output  `toml:"output"`
	Crawl   Crawl   `toml:"crawl"`
	Sources Sources `toml:"sources"`
	Topics  []Topic `toml:"topics"`
}

type Output struct {
	ArchiveDir string `toml:"archive_dir"`
	LogDir     string `toml:"log_dir"`
	DB         string `toml:"db"`
	OnUpdate   string `toml:"on_update"`
}

type Crawl struct {
	UserAgent           string   `toml:"user_agent"`
	PerHostDelay        Duration `toml:"per_host_delay"`
	Concurrency         int      `toml:"concurrency"`
	Timeout             Duration `toml:"timeout"`
	Retries             int      `toml:"retries"`
	RespectRobots       bool     `toml:"respect_robots"`
	MaxPagesPerRun      int      `toml:"max_pages_per_run"`
	RecentDays          int      `toml:"recent_days"`
	RecheckDays         int      `toml:"recheck_days"`
	FollowExternalLinks bool     `toml:"follow_external_links"`
}

type Sources struct {
	Sites      []Site     `toml:"sites"`
	Feeds      []Feed     `toml:"feeds"`
	GoogleNews GoogleNews `toml:"google_news"`
}

type Site struct {
	Name         string   `toml:"name"`
	StartURLs    []string `toml:"start_urls"`
	UseSitemap   bool     `toml:"use_sitemap"`
	MaxDepth     int      `toml:"max_depth"`
	MaxPages     int      `toml:"max_pages"`
	IncludePaths []string `toml:"include_paths"`
	ExcludePaths []string `toml:"exclude_paths"`
	Languages    []string `toml:"languages"`
	RenderJS     bool     `toml:"render_js"`
}

type Feed struct {
	Name          string   `toml:"name"`
	URL           string   `toml:"url"`
	Languages     []string `toml:"languages"`
	FetchFullText bool     `toml:"fetch_full_text"`
}

type GoogleNews struct {
	Enabled  bool     `toml:"enabled"`
	Editions []string `toml:"editions"`
}

type Topic struct {
	Name           string   `toml:"name"`
	Query          string   `toml:"query"`
	Languages      []string `toml:"languages"`
	MatchIn        []string `toml:"match_in"`
	CaseSensitive  bool     `toml:"case_sensitive"`
	CNVariantMerge bool     `toml:"cn_variant_merge"`
	Sites          []string `toml:"sites"`
	Images         Images   `toml:"images"`

	// Compiled is the parsed Query, filled in by Load.
	Compiled *query.Query `toml:"-"`
}

type Images struct {
	Enabled             bool `toml:"enabled"`
	MinWidth            int  `toml:"min_width"`
	RequireKeywordInAlt bool `toml:"require_keyword_in_alt"`
	MaxPerPage          int  `toml:"max_per_page"`
}

// AppliesTo reports whether the topic should be evaluated for content from
// the named source.
func (t *Topic) AppliesTo(source string) bool {
	return len(t.Sites) == 0 || slices.Contains(t.Sites, source)
}

// ValidationError lists every problem found in a config, each prefixed with
// the offending field name.
type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	return "invalid config:\n  " + strings.Join(e.Problems, "\n  ")
}

// Default returns a Config holding every default value.
func Default() Config {
	return Config{
		Output: Output{
			ArchiveDir: "./archive",
			LogDir:     "./logs",
			DB:         "./data/index.db",
			OnUpdate:   KeepVersions,
		},
		Crawl: Crawl{
			UserAgent:      "Gleaner/1.0 (personal archive)",
			PerHostDelay:   Duration{2 * time.Second},
			Concurrency:    4,
			Timeout:        Duration{20 * time.Second},
			Retries:        2,
			RespectRobots:  true,
			MaxPagesPerRun: 2000,
			RecentDays:     7,
			RecheckDays:    3,
		},
	}
}

func defaultTopic() Topic {
	return Topic{
		MatchIn:        []string{"title", "body"},
		CNVariantMerge: true,
		Images:         Images{Enabled: true, MinWidth: 300, MaxPerPage: 10},
	}
}

// Load reads, defaults and validates the config at path. Validation errors
// are returned as *ValidationError.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(string(data))
}

// Parse is Load for config text already in memory.
func Parse(data string) (*Config, error) {
	cfg := Default()
	md, err := toml.Decode(data, &cfg)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	var problems []string
	for _, k := range md.Undecoded() {
		problems = append(problems, fmt.Sprintf("%s: unknown field", k))
	}
	applyTopicDefaults(&cfg, md)
	problems = append(problems, cfg.validate()...)
	if len(problems) > 0 {
		return nil, &ValidationError{Problems: problems}
	}
	return &cfg, nil
}

// applyTopicDefaults fills fields of each [[topics]] entry that the file
// does not set. Tables in an array cannot be pre-filled before decoding, so
// this checks which keys were present.
func applyTopicDefaults(cfg *Config, md toml.MetaData) {
	def := defaultTopic()
	for i := range cfg.Topics {
		t := &cfg.Topics[i]
		has := func(key ...string) bool { return isDefinedAt(md, i, key...) }
		if !has("match_in") {
			t.MatchIn = def.MatchIn
		}
		if !has("cn_variant_merge") {
			t.CNVariantMerge = def.CNVariantMerge
		}
		if !has("images", "enabled") {
			t.Images.Enabled = def.Images.Enabled
		}
		if !has("images", "min_width") {
			t.Images.MinWidth = def.Images.MinWidth
		}
		if !has("images", "max_per_page") {
			t.Images.MaxPerPage = def.Images.MaxPerPage
		}
	}
}

// isDefinedAt reports whether key was set in the i-th [[topics]] table.
// toml.MetaData.IsDefined cannot index into arrays of tables, so the keys
// list is scanned instead: each [[topics]] entry starts with a bare
// "topics" key, followed by that entry's own keys.
func isDefinedAt(md toml.MetaData, i int, key ...string) bool {
	want := strings.Join(append([]string{"topics"}, key...), ".")
	idx := -1
	for _, k := range md.Keys() {
		s := k.String()
		if s == "topics" {
			idx++
			continue
		}
		if idx == i && s == want {
			return true
		}
	}
	return false
}

func (c *Config) validate() []string {
	var p []string
	add := func(field, format string, args ...any) {
		p = append(p, field+": "+fmt.Sprintf(format, args...))
	}

	if strings.TrimSpace(c.Output.ArchiveDir) == "" {
		add("output.archive_dir", "must not be empty")
	}
	if strings.TrimSpace(c.Output.LogDir) == "" {
		add("output.log_dir", "must not be empty")
	}
	if strings.TrimSpace(c.Output.DB) == "" {
		add("output.db", "must not be empty")
	}
	if c.Output.OnUpdate != KeepVersions && c.Output.OnUpdate != Overwrite {
		add("output.on_update", "must be %q or %q, got %q", KeepVersions, Overwrite, c.Output.OnUpdate)
	}

	if strings.TrimSpace(c.Crawl.UserAgent) == "" {
		add("crawl.user_agent", "must not be empty")
	}
	if c.Crawl.PerHostDelay.Duration < 0 {
		add("crawl.per_host_delay", "must not be negative")
	}
	if c.Crawl.Timeout.Duration <= 0 {
		add("crawl.timeout", "must be positive")
	}
	if c.Crawl.Concurrency < 1 {
		add("crawl.concurrency", "must be at least 1")
	}
	if c.Crawl.Retries < 0 {
		add("crawl.retries", "must not be negative")
	}
	if c.Crawl.MaxPagesPerRun < 1 {
		add("crawl.max_pages_per_run", "must be at least 1")
	}
	if c.Crawl.RecentDays < 0 {
		add("crawl.recent_days", "must not be negative")
	}
	if c.Crawl.RecheckDays < 0 {
		add("crawl.recheck_days", "must not be negative")
	}

	names := map[string]string{} // source name -> field that defined it
	checkName := func(field, name string) {
		if strings.TrimSpace(name) == "" {
			add(field, "must not be empty")
			return
		}
		if prev, ok := names[name]; ok {
			add(field, "duplicate source name %q (also used by %s)", name, prev)
			return
		}
		names[name] = field
	}
	checkLangs := func(field string, langs []string) {
		for _, l := range langs {
			if !slices.Contains(Languages, l) {
				add(field, "unsupported language %q (supported: %s)", l, strings.Join(Languages, ", "))
			}
		}
	}

	for i, s := range c.Sources.Sites {
		f := fmt.Sprintf("sources.sites[%d]", i)
		checkName(f+".name", s.Name)
		if len(s.StartURLs) == 0 {
			add(f+".start_urls", "must list at least one URL")
		}
		for j, u := range s.StartURLs {
			if err := checkHTTPURL(u); err != nil {
				add(fmt.Sprintf("%s.start_urls[%d]", f, j), "%v", err)
			}
		}
		if s.MaxDepth < 0 {
			add(f+".max_depth", "must not be negative")
		}
		if s.MaxPages < 0 {
			add(f+".max_pages", "must not be negative")
		}
		checkLangs(f+".languages", s.Languages)
	}
	for i, fd := range c.Sources.Feeds {
		f := fmt.Sprintf("sources.feeds[%d]", i)
		checkName(f+".name", fd.Name)
		if err := checkHTTPURL(fd.URL); err != nil {
			add(f+".url", "%v", err)
		}
		checkLangs(f+".languages", fd.Languages)
	}
	if gn := c.Sources.GoogleNews; gn.Enabled {
		if len(gn.Editions) == 0 {
			add("sources.google_news.editions", "must list at least one edition when enabled")
		}
		checkLangs("sources.google_news.editions", gn.Editions)
	}
	if len(c.Sources.Sites) == 0 && len(c.Sources.Feeds) == 0 && !c.Sources.GoogleNews.Enabled {
		add("sources", "define at least one of sources.sites, sources.feeds or sources.google_news")
	}

	if len(c.Topics) == 0 {
		add("topics", "define at least one [[topics]] entry")
	}
	topicNames := map[string]bool{}
	for i := range c.Topics {
		t := &c.Topics[i]
		f := fmt.Sprintf("topics[%d]", i)
		if err := normalize.CheckFileName(t.Name); err != nil {
			add(f+".name", "%v (the name is used as a folder name)", err)
		} else if topicNames[t.Name] {
			add(f+".name", "duplicate topic name %q", t.Name)
		}
		topicNames[t.Name] = true

		q, err := query.Parse(t.Query, query.Options{CaseSensitive: t.CaseSensitive})
		if err != nil {
			add(f+".query", "%v", err)
		}
		t.Compiled = q

		checkLangs(f+".languages", t.Languages)
		if len(t.MatchIn) == 0 {
			add(f+".match_in", `must list "title" and/or "body"`)
		}
		for _, m := range t.MatchIn {
			if m != "title" && m != "body" {
				add(f+".match_in", `must be "title" or "body", got %q`, m)
			}
		}
		for _, s := range t.Sites {
			if _, ok := names[s]; !ok {
				add(f+".sites", "unknown source name %q", s)
			}
		}
		if t.Images.MinWidth < 0 {
			add(f+".images.min_width", "must not be negative")
		}
		if t.Images.MaxPerPage < 0 {
			add(f+".images.max_per_page", "must not be negative")
		}
	}
	return p
}

func checkHTTPURL(raw string) error {
	if raw == "" {
		return errors.New("must not be empty")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL: %v", err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("must be an absolute http(s) URL, got %q", raw)
	}
	return nil
}
