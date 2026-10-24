package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/elvis3434/gleaner24/internal/crawler"
	"github.com/elvis3434/gleaner24/internal/lock"
	"github.com/elvis3434/gleaner24/internal/market"
	"github.com/elvis3434/gleaner24/internal/market/chart"
	"github.com/elvis3434/gleaner24/internal/market/futures"
	"github.com/elvis3434/gleaner24/internal/market/options"
	"github.com/elvis3434/gleaner24/internal/market/stock"
	"github.com/elvis3434/gleaner24/internal/runlog"
)

// maxBody allows a month of a busy options product in one response.
const maxBody = 64 << 20

type runOptions struct {
	DryRun   bool
	From, To time.Time
	Out      io.Writer
	Loc      *time.Location
	Now      func() time.Time
	// Client overrides the HTTP client (tests point it at a local server).
	Client *http.Client
}

// DatasetStats counts one configured item (a stock, a futures or options
// product).
type DatasetStats struct {
	Name      string `json:"name"`
	Requests  int    `json:"requests"`
	Rows      int    `json:"rows"`
	New       int    `json:"new"`
	Updated   int    `json:"updated"`
	Unchanged int    `json:"unchanged"`
	NoData    int    `json:"no_data"` // requests answered without rows
}

// Stats summarises a run; it is stored as runs.stats_json.
type Stats struct {
	From            string           `json:"from"`
	To              string           `json:"to"`
	StartedAt       string           `json:"started_at"`
	FinishedAt      string           `json:"finished_at"`
	DurationSeconds float64          `json:"duration_seconds"`
	EndReason       string           `json:"end_reason"`
	DryRun          bool             `json:"dry_run"`
	Requests        int              `json:"requests"`
	New             int              `json:"new"`
	Updated         int              `json:"updated"`
	Unchanged       int              `json:"unchanged"`
	Failed          int              `json:"failed"`
	Failures        []runlog.Failure `json:"failures"`
	Warnings        []string         `json:"warnings"`
	Chart           string           `json:"chart,omitempty"` // index.html written by this run
	Datasets        []*DatasetStats  `json:"datasets"`
}

type runner struct {
	cfg   *market.Config
	opts  runOptions
	fetch *crawler.Fetcher
	st    *market.Store // nil in a dry run without a database
	log   *runlog.Log
	stats Stats
	now   time.Time
}

func run(ctx context.Context, cfg *market.Config, opts runOptions) (Stats, error) {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	r := &runner{
		cfg:   cfg,
		opts:  opts,
		fetch: crawler.New(cfg.Crawl.UserAgent, cfg.Crawl.Timeout, cfg.Crawl.PerHostDelay, cfg.Crawl.Retries),
		now:   opts.Now().In(opts.Loc),
	}
	r.fetch.MaxBody = maxBody
	if opts.Client != nil {
		r.fetch.Client = opts.Client
	}
	r.stats = Stats{
		From: opts.From.Format("2006-01-02"), To: opts.To.Format("2006-01-02"),
		StartedAt: r.now.Format(time.RFC3339), DryRun: opts.DryRun, Failures: []runlog.Failure{}, Warnings: []string{},
	}

	var err error
	if !opts.DryRun {
		l, err := lock.Acquire(LockPath(cfg))
		if err != nil {
			return r.stats, err
		}
		defer l.Release()
	}
	if opts.DryRun {
		r.st, err = market.OpenStoreReadOnly(cfg.Output.DB)
		if errors.Is(err, os.ErrNotExist) {
			r.st, err = nil, nil
		}
		r.log = runlog.New(opts.Out, r.now)
	} else {
		r.st, err = market.OpenStore(cfg.Output.DB)
		if err == nil {
			r.log, err = runlog.CreateNamed(cfg.Output.LogDir, "quotes", r.now, opts.Out)
		}
	}
	if err != nil {
		if r.st != nil {
			r.st.Close()
		}
		return r.stats, err
	}
	if r.st != nil {
		defer r.st.Close()
	}
	defer r.log.Close()

	runID, err := r.st.StartRun(r.stats.StartedAt, filepath.ToSlash(r.log.Path()))
	if err != nil {
		return r.stats, err
	}
	if opts.DryRun {
		r.log.Printf("dry run %s ~ %s: the database will not be written", r.stats.From, r.stats.To)
	} else {
		r.log.Printf("run %d: %s ~ %s, log %s", runID, r.stats.From, r.stats.To, r.log.Path())
	}

	reason, fatal := runlog.Completed, error(nil)
	for _, job := range r.jobs() {
		if err := job(ctx); err != nil {
			if ctx.Err() != nil {
				reason = runlog.Aborted
				r.log.Printf("stopping: interrupted")
			} else {
				reason, fatal = runlog.Aborted, err
				r.log.Printf("stopping: %v", err)
			}
			break
		}
	}
	if reason == runlog.Completed && !opts.DryRun && cfg.Chart.Auto {
		r.chart()
	}

	end := opts.Now().In(opts.Loc)
	s := &r.stats
	s.FinishedAt = end.Format(time.RFC3339)
	s.DurationSeconds = end.Sub(r.now).Round(time.Millisecond).Seconds()
	s.EndReason = reason
	s.Requests = r.fetch.Requests()
	for _, d := range s.Datasets {
		s.New += d.New
		s.Updated += d.Updated
		s.Unchanged += d.Unchanged
		// Every request answered "no data": usually a wrong code or market
		// (a listed stock under tpex, say) rather than a quiet period.
		if reason == runlog.Completed && d.Requests > 0 && d.NoData == d.Requests {
			w := fmt.Sprintf("%s: 整段期間都沒有資料，請確認代號與市場（上市 / 上櫃）是否正確", d.Name)
			s.Warnings = append(s.Warnings, w)
			r.log.Printf("WARN %s", w)
		}
	}
	r.log.WriteString(summary(*s))
	statsJSON, _ := json.Marshal(s)
	if err := r.st.FinishRun(runID, s.FinishedAt, s.EndReason, string(statsJSON)); err != nil && fatal == nil {
		fatal = err
	}
	return *s, fatal
}

// jobs lists the work of the run in a fixed order. A job returns an error
// only when the run must stop (interruption or a database failure).
func (r *runner) jobs() []func(context.Context) error {
	var jobs []func(context.Context) error
	for _, mkt := range []struct {
		code, label string
		symbols     []string
	}{{stock.TWSE, "上市", r.cfg.Stocks.TWSE}, {stock.TPEx, "上櫃", r.cfg.Stocks.TPEx}} {
		for _, sym := range mkt.symbols {
			jobs = append(jobs, func(ctx context.Context) error { return r.stock(ctx, mkt.code, mkt.label, sym) })
		}
	}
	for _, f := range r.cfg.Futures {
		jobs = append(jobs, func(ctx context.Context) error { return r.futures(ctx, f) })
	}
	for _, o := range r.cfg.Options {
		jobs = append(jobs, func(ctx context.Context) error { return r.options(ctx, o) })
	}
	return jobs
}

func (r *runner) dataset(name string) *DatasetStats {
	d := &DatasetStats{Name: name}
	r.stats.Datasets = append(r.stats.Datasets, d)
	return d
}

// fail records a failed request. Responses with an unexpected format are
// saved under logs/raw/ so they can be inspected and turned into tests.
func (r *runner) fail(what, rawName string, body []byte, err error) {
	r.stats.Failed++
	msg := err.Error()
	if errors.Is(err, market.ErrFormat) && body != nil {
		if p, werr := r.saveRaw(rawName, body); werr == nil {
			msg += " (response saved to " + p + ")"
		}
	}
	r.stats.Failures = append(r.stats.Failures, runlog.Failure{URL: what, Error: msg})
	r.log.Printf("FAIL %s: %s", what, msg)
}

func (r *runner) saveRaw(name string, body []byte) (string, error) {
	dir := filepath.Join(r.cfg.Output.LogDir, "raw")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	p := filepath.Join(dir, r.now.Format("20060102_150405_")+name)
	return p, os.WriteFile(p, body, 0o644)
}

// LockPath is the lock file that keeps quotes runs (and chart generation)
// from overlapping.
func LockPath(cfg *market.Config) string {
	return filepath.Join(filepath.Dir(cfg.Output.DB), "quotes.lock")
}

// stopErr returns the context error when a failed request was caused by
// the run being interrupted, and nil for an ordinary request failure.
func stopErr(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}

func (r *runner) write(d *DatasetStats, table string, rows []market.Row) error {
	c, err := r.st.UpsertAll(table, rows, r.now.Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("write %s (%s): %w", table, d.Name, err)
	}
	d.Rows += len(rows)
	d.New += c.New
	d.Updated += c.Updated
	d.Unchanged += c.Unchanged
	return nil
}

func (r *runner) stock(ctx context.Context, mkt, label, symbol string) error {
	d := r.dataset(label + " " + symbol)
	for _, month := range market.Months(r.opts.From, r.opts.To) {
		u := stock.URL(mkt, symbol, month)
		d.Requests++
		resp, err := r.fetch.Get(ctx, u)
		if err != nil {
			if stop := stopErr(ctx); stop != nil {
				return stop
			}
			r.fail(u, "", nil, err)
			continue
		}
		rows, err := stock.Parse(mkt, symbol, resp.Body)
		if errors.Is(err, market.ErrNoData) {
			d.NoData++
			r.log.Printf("%s %s: %v", d.Name, month.Format("2006-01"), err)
			continue
		}
		if err != nil {
			r.fail(u, fmt.Sprintf("%s_%s_%s.json", mkt, symbol, month.Format("200601")), resp.Body, err)
			continue
		}
		if err := r.st.SetStockName(symbol, mkt, stock.Name(mkt, symbol, resp.Body), r.now.Format(time.RFC3339)); err != nil {
			return fmt.Errorf("write stock name %s: %w", symbol, err)
		}
		recs := make([]market.Row, len(rows))
		for i, row := range rows {
			recs[i] = row.Row()
		}
		if err := r.write(d, stock.Table, recs); err != nil {
			return err
		}
		r.log.Printf("%s %s: %d rows", d.Name, month.Format("2006-01"), len(rows))
	}
	return nil
}

func inSpan(date string, s market.Span) bool {
	return date >= s.From.Format("2006-01-02") && date <= s.To.Format("2006-01-02")
}

// taifex fetches product over the run's range month by month and hands
// each parsed chunk to handle.
func (r *runner) taifex(ctx context.Context, d *DatasetStats, endpoint, product, kind string, handle func(body []byte, span market.Span) ([]market.Row, error)) error {
	for _, span := range market.MonthChunks(r.opts.From, r.opts.To) {
		what := fmt.Sprintf("%s %s %s~%s", endpoint, product, span.From.Format("2006-01-02"), span.To.Format("2006-01-02"))
		d.Requests++
		resp, err := r.fetch.Post(ctx, endpoint, market.TaifexForm(product, span.From, span.To))
		if err != nil {
			if stop := stopErr(ctx); stop != nil {
				return stop
			}
			r.fail(what, "", nil, err)
			continue
		}
		rows, err := handle(resp.Body, span)
		if errors.Is(err, market.ErrNoData) {
			d.NoData++
			r.log.Printf("%s %s~%s: no data", d.Name, span.From.Format("01-02"), span.To.Format("01-02"))
			continue
		}
		if err != nil {
			r.fail(what, fmt.Sprintf("%s_%s_%s.csv", kind, product, span.From.Format("20060102")), resp.Body, err)
			continue
		}
		if err := r.write(d, kind+"_daily", rows); err != nil {
			return err
		}
		r.log.Printf("%s %s~%s: %d rows", d.Name, span.From.Format("01-02"), span.To.Format("01-02"), len(rows))
	}
	return nil
}

func (r *runner) futures(ctx context.Context, f market.Futures) error {
	d := r.dataset("期貨 " + f.Product)
	return r.taifex(ctx, d, market.FuturesDownloadURL, f.Product, "futures", func(body []byte, span market.Span) ([]market.Row, error) {
		rows, err := futures.Parse(body, f.Product)
		if err != nil {
			return nil, err
		}
		var inRange []futures.Daily
		for _, row := range rows {
			if inSpan(row.Date, span) {
				inRange = append(inRange, row)
			}
		}
		var recs []market.Row
		for _, row := range futures.Select(inRange, f.Months, f.IncludeSpreads, f.Sessions) {
			recs = append(recs, row.Row())
		}
		return recs, nil
	})
}

func (r *runner) options(ctx context.Context, o market.Options) error {
	d := r.dataset("選擇權 " + o.Product)
	sel := options.Selection{Expiries: o.Expiries, Weekly: o.Weekly, StrikeRange: o.StrikeRange, Sessions: o.Sessions}
	return r.taifex(ctx, d, market.OptionsDownloadURL, o.Product, "options", func(body []byte, span market.Span) ([]market.Row, error) {
		rows, err := options.Parse(body, o.Product)
		if err != nil {
			return nil, err
		}
		var inRange []options.Daily
		for _, row := range rows {
			if inSpan(row.Date, span) {
				inRange = append(inRange, row)
			}
		}
		var recs []market.Row
		for _, row := range options.Select(inRange, sel) {
			recs = append(recs, row.Row())
		}
		return recs, nil
	})
}

// chart regenerates the K-line page; a failure is reported but does not
// abort the run, since the data itself is already stored.
func (r *runner) chart() {
	res, err := chart.Generate(r.st.DB(), r.cfg.Chart.Dir, r.now)
	if err != nil {
		r.fail("chart "+r.cfg.Chart.Dir, "", nil, err)
		return
	}
	r.stats.Chart = res.Index
	r.log.Printf("chart: %s (%d data files)", res.Index, res.DataFiles)
}

var reasonText = map[string]string{
	runlog.Completed: "完成",
	runlog.Aborted:   "中斷",
}

func summary(s Stats) string {
	var b strings.Builder
	row := func(k string, v any) { fmt.Fprintf(&b, "%s %v\n", runlog.Pad(k, 12), v) }
	b.WriteString("\n===== 行情收集摘要 =====\n")
	if s.DryRun {
		b.WriteString("（dry run：未寫入資料庫，新增 / 更新為與現有資料比對的結果）\n")
	}
	row("資料期間", s.From+" ~ "+s.To)
	row("開始時間", s.StartedAt)
	row("結束時間", s.FinishedAt)
	row("耗時", time.Duration(s.DurationSeconds*float64(time.Second)).Round(time.Second))
	row("結束原因", fmt.Sprintf("%s (%s)", reasonText[s.EndReason], s.EndReason))
	row("請求數", s.Requests)
	row("新增", s.New)
	row("更新", s.Updated)
	row("未變", s.Unchanged)
	row("失敗", s.Failed)
	for _, f := range s.Failures {
		fmt.Fprintf(&b, "  - %s: %s\n", f.URL, f.Error)
	}
	if len(s.Warnings) > 0 {
		row("警告", len(s.Warnings))
		for _, w := range s.Warnings {
			fmt.Fprintf(&b, "  - %s\n", w)
		}
	}
	if s.Chart != "" {
		row("K 線網頁", s.Chart)
	}
	b.WriteString("各項目:\n")
	for _, d := range s.Datasets {
		fmt.Fprintf(&b, "  - %s: 請求 %d、筆數 %d、新增 %d、更新 %d、未變 %d", d.Name, d.Requests, d.Rows, d.New, d.Updated, d.Unchanged)
		if d.NoData > 0 {
			fmt.Fprintf(&b, "、無資料 %d", d.NoData)
		}
		b.WriteString("\n")
	}
	return b.String()
}
