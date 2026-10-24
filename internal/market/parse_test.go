package market

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/text/encoding/traditionalchinese"
)

func TestFloat(t *testing.T) {
	tests := map[string]any{
		"2,440.00": 2440.0, "+35.00": 35.0, "-55.00": -55.0, " 0.00": 0.0,
		"2.63%": 2.63, "42800.0000": 42800.0,
		"-": nil, "--": nil, "NULL": nil, "": nil,
	}
	for in, want := range tests {
		got, err := Float(in)
		if err != nil {
			t.Errorf("Float(%q) error %v", in, err)
			continue
		}
		if want == nil {
			if got != nil {
				t.Errorf("Float(%q) = %v, want nil", in, *got)
			}
		} else if got == nil || *got != want.(float64) {
			t.Errorf("Float(%q) = %v, want %v", in, got, want)
		}
	}
	if _, err := Float("X0.00"); !errors.Is(err, ErrFormat) {
		t.Errorf("Float(X0.00) err = %v", err)
	}
}

func TestInt(t *testing.T) {
	v, err := Int("31,855,287")
	if err != nil || *v != 31855287 {
		t.Errorf("Int = %v, %v", v, err)
	}
	v, _ = Int("12.0")
	if *v != 12 {
		t.Errorf("Int(12.0) = %d", *v)
	}
	if v, _ := Int("-"); v != nil {
		t.Error("Int(-) not nil")
	}
	if _, err := Int("1.5"); !errors.Is(err, ErrFormat) {
		t.Error("Int(1.5) accepted")
	}
	if s := Scale(Int64(3), 1000); *s != 3000 || Scale(nil, 10) != nil {
		t.Error("Scale wrong")
	}
}

func Int64(v int64) *int64 { return &v }

func TestDates(t *testing.T) {
	for in, want := range map[string]string{"115/09/01": "2026-09-01", "＊115/09/30": "2026-09-30", "101/1/2": "2012-01-02"} {
		if got, err := ROCDate(in); err != nil || got != want {
			t.Errorf("ROCDate(%q) = %q, %v", in, got, err)
		}
	}
	for _, bad := range []string{"2026-09-01", "115/13/01", "abc"} {
		if _, err := ROCDate(bad); err == nil {
			t.Errorf("ROCDate(%q) accepted", bad)
		}
	}
	for in, want := range map[string]string{"2026/10/05": "2026-10-05", "20261007": "2026-10-07"} {
		if got, err := SlashDate(in); err != nil || got != want {
			t.Errorf("SlashDate(%q) = %q, %v", in, got, err)
		}
	}
}

func TestSession(t *testing.T) {
	if s, _ := Session("一般"); s != Regular {
		t.Error("一般")
	}
	if s, _ := Session("盤後"); s != AfterHours {
		t.Error("盤後")
	}
	if _, err := Session("夜盤"); !errors.Is(err, ErrFormat) {
		t.Error("unknown session accepted")
	}
}

func TestReadBig5CSVOfficialSample(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "quotes", "taifex_fut_TX_20261005.csv"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := ReadBig5CSV(body, []string{"交易日期", "契約", "到期月份(週別)", "交易時段"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 23 {
		t.Errorf("rows = %d", len(rows))
	}
	if r := rows[0]; r["契約"] != "TX" || r["到期月份(週別)"] != "202610" || r["交易時段"] != "一般" || r["收盤價"] != "49949" {
		t.Errorf("row 0 = %v", r)
	}
}

func big5(t *testing.T, s string) []byte {
	t.Helper()
	b, err := traditionalchinese.Big5.NewEncoder().Bytes([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestReadBig5CSVErrors(t *testing.T) {
	if _, err := ReadBig5CSV(nil, nil); !errors.Is(err, ErrNoData) {
		t.Errorf("empty: %v", err)
	}
	if _, err := ReadBig5CSV(big5(t, "<html><body>查無資料</body></html>"), nil); !errors.Is(err, ErrNoData) {
		t.Errorf("no-data page: %v", err)
	}
	if _, err := ReadBig5CSV(big5(t, "<html>系統錯誤</html>"), nil); !errors.Is(err, ErrFormat) {
		t.Errorf("error page: %v", err)
	}
	if _, err := ReadBig5CSV(big5(t, "交易日期,契約\r\n"), []string{"交易日期"}); !errors.Is(err, ErrNoData) {
		t.Errorf("header only: %v", err)
	}
	if _, err := ReadBig5CSV(big5(t, "交易日期,契約\r\n2026/10/05,TX\r\n"), []string{"結算價"}); !errors.Is(err, ErrFormat) {
		t.Errorf("missing column: %v", err)
	}
	if _, err := ReadBig5CSV(big5(t, "交易日期,契約,結算價\r\n2026/10/05,TX\r\n"), []string{"結算價"}); !errors.Is(err, ErrFormat) {
		t.Errorf("short row: %v", err)
	}
}
