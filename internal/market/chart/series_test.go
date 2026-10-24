package chart

import (
	"encoding/json"
	"testing"

	"github.com/elvis3434/gleaner24/internal/market"
)

func fp(v float64) *float64 { return &v }
func ip(v int64) *int64     { return &v }

func q(date string, o, h, l, c float64, v int64) Quote {
	return Quote{Date: date, Open: fp(o), High: fp(h), Low: fp(l), Close: fp(c), Volume: ip(v)}
}

func TestFullSession(t *testing.T) {
	day := q("2026-10-05", 49470, 50097, 49461, 49949, 41858)
	night := q("2026-10-05", 48671, 49495, 48600, 49346, 32288)
	b, ok := FullSession(day, &night)
	want := Bar{"2026-10-05", 48671, 50097, 48600, 49949, 74146}
	if !ok || b != want {
		t.Errorf("full = %+v, want %+v", b, want)
	}
	// Untraded night session: the day bar, with any night volume added.
	quiet := Quote{Date: "2026-10-05", Volume: ip(0)}
	if b, _ := FullSession(day, &quiet); b.Open != 49470 || b.Volume != 41858 {
		t.Errorf("quiet night = %+v", b)
	}
	if b, _ := FullSession(day, nil); b.Open != 49470 {
		t.Errorf("no night = %+v", b)
	}
	// No regular-session trade: no bar at all.
	if _, ok := FullSession(Quote{Date: "2026-10-05"}, &night); ok {
		t.Error("bar without a traded regular session")
	}
}

func fut(date, month, session string, close float64) FuturesRow {
	return FuturesRow{Quote: q(date, close, close+10, close-10, close, 100), ContractMonth: month, Session: session}
}

func TestContinuousFuturesRollsAfterSettlement(t *testing.T) {
	// 2026-10-21 is the October settlement day: the October contract still
	// trades, so it stays the near month that day; November takes over on
	// the 22nd.
	var rows []FuturesRow
	for _, d := range []string{"2026-10-20", "2026-10-21"} {
		rows = append(rows, fut(d, "202610", market.Regular, 100), fut(d, "202611", market.Regular, 200))
	}
	for _, d := range []string{"2026-10-22", "2026-10-23"} {
		rows = append(rows, fut(d, "202611", market.Regular, 210), fut(d, "202612", market.Regular, 300))
	}
	// Noise that must not affect ranking: spreads, weekly contracts, and a
	// night-only row.
	rows = append(rows,
		fut("2026-10-21", "202610/202611", market.Regular, 5),
		fut("2026-10-21", "202610W4", market.Regular, 1),
		fut("2026-10-24", "202611", market.AfterHours, 220))

	c := ContinuousFutures(rows)
	near, next := c[0], c[1]
	wantNear := map[string]string{"2026-10-20": "202610", "2026-10-21": "202610", "2026-10-22": "202611", "2026-10-23": "202611"}
	wantNext := map[string]string{"2026-10-20": "202611", "2026-10-21": "202611", "2026-10-22": "202612", "2026-10-23": "202612"}
	for d, m := range wantNear {
		if near.Months[d] != m {
			t.Errorf("near %s = %s, want %s", d, near.Months[d], m)
		}
		if next.Months[d] != wantNext[d] {
			t.Errorf("next %s = %s, want %s", d, next.Months[d], wantNext[d])
		}
	}
	if len(near.Months) != 4 {
		t.Errorf("night-only date charted: %v", near.Months)
	}
	if len(near.Rolls) != 1 || near.Rolls[0] != (Marker{"2026-10-22", "換 202611"}) {
		t.Errorf("near rolls = %v", near.Rolls)
	}
	if len(next.Rolls) != 1 || next.Rolls[0] != (Marker{"2026-10-22", "換 202612"}) {
		t.Errorf("next rolls = %v", next.Rolls)
	}
	if len(near.Day) != 4 || near.Day[1].Close != 100 || near.Day[2].Close != 210 {
		t.Errorf("near day bars = %+v", near.Day)
	}
}

func TestContinuousFuturesFullSession(t *testing.T) {
	rows := []FuturesRow{
		fut("2026-10-05", "202610", market.Regular, 100),
		{Quote: q("2026-10-05", 90, 120, 80, 95, 50), ContractMonth: "202610", Session: market.AfterHours},
	}
	near := ContinuousFutures(rows)[0]
	if len(near.Full) != 1 || near.Full[0] != (Bar{"2026-10-05", 90, 120, 80, 100, 150}) {
		t.Errorf("full = %+v", near.Full)
	}
	if near.Day[0] != (Bar{"2026-10-05", 100, 110, 90, 100, 100}) {
		t.Errorf("day = %+v", near.Day)
	}
}

func TestOptionSeries(t *testing.T) {
	rows := []OptionRow{
		{Quote: q("2026-10-06", 2, 3, 1, 2, 5), Strike: 42800, CallPut: "put", Session: market.Regular},
		{Quote: q("2026-10-05", 1, 2, 0.5, 1, 9), Strike: 42800, CallPut: "put", Session: market.Regular},
		{Quote: q("2026-10-05", 0.9, 0.9, 0.5, 0.6, 10), Strike: 42800, CallPut: "put", Session: market.AfterHours},
		{Quote: Quote{Date: "2026-10-05", Volume: ip(0)}, Strike: 42900, CallPut: "call", Session: market.Regular},
	}
	series, strikes := OptionSeries(rows)
	if len(strikes) != 2 || strikes[0] != 42800 || strikes[1] != 42900 {
		t.Errorf("strikes = %v", strikes)
	}
	p := series["42800P"]
	if p == nil || len(p.Day) != 2 || p.Day[0].Time != "2026-10-05" || p.Day[1].Time != "2026-10-06" {
		t.Fatalf("42800P = %+v", p)
	}
	if p.Full[0] != (Bar{"2026-10-05", 0.9, 2, 0.5, 1, 19}) {
		t.Errorf("42800P full = %+v", p.Full[0])
	}
	if c := series["42900C"]; c == nil || len(c.Day) != 0 {
		t.Errorf("untraded series = %+v", c)
	}
	if SeriesID(42850.5, "call") != "42850.5C" || SeriesID(100, "put") != "100P" {
		t.Error("SeriesID")
	}
}

func TestBarJSON(t *testing.T) {
	b, _ := json.Marshal(Bar{"2026-10-05", 1, 2, 0.5, 1.5, 31855.287})
	if string(b) != `["2026-10-05",1,2,0.5,1.5,31855.287]` {
		t.Errorf("json = %s", b)
	}
}
