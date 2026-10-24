package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elvis3434/gleaner24/internal/market"
	"github.com/elvis3434/gleaner24/internal/runlog"
)

var taipei = location()

// exchange imitates the three exchanges with the official sample responses
// in testdata/quotes: TWSE 2330 and TPEx 6223 for 2026-09, TAIFEX TX and
// TXO for 2026-10-05. Anything else gets each exchange's no-data answer.
type exchange struct {
	*httptest.Server
	mu        sync.Mutex
	overrides map[string][]byte // path -> body
	requests  []string
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "quotes", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func newExchange(t *testing.T) *exchange {
	t.Helper()
	ex := &exchange{overrides: map[string][]byte{}}
	twse := fixture(t, "twse_stock_day_2330_202609.json")
	tpex := fixture(t, "tpex_trading_stock_6223_202609.json")
	fut := fixture(t, "taifex_fut_TX_20261005.csv")
	opt := fixture(t, "taifex_opt_TXO_20261005.csv")
	ex.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		ex.mu.Lock()
		ex.requests = append(ex.requests, r.Method+" "+r.Host+r.URL.Path)
		override, overridden := ex.overrides[r.URL.Path]
		ex.mu.Unlock()
		if overridden {
			w.Write(override)
			return
		}
		f := r.Form
		// TAIFEX answers whole ranges; the sample holds 2026/10/05 only.
		taifexHas := f.Get("queryStartDate") <= "2026/10/05" && f.Get("queryEndDate") >= "2026/10/05"
		switch r.URL.Path {
		case "/exchangeReport/STOCK_DAY":
			if f.Get("stockNo") == "2330" && f.Get("date") == "20260901" {
				w.Write(twse)
				return
			}
			w.Write([]byte(`{"stat":"很抱歉，沒有符合條件的資料!"}`))
		case "/www/zh-tw/afterTrading/tradingStock":
			if f.Get("code") == "6223" && f.Get("date") == "2026/09/01" {
				w.Write(tpex)
				return
			}
			w.Write([]byte(`{"tables":[{"data":[],"fields":[],"totalCount":0}],"stat":"ok"}`))
		case "/cht/3/futDataDown":
			if r.Method == "POST" && f.Get("commodity_id") == "TX" && taifexHas {
				w.Write(fut)
				return
			}
			w.Write(fut[:bytes.IndexByte(fut, '\n')+1]) // header only
		case "/cht/3/optDataDown":
			if r.Method == "POST" && f.Get("commodity_id") == "TXO" && taifexHas {
				w.Write(opt)
				return
			}
			w.Write(opt[:bytes.IndexByte(opt, '\n')+1])
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ex.Close)
	return ex
}

// client sends every request to the test server, keeping the original Host
// so the per-host delay and the handler see the real exchange host.
func (ex *exchange) client() *http.Client {
	target, _ := url.Parse(ex.URL)
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		r = r.Clone(r.Context())
		r.Host = r.URL.Host
		r.URL.Scheme, r.URL.Host = target.Scheme, target.Host
		return http.DefaultTransport.RoundTrip(r)
	})}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func (ex *exchange) override(path string, body []byte) {
	ex.mu.Lock()
	defer ex.mu.Unlock()
	ex.overrides[path] = body
}

func testConfig(t *testing.T, dir string) *market.Config {
	t.Helper()
	cfg, err := market.ParseConfig(`
[output]
db      = '` + filepath.Join(dir, "data", "quotes.db") + `'
log_dir = '` + filepath.Join(dir, "logs") + `'

[chart]
dir = '` + filepath.Join(dir, "charts") + `'

[crawl]
per_host_delay = "0s"
retries = 0

[stocks]
twse = ["2330"]
tpex = ["6223"]

[[futures]]
product = "TX"

[[options]]
product = "TXO"
`)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func date(s string) time.Time {
	t, _ := time.ParseInLocation("2006-01-02", s, taipei)
	return t
}

func doRun(t *testing.T, ex *exchange, cfg *market.Config, dry bool) (Stats, string) {
	t.Helper()
	var out bytes.Buffer
	stats, err := run(context.Background(), cfg, runOptions{
		DryRun: dry, From: date("2026-09-28"), To: date("2026-10-05"),
		Out: &out, Loc: taipei, Client: ex.client(),
		Now: func() time.Time { return time.Date(2026, 10, 5, 16, 0, 0, 0, taipei) },
	})
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out.String())
	}
	return stats, out.String()
}

func byName(s Stats) map[string]*DatasetStats {
	m := map[string]*DatasetStats{}
	for _, d := range s.Datasets {
		m[d.Name] = d
	}
	return m
}

func TestRunTwiceIsIdempotent(t *testing.T) {
	ex := newExchange(t)
	dir := t.TempDir()
	cfg := testConfig(t, dir)

	s1, out := doRun(t, ex, cfg, false)
	d := byName(s1)
	// Stocks: September has 20 trading days; October is empty in the sample.
	if d["上市 2330"].New != 20 || d["上市 2330"].Requests != 2 || d["上市 2330"].NoData != 1 {
		t.Errorf("2330 = %+v\n%s", d["上市 2330"], out)
	}
	if d["上櫃 6223"].New != 20 {
		t.Errorf("6223 = %+v", d["上櫃 6223"])
	}
	// TX: nearest 2 months x 2 sessions, no spreads. Requests: Sep 28-30, Oct 1-5.
	if d["期貨 TX"].New != 4 || d["期貨 TX"].Requests != 2 || d["期貨 TX"].NoData != 1 {
		t.Errorf("TX = %+v", d["期貨 TX"])
	}
	// TXO sample: strikes 42800 and 42900 of one weekly expiry, both within 5%.
	if d["選擇權 TXO"].New != 6 {
		t.Errorf("TXO = %+v", d["選擇權 TXO"])
	}
	if s1.New != 50 || s1.Updated != 0 || s1.Failed != 0 || s1.EndReason != runlog.Completed {
		t.Errorf("run 1 = new %d updated %d failed %d %s\n%s", s1.New, s1.Updated, s1.Failed, s1.EndReason, out)
	}
	if !strings.Contains(out, "期貨 TX: 請求 2、筆數 4、新增 4") {
		t.Errorf("summary:\n%s", out)
	}

	s2, out := doRun(t, ex, cfg, false)
	if s2.New != 0 || s2.Updated != 0 || s2.Unchanged != 50 {
		t.Errorf("run 2 = new %d updated %d unchanged %d\n%s", s2.New, s2.Updated, s2.Unchanged, out)
	}

	st, err := market.OpenStoreReadOnly(cfg.Output.DB)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for table, want := range map[string]int{"stock_daily": 40, "futures_daily": 4, "options_daily": 6, "runs": 2} {
		if n, _ := st.Count(table); n != want {
			t.Errorf("%s rows = %d, want %d", table, n, want)
		}
	}
	// The night session is a separate row of the same trading date.
	var regular, night float64
	st.DB().QueryRow(`SELECT close FROM futures_daily WHERE contract_month='202610' AND session='regular'`).Scan(&regular)
	st.DB().QueryRow(`SELECT close FROM futures_daily WHERE contract_month='202610' AND session='after_hours'`).Scan(&night)
	if regular != 49949 || night != 49346 {
		t.Errorf("TX 202610 regular=%v night=%v", regular, night)
	}
	var vol int64
	st.DB().QueryRow(`SELECT volume FROM stock_daily WHERE symbol='6223' AND date='2026-09-01'`).Scan(&vol)
	if vol != 1649000 {
		t.Errorf("6223 volume = %d shares", vol)
	}
	var statsJSON string
	st.DB().QueryRow(`SELECT stats_json FROM runs ORDER BY id LIMIT 1`).Scan(&statsJSON)
	if !strings.Contains(statsJSON, `"new":50`) {
		t.Errorf("stats_json = %s", statsJSON)
	}
	if logs, _ := filepath.Glob(filepath.Join(dir, "logs", "quotes_20261005_1600*.log")); len(logs) != 2 {
		t.Errorf("logs = %v", logs)
	}
	var name string
	st.DB().QueryRow(`SELECT name FROM stock_names WHERE symbol='6223'`).Scan(&name)
	if name != "旺矽" {
		t.Errorf("6223 name = %q", name)
	}
	// The K-line page is regenerated after each run.
	if s2.Chart == "" || !strings.Contains(out, "K 線網頁") {
		t.Errorf("chart not generated: %q\n%s", s2.Chart, out)
	}
	for _, f := range []string{"index.html", "data/catalog.js", "data/fut_TX.js", "data/stock_6223.js", "data/opt_TXO_202610W1.js"} {
		if _, err := os.Stat(filepath.Join(cfg.Chart.Dir, filepath.FromSlash(f))); err != nil {
			t.Errorf("chart file %s: %v", f, err)
		}
	}

	// The exchanges were asked with their real hosts.
	hosts := strings.Join(ex.requests, "\n")
	for _, h := range []string{"GET www.twse.com.tw/exchangeReport/STOCK_DAY", "GET www.tpex.org.tw/www/zh-tw/afterTrading/tradingStock", "POST www.taifex.com.tw/cht/3/futDataDown", "POST www.taifex.com.tw/cht/3/optDataDown"} {
		if !strings.Contains(hosts, h) {
			t.Errorf("no request %q in:\n%s", h, hosts)
		}
	}
}

func TestRunDetectsUpdate(t *testing.T) {
	ex := newExchange(t)
	cfg := testConfig(t, t.TempDir())
	doRun(t, ex, cfg, false)

	// Settlement data revised after the first run.
	fut := fixture(t, "taifex_fut_TX_20261005.csv")
	ex.override("/cht/3/futDataDown", bytes.Replace(fut, []byte(",49944,108339,"), []byte(",49944,108400,"), 1))
	s, out := doRun(t, ex, cfg, false)
	if d := byName(s)["期貨 TX"]; d.Updated != 1 || d.Unchanged != 3 {
		t.Errorf("TX = %+v\n%s", d, out)
	}
}

func TestDryRun(t *testing.T) {
	ex := newExchange(t)
	dir := t.TempDir()
	cfg := testConfig(t, dir)
	s, out := doRun(t, ex, cfg, true)
	if s.New != 50 || !strings.Contains(out, "dry run") {
		t.Errorf("dry run new = %d\n%s", s.New, out)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("dry run wrote %v", entries)
	}

	// With an existing database, a dry run compares against it.
	doRun(t, ex, cfg, false)
	s, _ = doRun(t, ex, cfg, true)
	if s.New != 0 || s.Unchanged != 50 {
		t.Errorf("dry run against db: new %d unchanged %d", s.New, s.Unchanged)
	}
}

func TestFormatChangeIsSavedAndSkipped(t *testing.T) {
	ex := newExchange(t)
	dir := t.TempDir()
	cfg := testConfig(t, dir)
	ex.override("/www/zh-tw/afterTrading/tradingStock", []byte(`{"stat":"ok","tables":[{"fields":["日期","成交量"],"data":[["115/09/01","1"]]}]}`))
	ex.override("/cht/3/optDataDown", []byte("<html><body>系統維護中</body></html>"))

	s, out := doRun(t, ex, cfg, false)
	// TPEx Sep + Oct and TXO Sep 28-30 + Oct 1-5 all get the bad responses.
	if s.Failed != 4 || byName(s)["上市 2330"].New != 20 || byName(s)["期貨 TX"].New != 4 {
		t.Errorf("failed=%d datasets=%+v\n%s", s.Failed, s.Datasets, out)
	}
	raw, _ := filepath.Glob(filepath.Join(dir, "logs", "raw", "*"))
	if len(raw) != 4 {
		t.Errorf("no raw responses saved\n%s", out)
	}
	if !strings.Contains(out, "response saved to") {
		t.Errorf("log does not point at the saved response:\n%s", out)
	}
	st, _ := market.OpenStoreReadOnly(cfg.Output.DB)
	defer st.Close()
	if n, _ := st.Count("options_daily"); n != 0 {
		t.Errorf("options rows written from a bad response: %d", n)
	}
}

func TestWarnsWhenNothingFound(t *testing.T) {
	ex := newExchange(t)
	cfg := testConfig(t, t.TempDir())
	cfg.Stocks.TPEx = append(cfg.Stocks.TPEx, "2330") // a listed stock under tpex
	s, out := doRun(t, ex, cfg, false)
	if len(s.Warnings) != 1 || !strings.Contains(s.Warnings[0], "上櫃 2330") || !strings.Contains(out, "警告") {
		t.Errorf("warnings = %v\n%s", s.Warnings, out)
	}
	if s.Failed != 0 {
		t.Errorf("no-data is not a failure: %d", s.Failed)
	}
}

func TestChartCommand(t *testing.T) {
	ex := newExchange(t)
	dir := t.TempDir()
	cfg := testConfig(t, dir)
	cfg.Chart.Auto = false
	s, _ := doRun(t, ex, cfg, false)
	if s.Chart != "" {
		t.Errorf("chart generated with auto = false")
	}
	cfgFile := filepath.Join(dir, "q.toml")
	os.WriteFile(cfgFile, []byte("[output]\ndb = '"+cfg.Output.DB+"'\n[chart]\ndir = '"+cfg.Chart.Dir+"'\n[stocks]\ntwse = [\"2330\"]\n"), 0o644)
	var out, errb bytes.Buffer
	if code := runMain([]string{"chart", "-c", cfgFile}, &out, &errb); code != exitOK {
		t.Fatalf("chart = %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "4 個資料檔") {
		t.Errorf("output: %s", out.String())
	}
}

func TestRunAborted(t *testing.T) {
	ex := newExchange(t)
	cfg := testConfig(t, t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s, err := run(ctx, cfg, runOptions{From: date("2026-10-01"), To: date("2026-10-05"), Loc: taipei, Client: ex.client()})
	if err != nil {
		t.Fatal(err)
	}
	if s.EndReason != runlog.Aborted {
		t.Errorf("reason = %s", s.EndReason)
	}
}

func TestDateRange(t *testing.T) {
	now := time.Date(2026, 10, 24, 15, 30, 0, 0, taipei)
	from, to, err := dateRange("", "", 5, now)
	if err != nil || from.Format("2006-01-02") != "2026-10-19" || to.Format("2006-01-02") != "2026-10-24" {
		t.Errorf("default = %v %v %v", from, to, err)
	}
	from, to, _ = dateRange("2026-09-01", "2026-09-30", 5, now)
	if from.Format("2006-01-02") != "2026-09-01" || to.Format("2006-01-02") != "2026-09-30" {
		t.Errorf("explicit = %v %v", from, to)
	}
	from, _, _ = dateRange("", "2026-10-10", 3, now)
	if from.Format("2006-01-02") != "2026-10-07" {
		t.Errorf("to only = %v", from)
	}
	for _, bad := range [][2]string{{"2026-10-10", "2026-10-01"}, {"10/01", ""}, {"", "x"}} {
		if _, _, err := dateRange(bad[0], bad[1], 5, now); err == nil {
			t.Errorf("dateRange(%q, %q) accepted", bad[0], bad[1])
		}
	}
}

func TestCLI(t *testing.T) {
	var out, errb bytes.Buffer
	if code := runMain([]string{"validate", "-c", filepath.Join("..", "..", "quotes.example.toml")}, &out, &errb); code != exitOK {
		t.Errorf("validate example = %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "期貨 TX") || !strings.Contains(out.String(), "選擇權 TXO") {
		t.Errorf("validate output: %s", out.String())
	}
	bad := filepath.Join(t.TempDir(), "bad.toml")
	os.WriteFile(bad, []byte("[[futures]]\nproduct = \"T X\"\n"), 0o644)
	errb.Reset()
	if code := runMain([]string{"validate", "-c", bad}, &out, &errb); code != exitConfig || !strings.Contains(errb.String(), "futures[0].product") {
		t.Errorf("validate bad = %d: %s", code, errb.String())
	}
	if code := runMain([]string{"run", "-c", filepath.Join("..", "..", "quotes.example.toml"), "--from", "2026-10-10", "--to", "2026-10-01"}, &out, &errb); code != exitConfig {
		t.Errorf("reversed range = %d", code)
	}
	// quotes chart needs an existing database.
	cfgFile := filepath.Join(t.TempDir(), "q.toml")
	os.WriteFile(cfgFile, []byte("[output]\ndb = '"+filepath.Join(t.TempDir(), "none.db")+"'\n[stocks]\ntwse = [\"2330\"]\n"), 0o644)
	errb.Reset()
	if code := runMain([]string{"chart", "-c", cfgFile}, &out, &errb); code != exitAborted || !strings.Contains(errb.String(), "quotes run") {
		t.Errorf("chart without db = %d: %s", code, errb.String())
	}
	if code := runMain([]string{"nope"}, &out, &errb); code != exitConfig {
		t.Errorf("unknown command = %d", code)
	}
}
