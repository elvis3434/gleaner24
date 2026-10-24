package stock

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/elvis3434/gleaner24/internal/market"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "quotes", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestURL(t *testing.T) {
	day := time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)
	if got := URL(TWSE, "2330", day); got != "https://www.twse.com.tw/exchangeReport/STOCK_DAY?date=20260901&response=json&stockNo=2330" {
		t.Errorf("TWSE URL = %s", got)
	}
	if got := URL(TPEx, "6223", day); got != "https://www.tpex.org.tw/www/zh-tw/afterTrading/tradingStock?code=6223&date=2026%2F09%2F01&response=json" {
		t.Errorf("TPEx URL = %s", got)
	}
}

func TestParseTWSEOfficial(t *testing.T) {
	rows, err := ParseTWSE("2330", fixture(t, "twse_stock_day_2330_202609.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 20 {
		t.Fatalf("rows = %d", len(rows))
	}
	d := rows[0]
	if d.Date != "2026-09-01" || d.Symbol != "2330" || d.Market != TWSE ||
		*d.Volume != 31855287 || *d.Turnover != 77463413685 ||
		*d.Open != 2395 || *d.High != 2440 || *d.Low != 2390 || *d.Close != 2440 ||
		*d.Change != 35 || *d.Transactions != 65271 {
		t.Errorf("row 0 = %+v", d)
	}
	if *rows[1].Change != -55 {
		t.Errorf("negative change = %v", *rows[1].Change)
	}
	// 2026-09-16 is marked X0.00 (不比價).
	for _, r := range rows {
		if r.Date == "2026-09-16" && r.Change != nil {
			t.Errorf("X change = %v, want nil", *r.Change)
		}
		if r.Date == "2026-09-29" && (r.Change == nil || *r.Change != 0) {
			t.Errorf("zero change = %v", r.Change)
		}
	}
	if rows[19].Date != "2026-09-30" {
		t.Errorf("last date = %s", rows[19].Date)
	}
}

func TestParseTPExOfficial(t *testing.T) {
	rows, err := Parse(TPEx, "6223", fixture(t, "tpex_trading_stock_6223_202609.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 20 {
		t.Fatalf("rows = %d", len(rows))
	}
	d := rows[0]
	// 1,649 張 = 1,649,000 shares; 8,865,194 仟元 = 8,865,194,000 NTD.
	if d.Date != "2026-09-01" || d.Market != TPEx || *d.Volume != 1649000 || *d.Turnover != 8865194000 ||
		*d.Open != 5200 || *d.High != 5585 || *d.Low != 5090 || *d.Close != 5410 || *d.Change != 280 || *d.Transactions != 16130 {
		t.Errorf("row 0 = %+v", d)
	}
	if *rows[1].Change != -120 {
		t.Errorf("negative change = %v", *rows[1].Change)
	}
}

func TestParseNoData(t *testing.T) {
	_, err := ParseTWSE("9999", []byte(`{"stat":"很抱歉，沒有符合條件的資料!"}`))
	if !errors.Is(err, market.ErrNoData) || !strings.Contains(err.Error(), "listed") {
		t.Errorf("TWSE no data: %v", err)
	}
	_, err = ParseTPEx("2330", []byte(`{"tables":[{"title":"個股日成交資訊","data":[],"fields":[],"totalCount":0}],"stat":"ok"}`))
	if !errors.Is(err, market.ErrNoData) {
		t.Errorf("TPEx no data: %v", err)
	}
}

func TestParseFormatChanges(t *testing.T) {
	cases := map[string]string{
		"not json":      `<html>blocked</html>`,
		"missing field": `{"stat":"OK","fields":["日期","成交股數"],"data":[["115/09/01","1"]]}`,
		"short row":     `{"stat":"OK","fields":["日期","成交股數","成交金額","開盤價","最高價","最低價","收盤價","漲跌價差","成交筆數"],"data":[["115/09/01","1"]]}`,
		"bad number":    `{"stat":"OK","fields":["日期","成交股數","成交金額","開盤價","最高價","最低價","收盤價","漲跌價差","成交筆數"],"data":[["115/09/01","1","2","abc","1","1","1","+1","1"]]}`,
	}
	for name, body := range cases {
		if _, err := ParseTWSE("2330", []byte(body)); !errors.Is(err, market.ErrFormat) {
			t.Errorf("%s: err = %v, want ErrFormat", name, err)
		}
	}
	// A TPEx report giving shares and dollars directly is not rescaled.
	body := `{"stat":"ok","tables":[{"fields":["日 期","成交股數","成交金額","開盤","最高","最低","收盤","漲跌","筆數"],"data":[["115/09/01","1,000","5,000","5","5","5","5","0","1"]]}]}`
	rows, err := ParseTPEx("x", []byte(body))
	if err != nil || *rows[0].Volume != 1000 || *rows[0].Turnover != 5000 {
		t.Errorf("unscaled TPEx = %+v, %v", rows, err)
	}
}

func TestRow(t *testing.T) {
	rows, _ := ParseTWSE("2330", fixture(t, "twse_stock_day_2330_202609.json"))
	r := rows[0].Row()
	if len(r.Key) != 2 || r.Key[0].Value != "2026-09-01" || r.Key[1].Value != "2330" || len(r.Data) != 10 {
		t.Errorf("row = %+v", r)
	}
}
