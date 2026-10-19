package runlog

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var taipei = time.FixedZone("CST", 8*3600)

func TestCreateUniqueNames(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 10, 19, 9, 5, 0, 0, taipei)
	var paths []string
	for i := 0; i < 3; i++ {
		l, err := Create(dir, at, nil)
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, filepath.Base(l.Path()))
		l.Close()
	}
	want := []string{"collect_20261019_0905.log", "collect_20261019_0905_2.log", "collect_20261019_0905_3.log"}
	for i := range want {
		if paths[i] != want[i] {
			t.Errorf("paths = %v, want %v", paths, want)
			break
		}
	}
}

func TestStatsAndSummary(t *testing.T) {
	var buf bytes.Buffer
	start := time.Date(2026, 10, 19, 9, 0, 0, 0, taipei)
	l := New(&buf, start)
	l.now = func() time.Time { return start.Add(95 * time.Second) }

	l.Fetched(7)
	l.Archived(false, "feed-a", []string{"tsmc"})
	l.Archived(true, "feed-a", []string{"tsmc", "chips"})
	l.Skip(SkipDuplicate, "u1")
	l.Skip(SkipLanguage, "u2")
	l.Skip(SkipNoMatch, "u3")
	l.Skip(SkipNoMatch, "u4")
	l.Fail("https://x.example/bad", errors.New("HTTP 404"))

	s := l.Finish(Completed)
	if s.New != 1 || s.Updated != 1 || s.Effective != 2 || s.Fetched != 7 {
		t.Errorf("stats = %+v", s)
	}
	if s.SkippedDuplicate != 1 || s.SkippedLanguage != 1 || s.SkippedNoMatch != 2 || s.Failed != 1 {
		t.Errorf("skips = %+v", s)
	}
	if s.PerTopic["tsmc"] != 2 || s.PerTopic["chips"] != 1 || s.PerSite["feed-a"] != 2 {
		t.Errorf("breakdown = %v %v", s.PerTopic, s.PerSite)
	}
	if s.StartedAt != "2026-10-19T09:00:00+08:00" || s.FinishedAt != "2026-10-19T09:01:35+08:00" || s.DurationSeconds != 95 {
		t.Errorf("times = %s %s %v", s.StartedAt, s.FinishedAt, s.DurationSeconds)
	}

	l.WriteSummary(s)
	out := buf.String()
	for _, w := range []string{"09:01:35 FAIL https://x.example/bad: HTTP 404", "範圍內完成 (completed)", "有效筆數", "  - tsmc: 2", "1m35s", "https://x.example/bad", "SKIP no_match u4"} {
		if !strings.Contains(out, w) {
			t.Errorf("log missing %q:\n%s", w, out)
		}
	}

	var back Stats
	if err := json.Unmarshal([]byte(s.JSON()), &back); err != nil {
		t.Fatal(err)
	}
	if back.Effective != 2 || back.EndReason != Completed || len(back.Failures) != 1 {
		t.Errorf("json round trip = %+v", back)
	}
}

func TestEchoAndEmptySummary(t *testing.T) {
	dir := t.TempDir()
	var echo bytes.Buffer
	l, err := Create(dir, time.Now().In(taipei), &echo)
	if err != nil {
		t.Fatal(err)
	}
	l.Printf("hello %d", 1)
	s := l.Finish(LimitReached)
	l.WriteSummary(s)
	l.Close()
	data, _ := os.ReadFile(l.Path())
	if !strings.Contains(string(data), "hello 1") || !strings.Contains(echo.String(), "hello 1") {
		t.Error("line not written to file and echo")
	}
	if !strings.Contains(string(data), "達到上限") || !strings.Contains(string(data), "(無)") {
		t.Errorf("summary = %s", data)
	}
	if s.Failures == nil {
		t.Error("failures should encode as [] not null")
	}
}
