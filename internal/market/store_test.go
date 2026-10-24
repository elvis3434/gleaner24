package market

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func f64(v float64) *float64 { return &v }

func futRow(close *float64, oi *int64) Row {
	return Row{
		Key: []Col{{"date", "2026-10-05"}, {"product", "TX"}, {"contract_month", "202610"}, {"session", Regular}},
		Data: []Col{{"open", f64(49470)}, {"high", nil}, {"low", (*float64)(nil)}, {"close", close}, {"change", nil},
			{"volume", Int64(41858)}, {"settlement", nil}, {"open_interest", oi}, {"best_bid", nil}, {"best_ask", nil}},
	}
}

func TestUpsertOutcomes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d", "quotes.db")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	c, err := s.UpsertAll("futures_daily", []Row{futRow(f64(49949), nil)}, "t1")
	if err != nil || c != (Counts{New: 1}) {
		t.Fatalf("first = %+v, %v", c, err)
	}
	c, _ = s.UpsertAll("futures_daily", []Row{futRow(f64(49949), nil)}, "t2")
	if c != (Counts{Unchanged: 1}) {
		t.Errorf("same data = %+v", c)
	}
	// Settlement data published later: same key, new values.
	c, _ = s.UpsertAll("futures_daily", []Row{futRow(f64(49949), Int64(108339))}, "t3")
	if c != (Counts{Updated: 1}) {
		t.Errorf("changed data = %+v", c)
	}
	var oi int64
	var fetched string
	s.DB().QueryRow(`SELECT open_interest, fetched_at FROM futures_daily`).Scan(&oi, &fetched)
	if oi != 108339 || fetched != "t3" {
		t.Errorf("stored oi=%d fetched=%s", oi, fetched)
	}
	if n, _ := s.Count("futures_daily"); n != 1 {
		t.Errorf("rows = %d", n)
	}

	// Other sessions are separate rows.
	night := futRow(f64(49346), nil)
	night.Key[3] = Col{"session", AfterHours}
	if c, _ := s.UpsertAll("futures_daily", []Row{night}, "t4"); c.New != 1 {
		t.Errorf("night = %+v", c)
	}

	id, err := s.StartRun("2026-10-24T15:30:00+08:00", "logs/q.log")
	if err != nil || id == 0 {
		t.Fatal(id, err)
	}
	if err := s.FinishRun(id, "x", "completed", "{}"); err != nil {
		t.Fatal(err)
	}
	s.Close()

	ro, err := OpenStoreReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	c, err = ro.UpsertAll("futures_daily", []Row{futRow(f64(1), Int64(108339)), futRow(f64(49949), Int64(108339))}, "t5")
	if err != nil || c != (Counts{Updated: 1, Unchanged: 1}) {
		t.Errorf("read-only compare = %+v, %v", c, err)
	}
	var close float64
	ro.DB().QueryRow(`SELECT close FROM futures_daily WHERE session = 'regular'`).Scan(&close)
	if close != 49949 {
		t.Error("read-only store was written")
	}
	if _, err := ro.StartRun("x", "y"); err != nil {
		t.Error(err)
	}

	if _, err := OpenStoreReadOnly(filepath.Join(t.TempDir(), "none.db")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing db: %v", err)
	}
	var nilStore *Store
	if c, _ := nilStore.UpsertAll("x", []Row{{}, {}}, ""); c.New != 2 {
		t.Errorf("nil store = %+v", c)
	}
}
