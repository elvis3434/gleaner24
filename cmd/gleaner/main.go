// Command gleaner collects pages matching keyword queries from configured
// feeds and sites and archives them as Markdown. See docs/SPEC.md.
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

	"github.com/elvis3434/gleaner24/internal/config"
	"github.com/elvis3434/gleaner24/internal/runlog"
)

// Exit codes (SPEC: CLI).
const (
	exitOK      = 0
	exitConfig  = 1
	exitAborted = 2
)

const usage = `gleaner - collect and archive pages matching keyword queries

Usage:
  gleaner run [-c config.toml] [--dry-run]
  gleaner validate [-c config.toml]
  gleaner test-query [--case-sensitive] "<expression>" <file-or-URL>

Exit codes: 0 success (including some failed pages), 1 config error,
2 run aborted (for example the database cannot be opened).
`

// location returns the Asia/Taipei time zone used for all timestamps.
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
	case "test-query":
		return cmdTestQuery(args[1:], stdout, stderr)
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

func loadConfig(path string, stderr io.Writer) (*config.Config, bool) {
	cfg, err := config.Load(path)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", path, err)
		return nil, false
	}
	return cfg, true
}

func cmdValidate(args []string, stdout, stderr io.Writer) int {
	fs := newFlags("validate", stderr)
	cfgPath := fs.String("c", "config.toml", "config file")
	if err := fs.Parse(args); err != nil {
		return exitConfig
	}
	cfg, ok := loadConfig(*cfgPath, stderr)
	if !ok {
		return exitConfig
	}
	fmt.Fprintf(stdout, "%s: OK (%d feeds, %d sites, %d topics)\n",
		*cfgPath, len(cfg.Sources.Feeds), len(cfg.Sources.Sites), len(cfg.Topics))
	for _, t := range cfg.Topics {
		fmt.Fprintf(stdout, "  topic %s: %s\n", t.Name, t.Query)
	}
	return exitOK
}

func cmdRun(args []string, stdout, stderr io.Writer) int {
	fs := newFlags("run", stderr)
	cfgPath := fs.String("c", "config.toml", "config file")
	dryRun := fs.Bool("dry-run", false, "fetch and match but write nothing; print what would be archived")
	if err := fs.Parse(args); err != nil {
		return exitConfig
	}
	cfg, ok := loadConfig(*cfgPath, stderr)
	if !ok {
		return exitConfig
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	stats, err := run(ctx, cfg, runOptions{DryRun: *dryRun, Out: stdout, Loc: location()})
	if err != nil {
		fmt.Fprintf(stderr, "run: %v\n", err)
		return exitAborted
	}
	if stats.EndReason == runlog.Aborted {
		return exitAborted
	}
	return exitOK
}
