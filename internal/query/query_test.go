package query

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestMatch(t *testing.T) {
	tests := []struct {
		name string
		expr string
		text string
		want bool
	}{
		{"single term hit", "TSMC", "TSMC reports revenue", true},
		{"single term miss", "TSMC", "Intel reports revenue", false},
		{"AND both", "TSMC AND revenue", "TSMC revenue up", true},
		{"AND one missing", "TSMC AND revenue", "TSMC profit up", false},
		{"implicit AND", "TSMC revenue", "TSMC revenue up", true},
		{"implicit AND miss", "TSMC revenue", "TSMC profit up", false},
		{"OR first", "TSMC OR Intel", "TSMC news", true},
		{"OR second", "TSMC OR Intel", "Intel news", true},
		{"OR none", "TSMC OR Intel", "AMD news", false},
		{"NOT excludes", "NOT rumor", "a rumor about chips", false},
		{"NOT allows", "NOT rumor", "official chips news", true},
		{"term NOT term", "TSMC NOT rumor", "TSMC rumor", false},
		{"term NOT term ok", "TSMC NOT rumor", "TSMC confirmed", true},
		{"AND binds tighter than OR", "a OR b AND c", "a", true},
		{"AND binds tighter than OR 2", "a OR b AND c", "b", false},
		{"parens override", "(a OR b) AND c", "a", false},
		{"parens override 2", "(a OR b) AND c", "b c", true},
		{"NOT binds tighter than AND", "NOT a AND b", "b", true},
		{"NOT binds tighter than AND 2", "NOT a AND b", "a b", false},
		{"NOT of group", "NOT (a OR b)", "c", true},
		{"NOT of group 2", "NOT (a OR b)", "b", false},
		{"double NOT", "NOT NOT a", "a", true},
		{"nested parens", "((a OR b) AND (c OR d)) NOT e", "b d", true},
		{"nested parens excluded", "((a OR b) AND (c OR d)) NOT e", "b d e", false},
		{"quoted phrase", `"interest rate" AND hike`, "the interest rate hike", true},
		{"quoted phrase needs exact order", `"interest rate"`, "rate of interest", false},
		{"quoted operator word", `"AND"`, "this AND that", true},
		{"lowercase operators are keywords", "and", "salt and pepper", true},
		{"case insensitive", "tsmc", "TSMC Q3", true},
		{"cjk substring", "台積電", "今天台積電公布營收", true},
		{"spec example hit", "(台積電 OR TSMC) AND (營收 OR 財報) NOT 傳聞", "台積電公布第三季營收", true},
		{"spec example excluded", "(台積電 OR TSMC) AND (營收 OR 財報) NOT 傳聞", "台積電營收傳聞", false},
		{"spec example miss", "(台積電 OR TSMC) AND (營收 OR 財報) NOT 傳聞", "台積電新廠動工", false},
		{"full-width text", "TSMC", "ＴＳＭＣ 營收", true},
		{"full-width keyword", "ＴＳＭＣ", "tsmc", true},
		{"full-width parens", "（a OR b） AND c", "b c", true},
		{"half-width katakana", "カタカナ", "ｶﾀｶﾅ表記", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := Parse(tt.expr, Options{})
			if err != nil {
				t.Fatalf("Parse(%q): %v", tt.expr, err)
			}
			got, _ := q.Match(tt.text)
			if got != tt.want {
				t.Errorf("Parse(%q).Match(%q) = %v, want %v", tt.expr, tt.text, got, tt.want)
			}
		})
	}
}

func TestCaseSensitive(t *testing.T) {
	q, err := Parse("TSMC", Options{CaseSensitive: true})
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := q.Match("tsmc"); ok {
		t.Error("case-sensitive query matched different case")
	}
	if ok, _ := q.Match("TSMC"); !ok {
		t.Error("case-sensitive query missed exact case")
	}
}

func TestTransform(t *testing.T) {
	// A toy Simplified-to-Traditional mapping standing in for OpenCC.
	s2t := strings.NewReplacer("台积电", "台積電", "营收", "營收")
	q, err := Parse("台積電 AND 營收", Options{Transform: s2t.Replace})
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := q.Match("台积电营收创新高"); !ok {
		t.Error("transform not applied to text")
	}
}

func TestMatchedKeywords(t *testing.T) {
	q, err := Parse(`(台積電 OR TSMC) AND (營收 OR 財報) NOT 傳聞 OR "chip war"`, Options{})
	if err != nil {
		t.Fatal(err)
	}
	ok, kws := q.Match("TSMC 台積電 營收 報告")
	if !ok {
		t.Fatal("expected match")
	}
	want := []string{"台積電", "TSMC", "營收"}
	if !reflect.DeepEqual(kws, want) {
		t.Errorf("matched = %v, want %v", kws, want)
	}

	// Keywords under NOT are never reported, and a miss reports nothing.
	if ok, kws := q.Match("傳聞"); ok || kws != nil {
		t.Errorf("Match(傳聞) = %v, %v; want false, nil", ok, kws)
	}

	// Duplicated keywords are reported once.
	q2, _ := Parse("a AND (a OR b)", Options{})
	_, kws = q2.Match("a b")
	if !reflect.DeepEqual(kws, []string{"a", "b"}) {
		t.Errorf("dedup matched = %v", kws)
	}
}

func TestSyntaxErrors(t *testing.T) {
	tests := []struct {
		expr    string
		pos     int
		msgPart string
	}{
		{"", 1, "empty expression"},
		{"   ", 1, "empty expression"},
		{"a AND", 6, "ends where a keyword"},
		{"OR a", 1, "found OR"},
		{"a OR OR b", 6, "found OR"},
		{"NOT", 4, "ends where a keyword"},
		{"(a OR b", 1, `unclosed "("`},
		{"a OR b)", 7, `unmatched ")"`},
		{"()", 2, "empty parentheses"},
		{`"interest rate`, 1, "unterminated"},
		{`a AND ""`, 7, "empty quoted phrase"},
		{"a AND )", 7, `found ")"`},
		{"台積電 AND (營收 OR", 15, "ends where a keyword"},
	}
	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			_, err := Parse(tt.expr, Options{})
			var se *SyntaxError
			if !errors.As(err, &se) {
				t.Fatalf("Parse(%q) error = %v, want *SyntaxError", tt.expr, err)
			}
			if se.Pos != tt.pos {
				t.Errorf("Parse(%q) pos = %d, want %d (%v)", tt.expr, se.Pos, tt.pos, se)
			}
			if !strings.Contains(se.Msg, tt.msgPart) {
				t.Errorf("Parse(%q) msg = %q, want it to contain %q", tt.expr, se.Msg, tt.msgPart)
			}
		})
	}
}

func TestErrorString(t *testing.T) {
	_, err := Parse("a OR", Options{})
	if err == nil || !strings.HasPrefix(err.Error(), "column 5:") {
		t.Errorf("error = %v, want prefix %q", err, "column 5:")
	}
}
