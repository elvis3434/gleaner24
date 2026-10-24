// Package stock fetches and parses daily trading data of individual listed
// (TWSE) and OTC (TPEx) stocks.
package stock

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/elvis3434/gleaner24/internal/market"
)

// Markets.
const (
	TWSE = "twse"
	TPEx = "tpex"
)

// Table is the database table for Daily rows.
const Table = "stock_daily"

// Daily is one trading day of one stock. Volume is in shares and Turnover
// in NT dollars for both markets.
type Daily struct {
	Date         string // YYYY-MM-DD
	Symbol       string
	Market       string
	Volume       *int64
	Turnover     *int64
	Open         *float64
	High         *float64
	Low          *float64
	Close        *float64
	Change       *float64 // nil when TWSE marks the day X (no comparison)
	Transactions *int64
	Note         string
}

// Row converts d for market.Store.UpsertAll.
func (d Daily) Row() market.Row {
	return market.Row{
		Key: []market.Col{{Name: "date", Value: d.Date}, {Name: "symbol", Value: d.Symbol}},
		Data: []market.Col{
			{Name: "market", Value: d.Market}, {Name: "volume", Value: d.Volume}, {Name: "turnover", Value: d.Turnover},
			{Name: "open", Value: d.Open}, {Name: "high", Value: d.High}, {Name: "low", Value: d.Low},
			{Name: "close", Value: d.Close}, {Name: "change", Value: d.Change},
			{Name: "transactions", Value: d.Transactions}, {Name: "note", Value: d.Note},
		},
	}
}

// URL returns the monthly report URL for symbol in market for the month
// containing day.
func URL(mkt, symbol string, day time.Time) string {
	switch mkt {
	case TPEx:
		q := url.Values{"date": {day.Format("2006/01") + "/01"}, "code": {symbol}, "response": {"json"}}
		return "https://www.tpex.org.tw/www/zh-tw/afterTrading/tradingStock?" + q.Encode()
	default:
		q := url.Values{"response": {"json"}, "date": {day.Format("200601") + "01"}, "stockNo": {symbol}}
		return "https://www.twse.com.tw/exchangeReport/STOCK_DAY?" + q.Encode()
	}
}

// Parse decodes a monthly report of the given market.
func Parse(mkt, symbol string, body []byte) ([]Daily, error) {
	if mkt == TPEx {
		return ParseTPEx(symbol, body)
	}
	return ParseTWSE(symbol, body)
}

// column finds the position of each wanted field (white space in field
// names is ignored, since TPEx writes "日 期").
func columns(fields []string, wanted ...string) (map[string]int, error) {
	idx := map[string]int{}
	for i, f := range fields {
		idx[strings.Join(strings.Fields(f), "")] = i
	}
	out := map[string]int{}
	for _, w := range wanted {
		i, ok := idx[w]
		if !ok {
			return nil, market.Formatf("field %q missing (fields: %v)", w, fields)
		}
		out[w] = i
	}
	return out, nil
}

// firstOf returns the name and position of the first field present.
func firstOf(fields []string, names ...string) (string, int, bool) {
	for _, n := range names {
		if c, err := columns(fields, n); err == nil {
			return n, c[n], true
		}
	}
	return "", 0, false
}

type twseReport struct {
	Stat   string     `json:"stat"`
	Fields []string   `json:"fields"`
	Data   [][]string `json:"data"`
}

// ParseTWSE decodes exchangeReport/STOCK_DAY.
func ParseTWSE(symbol string, body []byte) ([]Daily, error) {
	var r twseReport
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, market.Formatf("TWSE JSON: %v", err)
	}
	if !strings.EqualFold(r.Stat, "OK") {
		return nil, fmt.Errorf("%w: TWSE says %q (is %s a listed stock?)", market.ErrNoData, r.Stat, symbol)
	}
	if len(r.Data) == 0 {
		return nil, market.ErrNoData
	}
	c, err := columns(r.Fields, "日期", "成交股數", "成交金額", "開盤價", "最高價", "最低價", "收盤價", "漲跌價差", "成交筆數")
	if err != nil {
		return nil, err
	}
	noteCol, _ := columns(r.Fields, "註記")
	return parseRows(r.Data, symbol, TWSE, c, func(row []string, d *Daily) error {
		var err error
		if d.Volume, err = market.Int(row[c["成交股數"]]); err != nil {
			return err
		}
		if d.Turnover, err = market.Int(row[c["成交金額"]]); err != nil {
			return err
		}
		if d.Transactions, err = market.Int(row[c["成交筆數"]]); err != nil {
			return err
		}
		// "+35.00", "-55.00", " 0.00"; a leading X means 不比價 (no comparison).
		if ch := strings.TrimSpace(row[c["漲跌價差"]]); !strings.HasPrefix(ch, "X") {
			if d.Change, err = market.Float(ch); err != nil {
				return err
			}
		}
		if i, ok := noteCol["註記"]; ok && i < len(row) {
			d.Note = strings.TrimSpace(row[i])
		}
		return nil
	}, "日期", "開盤價", "最高價", "最低價", "收盤價")
}

type tpexReport struct {
	Stat   string `json:"stat"`
	Tables []struct {
		Fields []string   `json:"fields"`
		Data   [][]string `json:"data"`
	} `json:"tables"`
}

// ParseTPEx decodes www/zh-tw/afterTrading/tradingStock. TPEx reports
// volume in lots (張, 1,000 shares) and turnover in thousands of NT
// dollars (仟元); both are converted.
func ParseTPEx(symbol string, body []byte) ([]Daily, error) {
	var r tpexReport
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, market.Formatf("TPEx JSON: %v", err)
	}
	if !strings.EqualFold(r.Stat, "ok") {
		return nil, fmt.Errorf("%w: TPEx says %q", market.ErrNoData, r.Stat)
	}
	if len(r.Tables) == 0 || len(r.Tables[0].Data) == 0 {
		return nil, fmt.Errorf("%w: no rows from TPEx (is %s an OTC stock?)", market.ErrNoData, symbol)
	}
	t := r.Tables[0]
	c, err := columns(t.Fields, "日期", "開盤", "最高", "最低", "收盤", "漲跌", "筆數")
	if err != nil {
		return nil, err
	}
	volName, volCol, ok := firstOf(t.Fields, "成交張數", "成交股數")
	if !ok {
		return nil, market.Formatf("TPEx volume field missing (fields: %v)", t.Fields)
	}
	amtName, amtCol, ok := firstOf(t.Fields, "成交仟元", "成交金額")
	if !ok {
		return nil, market.Formatf("TPEx turnover field missing (fields: %v)", t.Fields)
	}
	volScale, amtScale := int64(1), int64(1)
	if volName == "成交張數" {
		volScale = 1000
	}
	if amtName == "成交仟元" {
		amtScale = 1000
	}
	c[volName], c[amtName] = volCol, amtCol
	return parseRows(t.Data, symbol, TPEx, c, func(row []string, d *Daily) error {
		v, err := market.Int(row[volCol])
		if err != nil {
			return err
		}
		d.Volume = market.Scale(v, volScale)
		a, err := market.Int(row[amtCol])
		if err != nil {
			return err
		}
		d.Turnover = market.Scale(a, amtScale)
		if d.Transactions, err = market.Int(row[c["筆數"]]); err != nil {
			return err
		}
		// TPEx writes the change without "+"; non-numeric marks give nil.
		d.Change, _ = market.Float(row[c["漲跌"]])
		return nil
	}, "日期", "開盤", "最高", "最低", "收盤")
}

// parseRows handles the columns both markets share (date and OHLC) and
// calls extra for the rest. names lists the date and OHLC field names.
func parseRows(data [][]string, symbol, mkt string, c map[string]int, extra func([]string, *Daily) error, names ...string) ([]Daily, error) {
	maxCol := 0
	for _, i := range c {
		maxCol = max(maxCol, i)
	}
	out := make([]Daily, 0, len(data))
	for _, row := range data {
		if len(row) <= maxCol {
			return nil, market.Formatf("row too short: %v", row)
		}
		date, err := market.ROCDate(row[c[names[0]]])
		if err != nil {
			return nil, err
		}
		d := Daily{Date: date, Symbol: symbol, Market: mkt}
		for i, dst := range []**float64{&d.Open, &d.High, &d.Low, &d.Close} {
			if *dst, err = market.Float(row[c[names[i+1]]]); err != nil {
				return nil, fmt.Errorf("%s %s: %w", symbol, date, err)
			}
		}
		if err := extra(row, &d); err != nil {
			return nil, fmt.Errorf("%s %s: %w", symbol, date, err)
		}
		out = append(out, d)
	}
	return out, nil
}
