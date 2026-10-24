package market

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/elvis3434/gleaner24/internal/config"
)

func TestExampleQuotesConfig(t *testing.T) {
	cfg, err := LoadConfig(filepath.Join("..", "..", "quotes.example.toml"))
	if err != nil {
		t.Fatalf("quotes.example.toml: %v", err)
	}
	if len(cfg.Stocks.TWSE) == 0 || len(cfg.Futures) == 0 || len(cfg.Options) == 0 {
		t.Errorf("example config = %+v", cfg)
	}
}

func TestConfigDefaults(t *testing.T) {
	cfg, err := ParseConfig(`
[stocks]
twse = ["2330"]

[[futures]]
product = "tx"

[[options]]
product = "TXO"
`)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Output.DB != "./data/quotes.db" || cfg.Output.LogDir != "./logs" {
		t.Errorf("output = %+v", cfg.Output)
	}
	want := Crawl{UserAgent: DefaultUserAgent, PerHostDelay: 3 * time.Second, Timeout: 30 * time.Second, Retries: 2, LookbackDays: 5}
	if cfg.Crawl != want {
		t.Errorf("crawl = %+v", cfg.Crawl)
	}
	f := cfg.Futures[0]
	if f.Product != "TX" || f.Months != 2 || f.IncludeSpreads || !reflect.DeepEqual(f.Sessions, []string{Regular, AfterHours}) {
		t.Errorf("futures = %+v", f)
	}
	o := cfg.Options[0]
	if o.Expiries != 2 || !o.Weekly || o.StrikeRange != 0.05 || len(o.Sessions) != 2 {
		t.Errorf("options = %+v", o)
	}
	if cfg.Chart != (Chart{Dir: "./charts", Auto: true}) {
		t.Errorf("chart = %+v", cfg.Chart)
	}
}

func TestConfigOverrides(t *testing.T) {
	cfg, err := ParseConfig(`
[crawl]
per_host_delay = "5s"
retries = 0
lookback_days = 0

[[futures]]
product = "TX"
months = 0
include_spreads = true
sessions = ["regular"]

[[futures]]
product = "MTX"

[[options]]
product = "TXO"
expiries = 0
weekly = false
strike_range = 0

[chart]
dir = "out"
auto = false
`)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Crawl.PerHostDelay != 5*time.Second || cfg.Crawl.Retries != 0 || cfg.Crawl.LookbackDays != 0 {
		t.Errorf("crawl = %+v", cfg.Crawl)
	}
	if f := cfg.Futures[0]; f.Months != 0 || !f.IncludeSpreads || !reflect.DeepEqual(f.Sessions, []string{"regular"}) {
		t.Errorf("futures[0] = %+v", f)
	}
	if f := cfg.Futures[1]; f.Months != 2 || len(f.Sessions) != 2 {
		t.Errorf("futures[1] defaults lost: %+v", f)
	}
	if o := cfg.Options[0]; o.Expiries != 0 || o.Weekly || o.StrikeRange != 0 {
		t.Errorf("options = %+v", o)
	}
	if cfg.Chart != (Chart{Dir: "out", Auto: false}) {
		t.Errorf("chart = %+v", cfg.Chart)
	}
}

func TestConfigErrors(t *testing.T) {
	_, err := ParseConfig(`
[crawl]
timeout = "0s"

[stocks]
twse = ["2330", "23 30"]
tpex = ["2330"]

[[futures]]
product = "TX"
months = -1
sessions = ["night"]

[[futures]]
product = "TX"

[[options]]
product = "TXO"
strike_range = 1.5

[extra]
x = 1
`)
	var ve *config.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v", err)
	}
	for _, want := range []string{
		"extra.x: unknown field",
		"crawl.timeout",
		`stocks.twse[1]: invalid stock code "23 30"`,
		`stocks.tpex[0]: duplicate stock code "2330"`,
		"futures[0].months",
		"futures[0].sessions",
		`futures[1].product: duplicate product "TX"`,
		"options[0].strike_range",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q:\n%v", want, err)
		}
	}
	if _, err := ParseConfig(``); err == nil || !strings.Contains(err.Error(), "at least one") {
		t.Errorf("empty config err = %v", err)
	}
}
