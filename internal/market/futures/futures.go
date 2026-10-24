// Package futures parses TAIFEX daily futures data and selects the
// contract months configured for collection.
package futures

import (
	"cmp"
	"slices"
	"strings"

	"github.com/elvis3434/gleaner24/internal/market"
)

// Table is the database table for Daily rows.
const Table = "futures_daily"

// Daily is one contract month (or spread) in one session of one day.
type Daily struct {
	Date          string // YYYY-MM-DD, the TAIFEX trading date
	Product       string
	ContractMonth string // 202610, 202610W1, or 202610/202611 for spreads
	Session       string // market.Regular or market.AfterHours
	Open          *float64
	High          *float64
	Low           *float64
	Close         *float64
	Change        *float64
	Volume        *int64
	Settlement    *float64 // absent for after-hours rows
	OpenInterest  *int64   // absent for after-hours rows
	BestBid       *float64
	BestAsk       *float64
}

// IsSpread reports whether the row is a calendar spread.
func (d Daily) IsSpread() bool { return strings.Contains(d.ContractMonth, "/") }

// Row converts d for market.Store.UpsertAll.
func (d Daily) Row() market.Row {
	return market.Row{
		Key: []market.Col{{Name: "date", Value: d.Date}, {Name: "product", Value: d.Product},
			{Name: "contract_month", Value: d.ContractMonth}, {Name: "session", Value: d.Session}},
		Data: []market.Col{
			{Name: "open", Value: d.Open}, {Name: "high", Value: d.High}, {Name: "low", Value: d.Low},
			{Name: "close", Value: d.Close}, {Name: "change", Value: d.Change}, {Name: "volume", Value: d.Volume},
			{Name: "settlement", Value: d.Settlement}, {Name: "open_interest", Value: d.OpenInterest},
			{Name: "best_bid", Value: d.BestBid}, {Name: "best_ask", Value: d.BestAsk},
		},
	}
}

var required = []string{"交易日期", "契約", "到期月份(週別)", "開盤價", "最高價", "最低價", "收盤價",
	"成交量", "結算價", "未沖銷契約數", "最後最佳買價", "最後最佳賣價", "交易時段"}

// Parse decodes a futDataDown CSV. Rows for other products than product
// (when non-empty) are dropped.
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
	d.Product = r["契約"]
	d.ContractMonth = strings.ReplaceAll(r["到期月份(週別)"], " ", "")
	if d.Product == "" || d.ContractMonth == "" {
		return d, market.Formatf("row without contract: %v", r)
	}
	floats := []struct {
		dst **float64
		col string
	}{{&d.Open, "開盤價"}, {&d.High, "最高價"}, {&d.Low, "最低價"}, {&d.Close, "收盤價"},
		{&d.Change, "漲跌價"}, {&d.Settlement, "結算價"}, {&d.BestBid, "最後最佳買價"}, {&d.BestAsk, "最後最佳賣價"}}
	for _, f := range floats {
		if *f.dst, err = market.Float(r[f.col]); err != nil {
			return d, err
		}
	}
	if d.Volume, err = market.Int(r["成交量"]); err != nil {
		return d, err
	}
	if d.OpenInterest, err = market.Int(r["未沖銷契約數"]); err != nil {
		return d, err
	}
	return d, nil
}

// Select keeps, for each trading date, the nearest months contract months
// (all when months is 0) in the given sessions. Spreads are kept only when
// includeSpreads is set and every leg is a selected month.
func Select(rows []Daily, months int, includeSpreads bool, sessions []string) []Daily {
	chosen := map[string]map[string]bool{} // date -> selected months
	byDate := map[string][]string{}
	for _, d := range rows {
		if !d.IsSpread() && !slices.Contains(byDate[d.Date], d.ContractMonth) {
			byDate[d.Date] = append(byDate[d.Date], d.ContractMonth)
		}
	}
	for date, ms := range byDate {
		slices.SortFunc(ms, cmp.Compare[string])
		if months > 0 && len(ms) > months {
			ms = ms[:months]
		}
		chosen[date] = map[string]bool{}
		for _, m := range ms {
			chosen[date][m] = true
		}
	}
	var out []Daily
	for _, d := range rows {
		if !slices.Contains(sessions, d.Session) {
			continue
		}
		if d.IsSpread() {
			if !includeSpreads {
				continue
			}
			legsOK := true
			for _, leg := range strings.Split(d.ContractMonth, "/") {
				legsOK = legsOK && chosen[d.Date][leg]
			}
			if !legsOK {
				continue
			}
		} else if !chosen[d.Date][d.ContractMonth] {
			continue
		}
		out = append(out, d)
	}
	return out
}
