package market

import (
	"net/url"
	"time"
)

// TAIFEX daily trading data downloads (POST, CSV in cp950).
const (
	FuturesDownloadURL = "https://www.taifex.com.tw/cht/3/futDataDown"
	OptionsDownloadURL = "https://www.taifex.com.tw/cht/3/optDataDown"
)

// TaifexForm is the query for one product over [from, to]. TAIFEX accepts
// at most one month per query; use MonthChunks to split longer ranges.
func TaifexForm(product string, from, to time.Time) url.Values {
	return url.Values{
		"down_type":      {"1"},
		"commodity_id":   {product},
		"commodity_id2":  {""},
		"queryStartDate": {from.Format("2006/01/02")},
		"queryEndDate":   {to.Format("2006/01/02")},
	}
}

// Span is an inclusive date range.
type Span struct{ From, To time.Time }

// MonthChunks splits [from, to] at calendar month boundaries.
func MonthChunks(from, to time.Time) []Span {
	var out []Span
	for start := from; !start.After(to); {
		end := time.Date(start.Year(), start.Month()+1, 1, 0, 0, 0, 0, start.Location()).AddDate(0, 0, -1)
		if end.After(to) {
			end = to
		}
		out = append(out, Span{start, end})
		start = end.AddDate(0, 0, 1)
	}
	return out
}

// Months returns the first day of every month that [from, to] touches.
func Months(from, to time.Time) []time.Time {
	var out []time.Time
	for _, s := range MonthChunks(from, to) {
		out = append(out, time.Date(s.From.Year(), s.From.Month(), 1, 0, 0, 0, 0, s.From.Location()))
	}
	return out
}
