package chart

import (
	"bytes"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

//go:embed assets/index.html assets/lightweight-charts.js assets/LICENSE-lightweight-charts.txt
var assets embed.FS

// staticFiles are copied from assets/ into the output directory.
var staticFiles = []string{"index.html", "lightweight-charts.js", "LICENSE-lightweight-charts.txt"}

// Catalog lists what the page can show (data/catalog.js).
type Catalog struct {
	Generated string         `json:"generated"`
	Stocks    []StockEntry   `json:"stocks"`
	Futures   []FuturesEntry `json:"futures"`
	Options   []OptionsEntry `json:"options"`
}

type StockEntry struct {
	Key    string `json:"key"`
	Symbol string `json:"symbol"`
	Name   string `json:"name"`
	Market string `json:"market"`
	From   string `json:"from"`
	To     string `json:"to"`
}

type FuturesEntry struct {
	Key     string `json:"key"`
	Product string `json:"product"`
	From    string `json:"from"`
	To      string `json:"to"`
}

type OptionsEntry struct {
	Product  string        `json:"product"`
	Expiries []ExpiryEntry `json:"expiries"`
}

type ExpiryEntry struct {
	Key           string `json:"key"`
	ContractMonth string `json:"month"`
	ExpiryDate    string `json:"expiry"`
}

// Result reports what Generate wrote.
type Result struct {
	Index     string // path of index.html
	DataFiles int
	Removed   int
}

var safeKey = regexp.MustCompile(`[^A-Za-z0-9_]`)

func key(parts ...string) string {
	return safeKey.ReplaceAllString(strings.Join(parts, "_"), "")
}

// Generate writes the K-line page for everything in db into dir.
func Generate(db *sql.DB, dir string, generated time.Time) (Result, error) {
	var res Result
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return res, err
	}
	for _, name := range staticFiles {
		b, err := assets.ReadFile("assets/" + name)
		if err != nil {
			return res, err
		}
		if err := writeFile(filepath.Join(dir, name), b); err != nil {
			return res, err
		}
	}

	cat := Catalog{Generated: generated.Format(time.RFC3339)}
	written := map[string]bool{}
	emit := func(k string, v any) error {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		var buf bytes.Buffer
		fmt.Fprintf(&buf, "QD.add(%q, %s);\n", k, b)
		written[k+".js"] = true
		return writeFile(filepath.Join(dataDir, k+".js"), buf.Bytes())
	}

	if err := stocks(db, &cat, emit); err != nil {
		return res, fmt.Errorf("stocks: %w", err)
	}
	if err := futuresData(db, &cat, emit); err != nil {
		return res, fmt.Errorf("futures: %w", err)
	}
	if err := optionsData(db, &cat, emit); err != nil {
		return res, fmt.Errorf("options: %w", err)
	}
	res.DataFiles = len(written)

	b, err := json.Marshal(cat)
	if err != nil {
		return res, err
	}
	if err := writeFile(filepath.Join(dataDir, "catalog.js"), []byte("QD.catalog("+string(b)+");\n")); err != nil {
		return res, err
	}
	written["catalog.js"] = true

	// Drop data files of items no longer in the database.
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return res, err
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".js") && !written[e.Name()] {
			if os.Remove(filepath.Join(dataDir, e.Name())) == nil {
				res.Removed++
			}
		}
	}
	res.Index = filepath.Join(dir, "index.html")
	return res, nil
}

// writeFile writes atomically so an open page never reads half a file.
func writeFile(path string, data []byte) error {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, data) {
		return nil
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

func scanQuote(q *Quote, dest ...any) []any {
	return append(dest, &q.Open, &q.High, &q.Low, &q.Close, &q.Volume)
}

func stocks(db *sql.DB, cat *Catalog, emit func(string, any) error) error {
	names := map[string]string{}
	rows, err := db.Query(`SELECT symbol, name FROM stock_names`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var s, n string
		if err := rows.Scan(&s, &n); err != nil {
			rows.Close()
			return err
		}
		names[s] = n
	}
	rows.Close()

	rows, err = db.Query(`SELECT symbol, market, date, open, high, low, close, volume FROM stock_daily ORDER BY symbol, date`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type acc struct {
		entry StockEntry
		bars  []Bar
	}
	var list []*acc
	bySymbol := map[string]*acc{}
	for rows.Next() {
		var sym, mkt string
		var qt Quote
		if err := rows.Scan(scanQuote(&qt, &sym, &mkt, &qt.Date)...); err != nil {
			return err
		}
		a := bySymbol[sym]
		if a == nil {
			a = &acc{entry: StockEntry{Key: key("stock", sym), Symbol: sym, Name: names[sym], Market: mkt, From: qt.Date}}
			bySymbol[sym] = a
			list = append(list, a)
		}
		a.entry.To = qt.Date
		// Volume is stored in shares; the page shows lots (張).
		if b, ok := qt.bar(1000); ok {
			a.bars = append(a.bars, b)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, a := range list {
		if err := emit(a.entry.Key, map[string]any{"day": nonNil(a.bars)}); err != nil {
			return err
		}
		cat.Stocks = append(cat.Stocks, a.entry)
	}
	return nil
}

func nonNil(b []Bar) []Bar {
	if b == nil {
		return []Bar{}
	}
	return b
}

func futuresData(db *sql.DB, cat *Catalog, emit func(string, any) error) error {
	rows, err := db.Query(`SELECT product, date, contract_month, session, open, high, low, close, volume
FROM futures_daily ORDER BY product, date`)
	if err != nil {
		return err
	}
	byProduct := map[string][]FuturesRow{}
	var products []string
	for rows.Next() {
		var p string
		var r FuturesRow
		if err := rows.Scan(scanQuote(&r.Quote, &p, &r.Date, &r.ContractMonth, &r.Session)...); err != nil {
			rows.Close()
			return err
		}
		if _, seen := byProduct[p]; !seen {
			products = append(products, p)
		}
		byProduct[p] = append(byProduct[p], r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, p := range products {
		c := ContinuousFutures(byProduct[p])
		if len(c[0].Day)+len(c[0].Full) == 0 {
			continue
		}
		for i := range c {
			c[i].Day, c[i].Full = nonNil(c[i].Day), nonNil(c[i].Full)
			if c[i].Rolls == nil {
				c[i].Rolls = []Marker{}
			}
		}
		k := key("fut", p)
		if err := emit(k, map[string]any{"near": c[0], "next": c[1]}); err != nil {
			return err
		}
		e := FuturesEntry{Key: k, Product: p}
		if len(c[0].Day) > 0 {
			e.From, e.To = c[0].Day[0].Time, c[0].Day[len(c[0].Day)-1].Time
		}
		cat.Futures = append(cat.Futures, e)
	}
	return nil
}

func optionsData(db *sql.DB, cat *Catalog, emit func(string, any) error) error {
	rows, err := db.Query(`SELECT product, contract_month, COALESCE(expiry_date, ''), date, strike, call_put, session,
	open, high, low, close, volume FROM options_daily ORDER BY product, contract_month, date`)
	if err != nil {
		return err
	}
	type group struct {
		product, month, expiry string
		rows                   []OptionRow
	}
	var groups []*group
	byKey := map[string]*group{}
	for rows.Next() {
		var p, m, exp string
		var r OptionRow
		if err := rows.Scan(scanQuote(&r.Quote, &p, &m, &exp, &r.Date, &r.Strike, &r.CallPut, &r.Session)...); err != nil {
			rows.Close()
			return err
		}
		g := byKey[p+"/"+m]
		if g == nil {
			g = &group{product: p, month: m}
			byKey[p+"/"+m] = g
			groups = append(groups, g)
		}
		if exp != "" {
			g.expiry = exp
		}
		g.rows = append(g.rows, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	// Order expiries by expiry date (contract month when unknown).
	sort.SliceStable(groups, func(i, j int) bool {
		a, b := groups[i], groups[j]
		if a.product != b.product {
			return a.product < b.product
		}
		ka, kb := a.expiry+a.month, b.expiry+b.month
		return ka < kb
	})
	idx := map[string]int{}
	for _, g := range groups {
		series, strikes := OptionSeries(g.rows)
		for _, s := range series {
			s.Day, s.Full = nonNil(s.Day), nonNil(s.Full)
		}
		k := key("opt", g.product, g.month)
		if err := emit(k, map[string]any{"expiry": g.expiry, "strikes": strikes, "series": series}); err != nil {
			return err
		}
		i, ok := idx[g.product]
		if !ok {
			i = len(cat.Options)
			idx[g.product] = i
			cat.Options = append(cat.Options, OptionsEntry{Product: g.product})
		}
		cat.Options[i].Expiries = append(cat.Options[i].Expiries, ExpiryEntry{Key: k, ContractMonth: g.month, ExpiryDate: g.expiry})
	}
	return nil
}
