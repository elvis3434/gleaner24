// Package options parses TAIFEX daily options data and selects the
// expiries and strikes configured for collection.
package options

import (
	"cmp"
	"math"
	"slices"
	"strings"

	"github.com/elvis3434/gleaner24/internal/market"
)

// Table is the database table for Daily rows.
const Table = "options_daily"

// Rights.
const (
	Call = "call"
	Put  = "put"
)

// Daily is one series (expiry, strike, right) in one session of one day.
type Daily struct {
	Date          string // YYYY-MM-DD, the TAIFEX trading date
	Product       string
	ContractMonth string // 202610, 202610W1, ...
	Strike        float64
	CallPut       string
	Session       string
	ExpiryDate    string // YYYY-MM-DD from 契約到期日; empty if not given
	Open          *float64
	High          *float64
	Low           *float64
	Close         *float64
	Change        *float64
	Volume        *int64
	Settlement    *float64
	OpenInterest  *int64
	BestBid       *float64
	BestAsk       *float64
}

// IsWeekly reports whether the series is a weekly option: its contract
// month carries a week suffix such as W1 or F2.
func (d Daily) IsWeekly() bool { return len(d.ContractMonth) > 6 }

// Row converts d for market.Store.UpsertAll.
func (d Daily) Row() market.Row {
	var expiry any
	if d.ExpiryDate != "" {
		expiry = d.ExpiryDate
	}
	return market.Row{
		Key: []market.Col{{Name: "date", Value: d.Date}, {Name: "product", Value: d.Product},
			{Name: "contract_month", Value: d.ContractMonth}, {Name: "strike", Value: d.Strike},
			{Name: "call_put", Value: d.CallPut}, {Name: "session", Value: d.Session}},
		Data: []market.Col{
			{Name: "expiry_date", Value: expiry},
			{Name: "open", Value: d.Open}, {Name: "high", Value: d.High}, {Name: "low", Value: d.Low},
			{Name: "close", Value: d.Close}, {Name: "change", Value: d.Change}, {Name: "volume", Value: d.Volume},
			{Name: "settlement", Value: d.Settlement}, {Name: "open_interest", Value: d.OpenInterest},
			{Name: "best_bid", Value: d.BestBid}, {Name: "best_ask", Value: d.BestAsk},
		},
	}
}

var required = []string{"交易日期", "契約", "到期月份(週別)", "履約價", "買賣權", "開盤價", "最高價", "最低價",
	"收盤價", "成交量", "結算價", "未沖銷契約數", "最後最佳買價", "最後最佳賣價", "交易時段"}

// Parse decodes an optDataDown CSV, dropping other products than product
// (when non-empty).
func Parse(body []byte, product string) ([]Daily, error) {
	rows, err := market.ReadBig5CSV(body, required)
	if err != nil {
		return nil, err
	}
	out := make([]Daily, 0, len(rows))
	for _, r := range rows {
		if product != "" && r["契約"] != product {
			continue
		}
		d, err := parseRow(r)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

func parseRow(r map[string]string) (Daily, error) {
	var d Daily
	var err error
	if d.Date, err = market.SlashDate(r["交易日期"]); err != nil {
		return d, err
	}
	if d.Session, err = market.Session(r["交易時段"]); err != nil {
		return d, err
	}
	switch r["買賣權"] {
	case "買權":
		d.CallPut = Call
	case "賣權":
		d.CallPut = Put
	default:
		return d, market.Formatf("unknown option right %q", r["買賣權"])
	}
	d.Product = r["契約"]
	d.ContractMonth = strings.ReplaceAll(r["到期月份(週別)"], " ", "")
	strike, err := market.Float(r["履約價"])
	if err != nil {
		return d, err
	}
	if strike == nil || d.Product == "" || d.ContractMonth == "" {
		return d, market.Formatf("row without series: %v", r)
	}
	d.Strike = *strike
	if e := r["契約到期日"]; e != "" && e != "-" {
		if d.ExpiryDate, err = market.SlashDate(e); err != nil {
			return d, err
		}
	}
	floats := []struct {
		dst **float64
		col string
	}{{&d.Open, "開盤價"}, {&d.High, "最高價"}, {&d.Low, "最低價"}, {&d.Close, "收盤價"},
		{&d.Settlement, "結算價"}, {&d.BestBid, "最後最佳買價"}, {&d.BestAsk, "最後最佳賣價"}}
	for _, f := range floats {
		if *f.dst, err = market.Float(r[f.col]); err != nil {
			return d, err
		}
	}
	d.Change, _ = market.Float(r["漲跌價"]) // optional column
	if d.Volume, err = market.Int(r["成交量"]); err != nil {
		return d, err
	}
	if d.OpenInterest, err = market.Int(r["未沖銷契約數"]); err != nil {
		return d, err
	}
	return d, nil
}

// series identifies one expiry on one trading date.
type series struct{ date, month string }

// Selection is what to keep (SPEC-quotes: Config [[options]]).
type Selection struct {
	Expiries    int     // nearest N expiries per trading date; 0 = all
	Weekly      bool    // include weekly series
	StrikeRange float64 // keep strikes within ±StrikeRange of the ATM strike; 0 = all
	Sessions    []string
}

// Select applies sel to rows.
func Select(rows []Daily, sel Selection) []Daily {
	type expiry struct{ month, date string }
	byDate := map[string][]expiry{}
	for _, d := range rows {
		if d.IsWeekly() && !sel.Weekly {
			continue
		}
		e := expiry{d.ContractMonth, d.ExpiryDate}
		if !slices.Contains(byDate[d.Date], e) {
			byDate[d.Date] = append(byDate[d.Date], e)
		}
	}
	keepMonth := map[string]map[string]bool{}
	for date, es := range byDate {
		slices.SortFunc(es, func(a, b expiry) int {
			// Order by expiry date when known; the contract month breaks ties
			// and is the only key when the CSV has no 契約到期日 column.
			return cmp.Or(cmp.Compare(a.date, b.date), cmp.Compare(a.month, b.month))
		})
		if sel.Expiries > 0 && len(es) > sel.Expiries {
			es = es[:sel.Expiries]
		}
		keepMonth[date] = map[string]bool{}
		for _, e := range es {
			keepMonth[date][e.month] = true
		}
	}

	var atm map[series]float64
	if sel.StrikeRange > 0 {
		atm = atTheMoney(rows)
	}
	var out []Daily
	for _, d := range rows {
		if !slices.Contains(sel.Sessions, d.Session) || !keepMonth[d.Date][d.ContractMonth] {
			continue
		}
		if sel.StrikeRange > 0 {
			if a, ok := atm[series{d.Date, d.ContractMonth}]; ok && math.Abs(d.Strike-a) > sel.StrikeRange*a+1e-9 {
				continue
			}
		}
		out = append(out, d)
	}
	return out
}

// atTheMoney estimates the ATM strike of each (date, contract month) as the
// strike where the regular-session call and put settlement prices are
// closest (put-call parity), so no underlying index price is needed.
// Series without any strike quoted on both sides are left out, which keeps
// all their strikes.
func atTheMoney(rows []Daily) map[series]float64 {
	type key struct {
		date, month string
		strike      float64
	}
	calls, puts := map[key]float64{}, map[key]float64{}
	for _, d := range rows {
		if d.Session != market.Regular || d.Settlement == nil {
			continue
		}
		k := key{d.Date, d.ContractMonth, d.Strike}
		if d.CallPut == Call {
			calls[k] = *d.Settlement
		} else {
			puts[k] = *d.Settlement
		}
	}
	best := map[series]float64{}
	gap := map[series]float64{}
	for k, c := range calls {
		p, ok := puts[k]
		if !ok {
			continue
		}
		s := series{k.date, k.month}
		g := math.Abs(c - p)
		if old, seen := gap[s]; !seen || g < old || (g == old && k.strike < best[s]) {
			gap[s], best[s] = g, k.strike
		}
	}
	return best
}
