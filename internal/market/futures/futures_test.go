package futures

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/elvis3434/gleaner24/internal/market"
)

func official(t *testing.T) []Daily {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "quotes", "taifex_fut_TX_20261005.csv"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := Parse(b, "TX")
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestParseOfficial(t *testing.T) {
	rows := official(t)
	if len(rows) != 23 {
		t.Fatalf("rows = %d", len(rows))
	}
	day := rows[0]
	if day.Date != "2026-10-05" || day.Product != "TX" || day.ContractMonth != "202610" || day.Session != market.Regular ||
		*day.Open != 49470 || *day.High != 50097 || *day.Low != 49461 || *day.Close != 49949 || *day.Change != 1280 ||
		*day.Volume != 41858 || *day.Settlement != 49944 || *day.OpenInterest != 108339 || *day.BestBid != 49944 || *day.BestAsk != 49951 {
		t.Errorf("regular row = %+v", day)
	}
	night := rows[1]
	if night.Session != market.AfterHours || night.ContractMonth != "202610" || *night.Close != 49346 ||
		night.Settlement != nil || night.OpenInterest != nil {
		t.Errorf("after-hours row = %+v", night)
	}
	// Both sessions of the same trading date measure change from the same
	// previous settlement (SPEC-quotes: 夜盤的日期歸屬).
	if *day.Close-*day.Change != *night.Close-*night.Change {
		t.Errorf("sessions disagree on previous settlement")
	}
	// A contract that did not trade has nil prices but a zero volume.
	for _, r := range rows {
		if r.ContractMonth == "202709" && r.Session == market.AfterHours {
			if r.Open != nil || r.Close != nil || *r.Volume != 0 {
				t.Errorf("untraded row = %+v", r)
			}
		}
	}
	spreads := 0
	for _, r := range rows {
		if r.IsSpread() {
			spreads++
		}
	}
	if spreads != 11 {
		t.Errorf("spreads = %d", spreads)
	}
	if other, _ := Parse(mustRead(t), "MTX"); len(other) != 0 {
		t.Errorf("product filter kept %d rows", len(other))
	}
}

func mustRead(t *testing.T) []byte {
	b, _ := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "quotes", "taifex_fut_TX_20261005.csv"))
	return b
}

func TestSelect(t *testing.T) {
	rows := official(t)
	both := []string{market.Regular, market.AfterHours}

	got := Select(rows, 2, false, both)
	if len(got) != 4 {
		t.Fatalf("2 months, both sessions = %d rows", len(got))
	}
	for _, r := range got {
		if r.ContractMonth != "202610" && r.ContractMonth != "202611" {
			t.Errorf("unexpected month %s", r.ContractMonth)
		}
	}

	got = Select(rows, 2, true, both)
	if len(got) != 6 { // + 202610/202611 in both sessions
		t.Errorf("with spreads = %d rows", len(got))
	}

	got = Select(rows, 0, false, []string{market.AfterHours})
	if len(got) != 6 { // six months, night only
		t.Errorf("all months, night = %d rows", len(got))
	}
	for _, r := range got {
		if r.Session != market.AfterHours {
			t.Error("session filter")
		}
	}
	if len(Select(rows, 0, true, both)) != 23 {
		t.Error("select all should keep every row")
	}
}

func TestParseErrors(t *testing.T) {
	if _, err := Parse([]byte("<html>error</html>"), "TX"); !errors.Is(err, market.ErrFormat) {
		t.Errorf("html: %v", err)
	}
}

func TestRow(t *testing.T) {
	r := official(t)[0].Row()
	if len(r.Key) != 4 || r.Key[3].Value != market.Regular || len(r.Data) != 10 {
		t.Errorf("row = %+v", r)
	}
}
