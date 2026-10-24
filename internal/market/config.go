package market

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/elvis3434/gleaner24/internal/config"
)

// Config is the quotes collector configuration (docs/SPEC-quotes.md).
type Config struct {
	Output  Output
	Crawl   Crawl
	Stocks  Stocks
	Futures []Futures
	Options []Options
}

type Output struct {
	DB     string
	LogDir string
}

type Crawl struct {
	UserAgent    string
	PerHostDelay time.Duration
	Timeout      time.Duration
	Retries      int
	LookbackDays int
}

type Stocks struct {
	TWSE []string // listed (上市)
	TPEx []string // OTC (上櫃)
}

type Futures struct {
	Product        string
	Months         int // nearest N contract months; 0 = all
	IncludeSpreads bool
	Sessions       []string
}

type Options struct {
	Product     string
	Expiries    int // nearest N expiries; 0 = all
	Weekly      bool
	StrikeRange float64 // fraction around the at-the-money strike; 0 = all
	Sessions    []string
}

// The raw* types mirror the TOML; pointers tell unset fields from zero
// values so that defaults apply per entry.
type rawConfig struct {
	Output struct {
		DB     string `toml:"db"`
		LogDir string `toml:"log_dir"`
	} `toml:"output"`
	Crawl struct {
		UserAgent    string           `toml:"user_agent"`
		PerHostDelay *config.Duration `toml:"per_host_delay"`
		Timeout      *config.Duration `toml:"timeout"`
		Retries      *int             `toml:"retries"`
		LookbackDays *int             `toml:"lookback_days"`
	} `toml:"crawl"`
	Stocks struct {
		TWSE []string `toml:"twse"`
		TPEx []string `toml:"tpex"`
	} `toml:"stocks"`
	Futures []struct {
		Product        string   `toml:"product"`
		Months         *int     `toml:"months"`
		IncludeSpreads bool     `toml:"include_spreads"`
		Sessions       []string `toml:"sessions"`
	} `toml:"futures"`
	Options []struct {
		Product     string   `toml:"product"`
		Expiries    *int     `toml:"expiries"`
		Weekly      *bool    `toml:"weekly"`
		StrikeRange *float64 `toml:"strike_range"`
		Sessions    []string `toml:"sessions"`
	} `toml:"options"`
}

// Defaults (SPEC-quotes: Config).
const (
	DefaultUserAgent    = "Gleaner/1.0 (personal archive)"
	DefaultPerHostDelay = 3 * time.Second
	DefaultTimeout      = 30 * time.Second
	DefaultRetries      = 2
	DefaultLookbackDays = 5
	DefaultMonths       = 2
	DefaultExpiries     = 2
	DefaultStrikeRange  = 0.05
)

var defaultSessions = []string{Regular, AfterHours}

var codeRE = regexp.MustCompile(`^[A-Za-z0-9]+$`)

// LoadConfig reads and validates a quotes config file. Validation problems
// are returned as *config.ValidationError, like the gleaner config.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseConfig(string(data))
}

// ParseConfig is LoadConfig for text in memory.
func ParseConfig(data string) (*Config, error) {
	var raw rawConfig
	md, err := toml.Decode(data, &raw)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	var p []string
	add := func(field, format string, args ...any) {
		p = append(p, field+": "+fmt.Sprintf(format, args...))
	}
	for _, k := range md.Undecoded() {
		add(k.String(), "unknown field")
	}

	cfg := &Config{
		Output: Output{DB: raw.Output.DB, LogDir: raw.Output.LogDir},
		Crawl: Crawl{
			UserAgent:    raw.Crawl.UserAgent,
			PerHostDelay: DefaultPerHostDelay,
			Timeout:      DefaultTimeout,
			Retries:      DefaultRetries,
			LookbackDays: DefaultLookbackDays,
		},
	}
	if cfg.Output.DB == "" {
		cfg.Output.DB = "./data/quotes.db"
	}
	if cfg.Output.LogDir == "" {
		cfg.Output.LogDir = "./logs"
	}
	if cfg.Crawl.UserAgent == "" {
		cfg.Crawl.UserAgent = DefaultUserAgent
	}
	if d := raw.Crawl.PerHostDelay; d != nil {
		cfg.Crawl.PerHostDelay = d.Duration
	}
	if d := raw.Crawl.Timeout; d != nil {
		cfg.Crawl.Timeout = d.Duration
	}
	if v := raw.Crawl.Retries; v != nil {
		cfg.Crawl.Retries = *v
	}
	if v := raw.Crawl.LookbackDays; v != nil {
		cfg.Crawl.LookbackDays = *v
	}
	if cfg.Crawl.PerHostDelay < 0 {
		add("crawl.per_host_delay", "must not be negative")
	}
	if cfg.Crawl.Timeout <= 0 {
		add("crawl.timeout", "must be positive")
	}
	if cfg.Crawl.Retries < 0 {
		add("crawl.retries", "must not be negative")
	}
	if cfg.Crawl.LookbackDays < 0 {
		add("crawl.lookback_days", "must not be negative")
	}

	symbols := map[string]string{}
	checkSymbols := func(field string, list []string) []string {
		var out []string
		for i, s := range list {
			s = strings.TrimSpace(s)
			f := fmt.Sprintf("%s[%d]", field, i)
			switch {
			case !codeRE.MatchString(s):
				add(f, "invalid stock code %q (letters and digits only)", s)
			case symbols[s] != "":
				add(f, "duplicate stock code %q (also in %s)", s, symbols[s])
			default:
				symbols[s] = field
				out = append(out, s)
			}
		}
		return out
	}
	cfg.Stocks.TWSE = checkSymbols("stocks.twse", raw.Stocks.TWSE)
	cfg.Stocks.TPEx = checkSymbols("stocks.tpex", raw.Stocks.TPEx)

	checkSessions := func(field string, list []string) []string {
		if len(list) == 0 {
			return slices.Clone(defaultSessions)
		}
		for _, s := range list {
			if s != Regular && s != AfterHours {
				add(field, "must be %q or %q, got %q", Regular, AfterHours, s)
			}
		}
		return list
	}
	products := map[string]string{}
	checkProduct := func(field, kind, product string) string {
		product = strings.ToUpper(strings.TrimSpace(product))
		switch {
		case !codeRE.MatchString(product):
			add(field, "invalid product code %q", product)
		case products[kind+product] != "":
			add(field, "duplicate product %q (also in %s)", product, products[kind+product])
		default:
			products[kind+product] = field
		}
		return product
	}

	for i, f := range raw.Futures {
		field := fmt.Sprintf("futures[%d]", i)
		fut := Futures{
			Product:        checkProduct(field+".product", "F", f.Product),
			Months:         DefaultMonths,
			IncludeSpreads: f.IncludeSpreads,
			Sessions:       checkSessions(field+".sessions", f.Sessions),
		}
		if f.Months != nil {
			fut.Months = *f.Months
		}
		if fut.Months < 0 {
			add(field+".months", "must not be negative")
		}
		cfg.Futures = append(cfg.Futures, fut)
	}
	for i, o := range raw.Options {
		field := fmt.Sprintf("options[%d]", i)
		opt := Options{
			Product:     checkProduct(field+".product", "O", o.Product),
			Expiries:    DefaultExpiries,
			Weekly:      true,
			StrikeRange: DefaultStrikeRange,
			Sessions:    checkSessions(field+".sessions", o.Sessions),
		}
		if o.Expiries != nil {
			opt.Expiries = *o.Expiries
		}
		if o.Weekly != nil {
			opt.Weekly = *o.Weekly
		}
		if o.StrikeRange != nil {
			opt.StrikeRange = *o.StrikeRange
		}
		if opt.Expiries < 0 {
			add(field+".expiries", "must not be negative")
		}
		if opt.StrikeRange < 0 || opt.StrikeRange >= 1 {
			add(field+".strike_range", "must be between 0 and 1 (e.g. 0.05 for ±5%%)")
		}
		cfg.Options = append(cfg.Options, opt)
	}

	if len(cfg.Stocks.TWSE)+len(cfg.Stocks.TPEx)+len(cfg.Futures)+len(cfg.Options) == 0 && len(p) == 0 {
		add("config", "specify at least one of stocks.twse, stocks.tpex, [[futures]] or [[options]]")
	}
	if len(p) > 0 {
		return nil, &config.ValidationError{Problems: p}
	}
	return cfg, nil
}
