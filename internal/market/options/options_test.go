package options

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/elvis3434/gleaner24/internal/market"
)

func TestParseOfficial(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "quotes", "taifex_opt_TXO_20261005.csv"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := Parse(b, "TXO")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 6 {
		t.Fatalf("rows = %d", len(rows))
	}
	c := rows[0]
	if c.Date != "2026-10-05" || c.Product != "TXO" || c.ContractMonth != "202610W1" || c.Strike != 42800 ||
		c.CallPut != Call || c.Session != market.Regular || c.ExpiryDate != "2026-10-07" ||
		c.Open != nil || *c.Volume != 0 || *c.Settlement != 6820 || *c.OpenInterest != 0 || c.BestBid != nil || *c.BestAsk != 6840 {
		t.Errorf("call row = %+v", c)
	}
	p := rows[2]
	if p.CallPut != Put || *p.Open != 0.5 || *p.High != 0.6 || *p.Low != 0.2 || *p.Close != 0.2 || *p.Volume != 19 ||
		*p.Settlement != 0.2 || *p.OpenInterest != 117 || *p.Change != -0.3 {
		t.Errorf("put row = %+v", p)
	}
	n := rows[3]
	if n.Session != market.AfterHours || n.Settlement != nil || n.OpenInterest != nil || *n.Close != 0.6 {
		t.Errorf("night row = %+v", n)
	}
	if !c.IsWeekly() {
		t.Error("202610W1 is weekly")
	}
}

func TestParseErrors(t *testing.T) {
	if _, err := Parse(nil, "TXO"); !errors.Is(err, market.ErrNoData) {
		t.Errorf("empty: %v", err)
	}
}

func fp(v float64) *float64 { return &v }

// chain builds a regular and an after-hours row per strike and right, with
// settlement prices following a simple intrinsic value around spot.
func chain(date, month, expiry string, spot float64, strikes ...float64) []Daily {
	var rows []Daily
	for _, k := range strikes {
		call, put := max(spot-k, 0)+10, max(k-spot, 0)+10
		for _, s := range []string{market.Regular, market.AfterHours} {
			for _, cp := range []string{Call, Put} {
				d := Daily{Date: date, Product: "TXO", ContractMonth: month, Strike: k, CallPut: cp, Session: s, ExpiryDate: expiry}
				if s == market.Regular {
					if cp == Call {
						d.Settlement = fp(call)
					} else {
						d.Settlement = fp(put)
					}
				}
				rows = append(rows, d)
			}
		}
	}
	return rows
}

func count(rows []Daily) (map[string]int, map[float64]bool) {
	months := map[string]int{}
	strikes := map[float64]bool{}
	for _, r := range rows {
		months[r.ContractMonth]++
		strikes[r.Strike] = true
	}
	return months, strikes
}

func TestSelectExpiriesAndWeekly(t *testing.T) {
	var rows []Daily
	rows = append(rows, chain("2026-10-05", "202611", "2026-11-18", 100, 100)...)
	rows = append(rows, chain("2026-10-05", "202610W1", "2026-10-07", 100, 100)...)
	rows = append(rows, chain("2026-10-05", "202610", "2026-10-21", 100, 100)...)
	rows = append(rows, chain("2026-10-05", "202610F2", "2026-10-09", 100, 100)...)
	both := []string{market.Regular, market.AfterHours}

	months, _ := count(Select(rows, Selection{Expiries: 2, Weekly: true, Sessions: both}))
	if len(months) != 2 || months["202610W1"] == 0 || months["202610F2"] == 0 {
		t.Errorf("nearest 2 with weekly = %v", months)
	}
	months, _ = count(Select(rows, Selection{Expiries: 2, Weekly: false, Sessions: both}))
	if len(months) != 2 || months["202610"] == 0 || months["202611"] == 0 {
		t.Errorf("nearest 2 monthly = %v", months)
	}
	months, _ = count(Select(rows, Selection{Expiries: 0, Weekly: true, Sessions: []string{market.AfterHours}}))
	if len(months) != 4 || months["202611"] != 2 {
		t.Errorf("all, night only = %v", months)
	}
}

func TestSelectStrikeRange(t *testing.T) {
	strikes := []float64{90, 94, 95, 96, 100, 104, 105, 106, 110}
	rows := chain("2026-10-05", "202610", "2026-10-21", 100.4, strikes...)

	got := Select(rows, Selection{Expiries: 1, StrikeRange: 0.05, Sessions: []string{market.Regular, market.AfterHours}})
	_, kept := count(got)
	for _, k := range []float64{95, 96, 100, 104, 105} {
		if !kept[k] {
			t.Errorf("strike %v dropped", k)
		}
	}
	for _, k := range []float64{90, 94, 106, 110} {
		if kept[k] {
			t.Errorf("strike %v kept", k)
		}
	}
	// The after-hours rows follow the regular-session ATM strike.
	night := 0
	for _, r := range got {
		if r.Session == market.AfterHours {
			night++
		}
	}
	if night != 10 { // 5 strikes x call/put
		t.Errorf("night rows = %d", night)
	}

	// Without settlement prices there is no ATM estimate: keep everything.
	for i := range rows {
		rows[i].Settlement = nil
	}
	if n := len(Select(rows, Selection{StrikeRange: 0.05, Sessions: []string{market.Regular}})); n != len(strikes)*2 {
		t.Errorf("no settlement: kept %d", n)
	}
}

func TestRow(t *testing.T) {
	d := Daily{Date: "2026-10-05", Product: "TXO", ContractMonth: "202610", Strike: 42800, CallPut: Put, Session: market.Regular}
	r := d.Row()
	if len(r.Key) != 6 || r.Key[3].Value != 42800.0 || r.Data[0].Value != nil {
		t.Errorf("row = %+v", r)
	}
}
