// Package chart builds the daily K-line web page (SPEC-quotes: K 線網頁)
// from the quotes database: a static index.html that opens from file://,
// with one data script per stock, futures product and options expiry.
package chart

import (
	"encoding/json"
	"sort"

	"github.com/elvis3434/gleaner24/internal/market"
	"github.com/elvis3434/gleaner24/internal/market/futures"
)

// Bar is one daily candle. It encodes as a compact JSON array
// [time, open, high, low, close, volume] to keep data files small.
type Bar struct {
	Time   string // YYYY-MM-DD
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

func (b Bar) MarshalJSON() ([]byte, error) {
	return json.Marshal([]any{b.Time, b.Open, b.High, b.Low, b.Close, b.Volume})
}

// Marker labels a date on a chart (used for contract rolls).
type Marker struct {
	Time string `json:"time"`
	Text string `json:"text"`
}

// Quote is one stored daily row of any instrument, as read for charting.
type Quote struct {
	Date                   string
	Open, High, Low, Close *float64
	Volume                 *int64
}

// bar returns the candle of q, or false when q has no complete prices (no
// trade that day).
func (q Quote) bar(volumeScale float64) (Bar, bool) {
	if q.Open == nil || q.High == nil || q.Low == nil || q.Close == nil {
		return Bar{}, false
	}
	b := Bar{Time: q.Date, Open: *q.Open, High: *q.High, Low: *q.Low, Close: *q.Close}
	if q.Volume != nil {
		b.Volume = float64(*q.Volume) / volumeScale
	}
	return b, true
}

// FullSession merges a trading date's after-hours session (the night
// before) into its regular session: open from the night session when it
// traded, otherwise from the day; high and low over both; close from the
// day; volumes added. Without a traded regular session there is no bar.
func FullSession(regular Quote, night *Quote) (Bar, bool) {
	day, ok := regular.bar(1)
	if !ok {
		return Bar{}, false
	}
	if night == nil {
		return day, true
	}
	n, ok := night.bar(1)
	if !ok {
		if night.Volume != nil {
			day.Volume += float64(*night.Volume)
		}
		return day, true
	}
	return Bar{
		Time:   day.Time,
		Open:   n.Open,
		High:   max(day.High, n.High),
		Low:    min(day.Low, n.Low),
		Close:  day.Close,
		Volume: day.Volume + n.Volume,
	}, true
}

// Sessions holds the two views of one series.
type Sessions struct {
	Day  []Bar `json:"day"`
	Full []Bar `json:"full"`
}

func (s *Sessions) add(regular Quote, night *Quote) {
	if b, ok := regular.bar(1); ok {
		s.Day = append(s.Day, b)
	}
	if b, ok := FullSession(regular, night); ok {
		s.Full = append(s.Full, b)
	}
}

// FuturesRow is one stored futures row.
type FuturesRow struct {
	Quote
	ContractMonth string
	Session       string
}

// Continuous is a rolled series such as 近月連續.
type Continuous struct {
	Sessions
	Rolls []Marker `json:"rolls"`
	// Months gives the contract month used on each date, for the legend.
	Months map[string]string `json:"months"`
}

// ContinuousFutures builds the nearest (rank 0) and second-nearest (rank 1)
// continuous series. On each trading date the monthly contracts that have a
// regular-session row are ordered by month; an expiring contract still
// trades on its settlement day, so that day stays with the expiring month
// and the series rolls on the next trading date.
func ContinuousFutures(rows []FuturesRow) [2]Continuous {
	type key struct{ date, month string }
	regular := map[key]Quote{}
	night := map[key]Quote{}
	months := map[string][]string{}
	for _, r := range rows {
		if !futures.IsMonth(r.ContractMonth) {
			continue
		}
		k := key{r.Date, r.ContractMonth}
		if r.Session == market.AfterHours {
			night[k] = r.Quote
			continue
		}
		regular[k] = r.Quote
		months[r.Date] = append(months[r.Date], r.ContractMonth)
	}
	dates := make([]string, 0, len(months))
	for d := range months {
		dates = append(dates, d)
	}
	sort.Strings(dates)

	var out [2]Continuous
	for i := range out {
		out[i].Months = map[string]string{}
	}
	prev := [2]string{}
	for _, d := range dates {
		ms := months[d]
		sort.Strings(ms)
		for rank := 0; rank < 2 && rank < len(ms); rank++ {
			m := ms[rank]
			c := &out[rank]
			var n *Quote
			if q, ok := night[key{d, m}]; ok {
				n = &q
			}
			c.add(regular[key{d, m}], n)
			c.Months[d] = m
			if prev[rank] != "" && prev[rank] != m {
				c.Rolls = append(c.Rolls, Marker{Time: d, Text: "換 " + m})
			}
			prev[rank] = m
		}
	}
	return out
}

// OptionRow is one stored options row.
type OptionRow struct {
	Quote
	Strike  float64
	CallPut string
	Session string
}

// OptionSeries builds day and full-session bars per strike and right of one
// expiry. Keys look like "42800C" and "42800P".
func OptionSeries(rows []OptionRow) (map[string]*Sessions, []float64) {
	type key struct {
		date   string
		strike float64
		right  string
	}
	regular := map[key]Quote{}
	night := map[key]Quote{}
	strikeSet := map[float64]bool{}
	for _, r := range rows {
		k := key{r.Date, r.Strike, r.CallPut}
		strikeSet[r.Strike] = true
		if r.Session == market.AfterHours {
			night[k] = r.Quote
		} else {
			regular[k] = r.Quote
		}
	}
	keys := make([]key, 0, len(regular))
	for k := range regular {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].date < keys[j].date })

	series := map[string]*Sessions{}
	for _, k := range keys {
		id := SeriesID(k.strike, k.right)
		s := series[id]
		if s == nil {
			s = &Sessions{}
			series[id] = s
		}
		var n *Quote
		if q, ok := night[k]; ok {
			n = &q
		}
		s.add(regular[k], n)
	}
	strikes := make([]float64, 0, len(strikeSet))
	for k := range strikeSet {
		strikes = append(strikes, k)
	}
	sort.Float64s(strikes)
	return series, strikes
}

// SeriesID names a strike and right, e.g. SeriesID(42800, "call") = "42800C".
func SeriesID(strike float64, right string) string {
	b, _ := json.Marshal(strike)
	suffix := "P"
	if right == "call" {
		suffix = "C"
	}
	return string(b) + suffix
}
