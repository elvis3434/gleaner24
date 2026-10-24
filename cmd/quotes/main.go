// Command quotes collects daily Taiwan stock, futures and options data from
// the exchanges' free public downloads into SQLite. See docs/SPEC-quotes.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // Windows has no system time zone database

	"github.com/elvis3434/gleaner24/internal/lock"
	"github.com/elvis3434/gleaner24/internal/market"
	"github.com/elvis3434/gleaner24/internal/market/chart"
	"github.com/elvis3434/gleaner24/internal/runlog"
)

// Exit codes, as for gleaner.
const (
	exitOK      = 0
	exitConfig  = 1
	exitAborted = 2
)

const usage = `quotes - collect daily Taiwan stock, futures and options data

Usage:
  quotes run [-c quotes.toml] [--from YYYY-MM-DD] [--to YYYY-MM-DD] [--dry-run]
  quotes validate [-c quotes.toml]
  quotes chart [-c quotes.toml]      regenerate the K-line page from the database

Without --from/--to, run collects the last lookback_days calendar days.
Exit codes: 0 success (including some failed requests), 1 config error,
2 run aborted (for example the database cannot be opened).
`

func location() *time.Location {
	loc, err := time.LoadLocation("Asia/Taipei")
	if err != nil {
		return time.FixedZone("CST", 8*3600)
	}
	return loc
}

func main() {
	os.Exit(runMain(os.Args[1:], os.Stdout, os.Stderr))
}

func runMain(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitConfig
	}
	switch args[0] {
	case "run":
		return cmdRun(args[1:], stdout, stderr)
	case "validate":
		return cmdValidate(args[1:], stdout, stderr)
	case "chart":
		return cmdChart(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return exitOK
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[0], usage)
		return exitConfig
	}
}

func newFlags(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

func loadConfig(path string, stderr io.Writer) (*market.Config, bool) {
	cfg, err := market.LoadConfig(path)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", path, err)
		return nil, false
	}
	return cfg, true
}

func cmdValidate(args []string, stdout, stderr io.Writer) int {
	fs := newFlags("validate", stderr)
	cfgPath := fs.String("c", "quotes.toml", "config file")
	if err := fs.Parse(args); err != nil {
		return exitConfig
	}
	cfg, ok := loadConfig(*cfgPath, stderr)
	if !ok {
		return exitConfig
	}
	fmt.Fprintf(stdout, "%s: OK\n", *cfgPath)
	fmt.Fprintf(stdout, "  上市: %v\n  上櫃: %v\n", cfg.Stocks.TWSE, cfg.Stocks.TPEx)
	for _, f := range cfg.Futures {
		fmt.Fprintf(stdout, "  期貨 %s: months=%d spreads=%v sessions=%v\n", f.Product, f.Months, f.IncludeSpreads, f.Sessions)
	}
	for _, o := range cfg.Options {
		fmt.Fprintf(stdout, "  選擇權 %s: expiries=%d weekly=%v strike_range=%g sessions=%v\n", o.Product, o.Expiries, o.Weekly, o.StrikeRange, o.Sessions)
	}
	return exitOK
}

func cmdRun(args []string, stdout, stderr io.Writer) int {
	fs := newFlags("run", stderr)
	cfgPath := fs.String("c", "quotes.toml", "config file")
	from := fs.String("from", "", "first date to collect (YYYY-MM-DD)")
	to := fs.String("to", "", "last date to collect (YYYY-MM-DD); default today")
	dryRun := fs.Bool("dry-run", false, "fetch and parse but do not write the database")
	if err := fs.Parse(args); err != nil {
		return exitConfig
	}
	cfg, ok := loadConfig(*cfgPath, stderr)
	if !ok {
		return exitConfig
	}
	loc := location()
	start, end, err := dateRange(*from, *to, cfg.Crawl.LookbackDays, time.Now().In(loc))
	if err != nil {
		fmt.Fprintf(stderr, "run: %v\n", err)
		return exitConfig
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	stats, err := run(ctx, cfg, runOptions{DryRun: *dryRun, From: start, To: end, Out: stdout, Loc: loc})
	if err != nil {
		fmt.Fprintf(stderr, "run: %v\n", err)
		return exitAborted
	}
	if stats.EndReason == runlog.Aborted {
		return exitAborted
	}
	return exitOK
}

func cmdChart(args []string, stdout, stderr io.Writer) int {
	fs := newFlags("chart", stderr)
	cfgPath := fs.String("c", "quotes.toml", "config file")
	if err := fs.Parse(args); err != nil {
		return exitConfig
	}
	cfg, ok := loadConfig(*cfgPath, stderr)
	if !ok {
		return exitConfig
	}
	if _, err := os.Stat(cfg.Output.DB); err != nil {
		fmt.Fprintf(stderr, "chart: %v (run quotes run first)\n", err)
		return exitAborted
	}
	l, err := lock.Acquire(LockPath(cfg))
	if err != nil {
		fmt.Fprintf(stderr, "chart: %v\n", err)
		return exitAborted
	}
	defer l.Release()
	// Opened read-write so that a database from an older version is
	// migrated (e.g. stock_names, added in stage Q3) before it is read.
	st, err := market.OpenStore(cfg.Output.DB)
	if err != nil {
		fmt.Fprintf(stderr, "chart: %v\n", err)
		return exitAborted
	}
	defer st.Close()
	res, err := chart.Generate(st.DB(), cfg.Chart.Dir, time.Now().In(location()))
	if err != nil {
		fmt.Fprintf(stderr, "chart: %v\n", err)
		return exitAborted
	}
	fmt.Fprintf(stdout, "K 線網頁：%s（%d 個資料檔）\n", res.Index, res.DataFiles)
	return exitOK
}

// dateRange resolves --from/--to: the end defaults to today and the start
// to lookback days before the end.
func dateRange(from, to string, lookback int, now time.Time) (time.Time, time.Time, error) {
	loc := now.Location()
	end := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	if to != "" {
		t, err := time.ParseInLocation("2006-01-02", to, loc)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("--to: want YYYY-MM-DD, got %q", to)
		}
		end = t
	}
	start := end.AddDate(0, 0, -lookback)
	if from != "" {
		t, err := time.ParseInLocation("2006-01-02", from, loc)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("--from: want YYYY-MM-DD, got %q", from)
		}
		start = t
	}
	if start.After(end) {
		return time.Time{}, time.Time{}, fmt.Errorf("--from %s is after --to %s", start.Format("2006-01-02"), end.Format("2006-01-02"))
	}
	return start, end, nil
}
