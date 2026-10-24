package chart

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/elvis3434/gleaner24/internal/market"
	"github.com/elvis3434/gleaner24/internal/market/futures"
	"github.com/elvis3434/gleaner24/internal/market/options"
	"github.com/elvis3434/gleaner24/internal/market/stock"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "quotes", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// sampleStore fills a database from the official sample responses.
func sampleStore(t *testing.T) *market.Store {
	t.Helper()
	st, err := market.OpenStore(filepath.Join(t.TempDir(), "quotes.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	var rows []market.Row
	tw, _ := stock.ParseTWSE("2330", fixture(t, "twse_stock_day_2330_202609.json"))
	tp, _ := stock.ParseTPEx("6223", fixture(t, "tpex_trading_stock_6223_202609.json"))
	for _, d := range append(tw, tp...) {
		rows = append(rows, d.Row())
	}
	if _, err := st.UpsertAll(stock.Table, rows, "t"); err != nil {
		t.Fatal(err)
	}
	st.SetStockName("2330", stock.TWSE, "台積電", "t")

	fut, _ := futures.Parse(fixture(t, "taifex_fut_TX_20261005.csv"), "TX")
	rows = nil
	for _, d := range futures.Select(fut, 2, false, []string{market.Regular, market.AfterHours}) {
		rows = append(rows, d.Row())
	}
	if _, err := st.UpsertAll(futures.Table, rows, "t"); err != nil {
		t.Fatal(err)
	}
	opt, _ := options.Parse(fixture(t, "taifex_opt_TXO_20261005.csv"), "TXO")
	rows = nil
	for _, d := range opt {
		rows = append(rows, d.Row())
	}
	if _, err := st.UpsertAll(options.Table, rows, "t"); err != nil {
		t.Fatal(err)
	}
	return st
}

// readData strips the QD.add/QD.catalog wrapper from a data script.
func readData(t *testing.T, path, prefix string, v any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := strings.TrimSpace(string(b))
	if !strings.HasPrefix(s, prefix) || !strings.HasSuffix(s, ");") {
		t.Fatalf("%s: unexpected wrapper: %.80s", path, s)
	}
	if err := json.Unmarshal([]byte(strings.TrimSuffix(strings.TrimPrefix(s, prefix), ");")), v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func TestGenerate(t *testing.T) {
	st := sampleStore(t)
	dir := filepath.Join(t.TempDir(), "charts")
	at := time.Date(2026, 10, 24, 15, 40, 0, 0, time.FixedZone("CST", 8*3600))
	res, err := Generate(st.DB(), dir, at)
	if err != nil {
		t.Fatal(err)
	}
	if res.DataFiles != 4 { // 2 stocks, TX, TXO 202610W1
		t.Errorf("data files = %d", res.DataFiles)
	}
	for _, f := range []string{"index.html", "lightweight-charts.js", "LICENSE-lightweight-charts.txt", "data/catalog.js",
		"data/stock_2330.js", "data/stock_6223.js", "data/fut_TX.js", "data/opt_TXO_202610W1.js"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(f))); err != nil {
			t.Errorf("missing %s", f)
		}
	}

	var cat Catalog
	readData(t, filepath.Join(dir, "data", "catalog.js"), "QD.catalog(", &cat)
	if cat.Generated != "2026-10-24T15:40:00+08:00" || len(cat.Stocks) != 2 || len(cat.Futures) != 1 || len(cat.Options) != 1 {
		t.Fatalf("catalog = %+v", cat)
	}
	if s := cat.Stocks[0]; s.Symbol != "2330" || s.Name != "台積電" || s.Market != "twse" || s.From != "2026-09-01" || s.To != "2026-09-30" {
		t.Errorf("stock entry = %+v", s)
	}
	if e := cat.Options[0].Expiries; len(e) != 1 || e[0].ContractMonth != "202610W1" || e[0].ExpiryDate != "2026-10-07" {
		t.Errorf("expiries = %+v", e)
	}

	var stockData struct{ Day [][]any }
	readData(t, filepath.Join(dir, "data", "stock_2330.js"), `QD.add("stock_2330", `, &stockData)
	first := stockData.Day[0]
	// 31,855,287 shares = 31,855.287 lots.
	if len(stockData.Day) != 20 || first[0] != "2026-09-01" || first[1] != 2395.0 || first[4] != 2440.0 || first[5] != 31855.287 {
		t.Errorf("stock bars = %v", first)
	}

	var futData map[string]struct {
		Day, Full [][]any
		Months    map[string]string
	}
	readData(t, filepath.Join(dir, "data", "fut_TX.js"), `QD.add("fut_TX", `, &futData)
	near, next := futData["near"], futData["next"]
	if len(near.Day) != 1 || near.Months["2026-10-05"] != "202610" || next.Months["2026-10-05"] != "202611" {
		t.Errorf("TX near/next = %+v / %+v", near, next)
	}
	// Full session 2026-10-05, 202610: night 48671-49495 (low 48671), day 49470-50097.
	if f := near.Full[0]; f[1] != 48671.0 || f[2] != 50097.0 || f[3] != 48671.0 || f[4] != 49949.0 || f[5] != 74146.0 {
		t.Errorf("TX near full = %v", f)
	}

	var optData struct {
		Expiry  string
		Strikes []float64
		Series  map[string]struct{ Day, Full [][]any }
	}
	readData(t, filepath.Join(dir, "data", "opt_TXO_202610W1.js"), `QD.add("opt_TXO_202610W1", `, &optData)
	if optData.Expiry != "2026-10-07" || len(optData.Strikes) != 2 || len(optData.Series["42800P"].Day) != 1 || len(optData.Series["42800C"].Day) != 0 {
		t.Errorf("options data = %+v", optData)
	}

	page, _ := os.ReadFile(filepath.Join(dir, "index.html"))
	for _, want := range []string{`src="lightweight-charts.js"`, `src="data/catalog.js"`, "https://www.tradingview.com/", "attributionLogo: true"} {
		if !strings.Contains(string(page), want) {
			t.Errorf("index.html lacks %q", want)
		}
	}

	// A stale data file is removed on the next generation.
	stale := filepath.Join(dir, "data", "stock_9999.js")
	os.WriteFile(stale, []byte("QD.add('x', {});"), 0o644)
	res, err = Generate(st.DB(), dir, at)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) || res.Removed != 1 {
		t.Errorf("stale file kept (removed=%d)", res.Removed)
	}
}

func TestGenerateEmptyDatabase(t *testing.T) {
	st, err := market.OpenStore(filepath.Join(t.TempDir(), "q.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	dir := t.TempDir()
	res, err := Generate(st.DB(), dir, time.Now())
	if err != nil || res.DataFiles != 0 {
		t.Fatalf("res = %+v, %v", res, err)
	}
	var cat Catalog
	readData(t, filepath.Join(dir, "data", "catalog.js"), "QD.catalog(", &cat)
	if len(cat.Stocks)+len(cat.Futures)+len(cat.Options) != 0 {
		t.Errorf("catalog = %+v", cat)
	}
}

func TestKey(t *testing.T) {
	if k := key("opt", "TXO", "202610W1"); k != "opt_TXO_202610W1" {
		t.Errorf("key = %s", k)
	}
	if k := key("stock", "../x y"); k != "stock_xy" {
		t.Errorf("unsafe key = %s", k)
	}
}
