// Package market holds what the quotes collector's instrument packages
// share: value parsing, the config, and the SQLite store.
package market

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/transform"
)

// ErrFormat marks a response whose layout is not what the parser expects,
// typically because the exchange changed its format. Nothing from such a
// response is stored.
var ErrFormat = errors.New("unexpected response format")

// ErrNoData marks a well-formed response that holds no rows (holiday,
// unknown symbol, data not yet published).
var ErrNoData = errors.New("no data")

// Formatf wraps ErrFormat with details.
func Formatf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrFormat, fmt.Sprintf(format, args...))
}

// Session values.
const (
	Regular    = "regular"
	AfterHours = "after_hours"
)

// Session maps TAIFEX's 交易時段 to Regular or AfterHours.
func Session(s string) (string, error) {
	switch strings.TrimSpace(s) {
	case "一般":
		return Regular, nil
	case "盤後":
		return AfterHours, nil
	}
	return "", Formatf("unknown trading session %q", s)
}

func isMissing(s string) bool {
	switch s {
	case "", "-", "--", "---", "NULL", "null", "N/A":
		return true
	}
	return false
}

func clean(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, ",", "")
	return strings.TrimSuffix(s, "%")
}

// Float parses an exchange number such as "2,440.00", "+35.00" or "-0.5".
// Placeholders for missing values ("-", "--", "NULL", "") give nil.
func Float(s string) (*float64, error) {
	s = clean(s)
	if isMissing(s) {
		return nil, nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil, Formatf("not a number: %q", s)
	}
	return &v, nil
}

// Int parses an integer such as "31,855,287". Missing values give nil.
func Int(s string) (*int64, error) {
	s = clean(s)
	if isMissing(s) {
		return nil, nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		// Some volume columns are written with a decimal part ("1234.0").
		f, ferr := strconv.ParseFloat(s, 64)
		if ferr != nil || f != float64(int64(f)) {
			return nil, Formatf("not an integer: %q", s)
		}
		v = int64(f)
	}
	return &v, nil
}

// Scale multiplies a parsed integer (used for 張 → 股 and 仟元 → 元).
func Scale(v *int64, by int64) *int64 {
	if v == nil {
		return nil
	}
	r := *v * by
	return &r
}

// ROCDate converts a Minguo date such as "115/09/01" to "2026-09-01".
// Marker characters around the date (e.g. "＊") are ignored.
func ROCDate(s string) (string, error) {
	s = strings.Map(func(r rune) rune {
		if (r >= '0' && r <= '9') || r == '/' {
			return r
		}
		return -1
	}, s)
	parts := strings.Split(s, "/")
	if len(parts) != 3 {
		return "", Formatf("not a ROC date: %q", s)
	}
	y, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	d, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil || y < 1 || m < 1 || m > 12 || d < 1 || d > 31 {
		return "", Formatf("not a ROC date: %q", s)
	}
	return fmt.Sprintf("%04d-%02d-%02d", y+1911, m, d), nil
}

// SlashDate converts "2026/10/05" or "20261005" to "2026-10-05".
func SlashDate(s string) (string, error) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{"2006/01/02", "20060102", "2006/1/2"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("2006-01-02"), nil
		}
	}
	return "", Formatf("not a date: %q", s)
}

// ReadBig5CSV decodes a TAIFEX CSV download (cp950/Big5) and returns one
// map per data row keyed by the header names (white space trimmed). Every
// name in required must be present in the header.
func ReadBig5CSV(body []byte, required []string) ([]map[string]string, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, ErrNoData
	}
	if trimmed[0] == '<' {
		// TAIFEX answers queries without data, and bad queries, with HTML.
		text, _ := decodeBig5(trimmed)
		if strings.Contains(text, "查無資料") {
			return nil, ErrNoData
		}
		return nil, Formatf("HTML page instead of CSV")
	}
	text, err := decodeBig5(body)
	if err != nil {
		return nil, Formatf("cannot decode Big5: %v", err)
	}
	r := csv.NewReader(strings.NewReader(strings.TrimPrefix(text, "\uFEFF")))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	header, err := r.Read()
	if err == io.EOF {
		return nil, ErrNoData
	}
	if err != nil {
		return nil, Formatf("bad CSV header: %v", err)
	}
	idx := map[string]int{}
	for i, h := range header {
		if h = strings.TrimSpace(h); h != "" {
			idx[h] = i
		}
	}
	for _, name := range required {
		if _, ok := idx[name]; !ok {
			return nil, Formatf("CSV header lacks %q (header: %v)", name, header)
		}
	}
	var rows []map[string]string
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, Formatf("bad CSV row: %v", err)
		}
		if len(rec) == 0 || strings.TrimSpace(strings.Join(rec, "")) == "" {
			continue
		}
		row := make(map[string]string, len(idx))
		for name, i := range idx {
			if i < len(rec) {
				row[name] = strings.TrimSpace(rec[i])
			} else if slicesContains(required, name) {
				return nil, Formatf("CSV row too short: %v", rec)
			}
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil, ErrNoData
	}
	return rows, nil
}

func slicesContains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func decodeBig5(b []byte) (string, error) {
	out, _, err := transform.Bytes(traditionalchinese.Big5.NewDecoder(), b)
	return string(out), err
}
