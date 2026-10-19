package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elvis3434/gleaner24/internal/config"
	"github.com/elvis3434/gleaner24/internal/runlog"
	"github.com/elvis3434/gleaner24/internal/store"
)

var taipei = location()

// testSite serves testdata/ with {{BASE}} replaced by the server URL.
// Pages can be overridden to simulate content changes.
type testSite struct {
	*httptest.Server
	mu        sync.Mutex
	overrides map[string]string
}

func newTestSite(t *testing.T) *testSite {
	t.Helper()
	ts := &testSite{overrides: map[string]string{}}
	files := map[string]string{
		"/rss.xml":              "feeds/rss.xml",
		"/atom.xml":             "feeds/atom.xml",
		"/news/tsmc-q3.html":    "pages/tsmc-q3.html",
		"/news/tsmc-rumor.html": "pages/tsmc-rumor.html",
		"/news/weather.html":    "pages/weather.html",
	}
	ts.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ts.mu.Lock()
		body, ok := ts.overrides[r.URL.Path]
		ts.mu.Unlock()
		if !ok {
			name, found := files[r.URL.Path]
			if !found {
				http.NotFound(w, r)
				return
			}
			b, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			body = string(b)
		}
		if strings.HasSuffix(r.URL.Path, ".xml") {
			w.Header().Set("Content-Type", "application/xml")
		} else {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		}
		w.Write([]byte(strings.ReplaceAll(body, "{{BASE}}", ts.URL)))
	}))
	t.Cleanup(ts.Close)
	return ts
}

func (ts *testSite) override(path, body string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.overrides[path] = body
}

func testConfig(t *testing.T, base, dir, extra string) *config.Config {
	t.Helper()
	text := `
[output]
archive_dir = '` + filepath.Join(dir, "archive") + `'
log_dir     = '` + filepath.Join(dir, "logs") + `'
db          = '` + filepath.Join(dir, "data", "index.db") + `'

[crawl]
per_host_delay = "0s"
retries = 0
` + extra + `

[[sources.feeds]]
name = "rss"
url = "` + base + `/rss.xml"
languages = ["zh-TW"]
fetch_full_text = true

[[sources.feeds]]
name = "atom"
url = "` + base + `/atom.xml"
fetch_full_text = false

[[topics]]
name = "tsmc"
query = '(台積電 OR TSMC) AND (營收 OR 財報) NOT 傳聞'

[[topics]]
name = "chips"
query = 'TSMC AND (revenue OR earnings)'
sites = ["atom"]
`
	cfg, err := config.Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func doRun(t *testing.T, cfg *config.Config, at time.Time, dry bool) (runlog.Stats, string) {
	t.Helper()
	var out bytes.Buffer
	stats, err := run(context.Background(), cfg, runOptions{
		DryRun: dry, Out: &out, Loc: taipei,
		Now: func() time.Time { return at },
	})
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out.String())
	}
	return stats, out.String()
}

func TestRunTwiceIsIdempotent(t *testing.T) {
	site := newTestSite(t)
	dir := t.TempDir()
	cfg := testConfig(t, site.URL, dir, "")
	day := time.Date(2026, 10, 19, 9, 0, 0, 0, taipei)

	s1, out1 := doRun(t, cfg, day, false)
	// rss: tsmc-q3 matches; rumor (NOT 傳聞) and weather do not; missing.html 404s.
	// atom: the TSMC entry matches "chips" only (no 營收/財報); memory matches nothing.
	if s1.New != 2 || s1.Updated != 0 || s1.Effective != 2 {
		t.Errorf("run 1: new=%d updated=%d effective=%d\n%s", s1.New, s1.Updated, s1.Effective, out1)
	}
	if s1.SkippedNoMatch != 3 || s1.Failed != 1 || s1.EndReason != runlog.Completed {
		t.Errorf("run 1: nomatch=%d failed=%d reason=%s\n%s", s1.SkippedNoMatch, s1.Failed, s1.EndReason, out1)
	}
	if s1.Fetched != 6 { // 2 feeds + 4 rss item pages
		t.Errorf("run 1: fetched=%d", s1.Fetched)
	}
	if s1.PerTopic["tsmc"] != 1 || s1.PerTopic["chips"] != 1 || s1.PerSite["rss"] != 1 || s1.PerSite["atom"] != 1 {
		t.Errorf("run 1: per topic %v, per site %v", s1.PerTopic, s1.PerSite)
	}

	// The tracking parameters are gone and the canonical URL is used.
	files := archiveFiles(t, filepath.Join(dir, "archive"))
	if len(files) != 2 {
		t.Fatalf("archive files = %v", files)
	}
	q3 := findFile(t, files, "台積電第三季營收創新高")
	md, _ := os.ReadFile(q3)
	for _, want := range []string{
		`title: "台積電第三季營收創新高"`,
		`source_url: "` + site.URL + `/news/tsmc-q3.html"`,
		`site: "rss"`,
		"collected_at: 2026-10-19T09:00:00+08:00",
		"published_at: 2026-10-18",
		`language: "zh-TW"`,
		`topics: ["tsmc"]`,
		`matched_keywords: ["台積電", "營收", "財報"]`,
		"version: 1",
		"單季營收達新台幣七千五百億元",
	} {
		if !strings.Contains(string(md), want) {
			t.Errorf("%s missing %q:\n%s", q3, want, md)
		}
	}
	if !strings.Contains(filepath.ToSlash(q3), "/2026/10/19/tsmc/") {
		t.Errorf("unexpected path %s", q3)
	}
	atom := findFile(t, files, "tsmc-revenue-beats-estimates")
	md, _ = os.ReadFile(atom)
	if !strings.Contains(string(md), `topics: ["chips"]`) || !strings.Contains(string(md), `language: "en"`) {
		t.Errorf("atom archive:\n%s", md)
	}

	// Second run: nothing new.
	s2, out2 := doRun(t, cfg, day.Add(time.Hour), false)
	if s2.Effective != 0 || s2.SkippedDuplicate != 2 {
		t.Errorf("run 2: effective=%d duplicate=%d\n%s", s2.Effective, s2.SkippedDuplicate, out2)
	}
	if n := len(archiveFiles(t, filepath.Join(dir, "archive"))); n != 2 {
		t.Errorf("run 2 wrote files: %d", n)
	}

	// Both runs are recorded in the database and have their own log file.
	st, err := store.OpenReadOnly(cfg.Output.DB)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	runs, err := st.RecentRuns(10)
	if err != nil || len(runs) != 2 {
		t.Fatalf("runs = %v, %v", runs, err)
	}
	if !strings.Contains(runs[0].StatsJSON, `"effective":0`) || !strings.Contains(runs[1].StatsJSON, `"effective":2`) {
		t.Errorf("stats_json = %s / %s", runs[0].StatsJSON, runs[1].StatsJSON)
	}
	logs, _ := filepath.Glob(filepath.Join(dir, "logs", "collect_*.log"))
	if len(logs) != 2 {
		t.Errorf("log files = %v", logs)
	}
	logText, _ := os.ReadFile(filepath.Join(dir, "logs", "collect_20261019_0900.log"))
	if !strings.Contains(string(logText), "有效筆數") || !strings.Contains(string(logText), "missing.html") {
		t.Errorf("log file:\n%s", logText)
	}
}

func TestRunDetectsUpdate(t *testing.T) {
	for _, mode := range []string{config.KeepVersions, config.Overwrite} {
		t.Run(mode, func(t *testing.T) {
			site := newTestSite(t)
			dir := t.TempDir()
			cfg := testConfig(t, site.URL, dir, "")
			cfg.Output.OnUpdate = mode
			day := time.Date(2026, 10, 19, 9, 0, 0, 0, taipei)
			doRun(t, cfg, day, false)

			orig, _ := os.ReadFile(filepath.Join("..", "..", "testdata", "pages", "tsmc-q3.html"))
			site.override("/news/tsmc-q3.html", strings.Replace(string(orig), "七千五百億元", "七千六百億元", 1))

			s, out := doRun(t, cfg, day.Add(24*time.Hour), false)
			if s.Updated != 1 || s.New != 0 || s.SkippedDuplicate != 1 {
				t.Fatalf("updated=%d new=%d dup=%d\n%s", s.Updated, s.New, s.SkippedDuplicate, out)
			}
			files := archiveFiles(t, filepath.Join(dir, "archive"))
			var v2 []string
			for _, f := range files {
				if strings.HasSuffix(f, "_v2.md") {
					v2 = append(v2, f)
				}
			}
			switch mode {
			case config.KeepVersions:
				if len(files) != 3 || len(v2) != 1 || !strings.Contains(filepath.ToSlash(v2[0]), "/2026/10/20/") {
					t.Errorf("files = %v", files)
				}
			case config.Overwrite:
				if len(files) != 2 || len(v2) != 0 {
					t.Errorf("files = %v", files)
				}
				md, _ := os.ReadFile(findFile(t, files, "台積電第三季營收創新高"))
				if !strings.Contains(string(md), "七千六百億元") || !strings.Contains(string(md), "version: 2") {
					t.Errorf("overwritten file:\n%s", md)
				}
			}
		})
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	site := newTestSite(t)
	dir := t.TempDir()
	cfg := testConfig(t, site.URL, dir, "")
	s, out := doRun(t, cfg, time.Date(2026, 10, 19, 9, 0, 0, 0, taipei), true)
	if s.New != 2 {
		t.Errorf("dry run new=%d\n%s", s.New, out)
	}
	if !strings.Contains(out, "WOULD NEW") {
		t.Errorf("dry run output:\n%s", out)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("dry run created %v", entries)
	}
}

func TestRunLimitReached(t *testing.T) {
	site := newTestSite(t)
	dir := t.TempDir()
	cfg := testConfig(t, site.URL, dir, "max_pages_per_run = 2")
	s, out := doRun(t, cfg, time.Date(2026, 10, 19, 9, 0, 0, 0, taipei), false)
	if s.EndReason != runlog.LimitReached || s.Fetched != 2 {
		t.Errorf("reason=%s fetched=%d\n%s", s.EndReason, s.Fetched, out)
	}
	if !strings.Contains(out, "達到上限") {
		t.Errorf("summary missing limit reason:\n%s", out)
	}
}

func TestRunAborted(t *testing.T) {
	site := newTestSite(t)
	dir := t.TempDir()
	cfg := testConfig(t, site.URL, dir, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stats, err := run(ctx, cfg, runOptions{Loc: taipei})
	if err != nil {
		t.Fatal(err)
	}
	if stats.EndReason != runlog.Aborted {
		t.Errorf("reason = %s", stats.EndReason)
	}
}

func TestCLI(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join("..", "..", "config.example.toml")
	bad := filepath.Join(dir, "bad.toml")
	os.WriteFile(bad, []byte("[[topics]]\nname = \"x\"\nquery = \"(a OR\"\n"), 0o644)

	var out, errb bytes.Buffer
	if code := runMain([]string{"validate", "-c", good}, &out, &errb); code != exitOK {
		t.Errorf("validate good = %d: %s", code, errb.String())
	}
	errb.Reset()
	if code := runMain([]string{"validate", "-c", bad}, &out, &errb); code != exitConfig {
		t.Errorf("validate bad = %d", code)
	}
	if !strings.Contains(errb.String(), "topics[0].query: column") {
		t.Errorf("validate output: %s", errb.String())
	}
	if code := runMain([]string{"run", "-c", bad}, &out, &errb); code != exitConfig {
		t.Errorf("run with bad config = %d", code)
	}
	if code := runMain([]string{"bogus"}, &out, &errb); code != exitConfig {
		t.Errorf("unknown command = %d", code)
	}

	page := filepath.Join("..", "..", "testdata", "pages", "tsmc-q3.html")
	out.Reset()
	if code := runMain([]string{"test-query", "台積電 AND 營收 NOT 傳聞", page}, &out, &errb); code != exitOK {
		t.Fatalf("test-query = %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "結果: 命中") || !strings.Contains(out.String(), "台積電, 營收") {
		t.Errorf("test-query output: %s", out.String())
	}
	out.Reset()
	runMain([]string{"test-query", "傳聞", page}, &out, &errb)
	if !strings.Contains(out.String(), "未命中") {
		t.Errorf("test-query miss output: %s", out.String())
	}
	if code := runMain([]string{"test-query", "(a", page}, &out, &errb); code != exitConfig {
		t.Errorf("test-query bad expr = %d", code)
	}
}

func archiveFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	return files
}

func findFile(t *testing.T, files []string, part string) string {
	t.Helper()
	for _, f := range files {
		if strings.Contains(filepath.Base(f), part) && !strings.HasSuffix(f, "_v2.md") {
			return f
		}
	}
	t.Fatalf("no file containing %q in %v", part, files)
	return ""
}
