package market

import (
	"testing"
	"time"
)

func day(s string) time.Time {
	t, _ := time.Parse("2006-01-02", s)
	return t
}

func TestMonthChunks(t *testing.T) {
	got := MonthChunks(day("2026-09-28"), day("2026-11-03"))
	want := [][2]string{{"2026-09-28", "2026-09-30"}, {"2026-10-01", "2026-10-31"}, {"2026-11-01", "2026-11-03"}}
	if len(got) != len(want) {
		t.Fatalf("chunks = %v", got)
	}
	for i, w := range want {
		if got[i].From.Format("2006-01-02") != w[0] || got[i].To.Format("2006-01-02") != w[1] {
			t.Errorf("chunk %d = %v - %v, want %v", i, got[i].From, got[i].To, w)
		}
	}
	if one := MonthChunks(day("2026-10-05"), day("2026-10-05")); len(one) != 1 {
		t.Errorf("single day = %v", one)
	}
	if none := MonthChunks(day("2026-10-06"), day("2026-10-05")); len(none) != 0 {
		t.Errorf("reversed = %v", none)
	}
	ms := Months(day("2026-12-30"), day("2027-01-02"))
	if len(ms) != 2 || ms[0].Format("2006-01-02") != "2026-12-01" || ms[1].Format("2006-01-02") != "2027-01-01" {
		t.Errorf("months = %v", ms)
	}
}

func TestTaifexForm(t *testing.T) {
	f := TaifexForm("TXO", day("2026-10-01"), day("2026-10-05"))
	if f.Get("commodity_id") != "TXO" || f.Get("queryStartDate") != "2026/10/01" || f.Get("queryEndDate") != "2026/10/05" || f.Get("down_type") != "1" {
		t.Errorf("form = %v", f)
	}
}
