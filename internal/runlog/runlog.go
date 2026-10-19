// Package runlog writes the per-run collection log and its summary (SPEC F8).
package runlog

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// End reasons.
const (
	Completed    = "completed"
	LimitReached = "limit_reached"
	Aborted      = "aborted"
)

// Failure is a page or feed that could not be fetched or parsed.
type Failure struct {
	URL   string `json:"url"`
	Error string `json:"error"`
}

// Stats are the counters of one run. Effective = New + Updated.
type Stats struct {
	StartedAt        string         `json:"started_at"`
	FinishedAt       string         `json:"finished_at"`
	DurationSeconds  float64        `json:"duration_seconds"`
	EndReason        string         `json:"end_reason"`
	Fetched          int            `json:"fetched"`
	New              int            `json:"new"`
	Updated          int            `json:"updated"`
	Effective        int            `json:"effective"`
	SkippedDuplicate int            `json:"skipped_duplicate"`
	SkippedLanguage  int            `json:"skipped_language"`
	SkippedNoMatch   int            `json:"skipped_no_match"`
	Failed           int            `json:"failed"`
	Failures         []Failure      `json:"failures"`
	ImagesOK         int            `json:"images_ok"`
	ImagesFailed     int            `json:"images_failed"`
	PerTopic         map[string]int `json:"per_topic"`
	PerSite          map[string]int `json:"per_site"`
}

// Log is the log of one run. Its methods are safe for concurrent use.
type Log struct {
	mu      sync.Mutex
	w       io.Writer
	closer  io.Closer
	path    string
	loc     *time.Location
	now     func() time.Time
	started time.Time
	stats   Stats
}

// Create opens logs/collect_YYYYMMDD_HHMM.log in dir. If that name is taken
// (two runs in the same minute) a _2, _3, ... suffix is added. Lines are
// also copied to echo when it is non-nil.
func Create(dir string, started time.Time, echo io.Writer) (*Log, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	base := "collect_" + started.Format("20060102_1504")
	for i := 1; ; i++ {
		name := base
		if i > 1 {
			name = fmt.Sprintf("%s_%d", base, i)
		}
		path := filepath.Join(dir, name+".log")
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var w io.Writer = f
		if echo != nil {
			w = io.MultiWriter(f, echo)
		}
		l := New(w, started)
		l.closer, l.path = f, path
		return l, nil
	}
}

// New returns a Log writing to w (no file).
func New(w io.Writer, started time.Time) *Log {
	return &Log{
		w:       w,
		loc:     started.Location(),
		now:     time.Now,
		started: started,
		stats:   Stats{PerTopic: map[string]int{}, PerSite: map[string]int{}},
	}
}

// Path returns the log file path, or "" for a Log created with New.
func (l *Log) Path() string { return l.path }

// Close closes the log file.
func (l *Log) Close() error {
	if l.closer == nil {
		return nil
	}
	return l.closer.Close()
}

// Printf writes a timestamped line.
func (l *Log) Printf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.printf(format, args...)
}

func (l *Log) printf(format string, args ...any) {
	fmt.Fprintf(l.w, "%s %s\n", l.now().In(l.loc).Format("15:04:05"), fmt.Sprintf(format, args...))
}

// Counter updates.

func (l *Log) Fetched(n int) { l.update(func(s *Stats) { s.Fetched = n }) }

// Skip reasons.
const (
	SkipDuplicate = "duplicate"
	SkipLanguage  = "language"
	SkipNoMatch   = "no_match"
)

// Skip counts a page dropped for reason (one of the Skip* constants).
func (l *Log) Skip(reason, url string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	switch reason {
	case SkipDuplicate:
		l.stats.SkippedDuplicate++
	case SkipLanguage:
		l.stats.SkippedLanguage++
	default:
		l.stats.SkippedNoMatch++
	}
	l.printf("SKIP %s %s", reason, url)
}

func (l *Log) Fail(url string, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.stats.Failed++
	l.stats.Failures = append(l.stats.Failures, Failure{URL: url, Error: err.Error()})
	l.printf("FAIL %s: %v", url, err)
}

// Archived records a new (updated=false) or updated article.
func (l *Log) Archived(updated bool, site string, topics []string) {
	l.update(func(s *Stats) {
		if updated {
			s.Updated++
		} else {
			s.New++
		}
		s.PerSite[site]++
		for _, t := range topics {
			s.PerTopic[t]++
		}
	})
}

func (l *Log) update(f func(*Stats)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	f(&l.stats)
}

// Finish fixes the end time and reason and returns the final stats.
func (l *Log) Finish(reason string) Stats {
	l.mu.Lock()
	defer l.mu.Unlock()
	end := l.now().In(l.loc)
	s := &l.stats
	s.StartedAt = l.started.Format(time.RFC3339)
	s.FinishedAt = end.Format(time.RFC3339)
	s.DurationSeconds = end.Sub(l.started).Round(time.Millisecond).Seconds()
	s.EndReason = reason
	s.Effective = s.New + s.Updated
	if s.Failures == nil {
		s.Failures = []Failure{}
	}
	return *s
}

// WriteSummary writes the summary table for s to the log.
func (l *Log) WriteSummary(s Stats) {
	l.mu.Lock()
	defer l.mu.Unlock()
	io.WriteString(l.w, Summary(s))
}

var reasonText = map[string]string{
	Completed:    "範圍內完成",
	LimitReached: "達到上限",
	Aborted:      "中斷",
}

// Summary formats the F8 summary.
func Summary(s Stats) string {
	var b strings.Builder
	row := func(k string, v any) { fmt.Fprintf(&b, "%s %v\n", pad(k, 18), v) }
	b.WriteString("\n===== 收集摘要 =====\n")
	row("開始時間", s.StartedAt)
	row("結束時間", s.FinishedAt)
	row("耗時", (time.Duration(s.DurationSeconds * float64(time.Second))).Round(time.Second))
	reason := reasonText[s.EndReason]
	if reason == "" {
		reason = s.EndReason
	}
	row("結束原因", fmt.Sprintf("%s (%s)", reason, s.EndReason))
	row("抓取頁數", s.Fetched)
	row("新增", s.New)
	row("更新", s.Updated)
	row("有效筆數", s.Effective)
	row("略過（重複）", s.SkippedDuplicate)
	row("略過（語言不符）", s.SkippedLanguage)
	row("略過（未命中）", s.SkippedNoMatch)
	row("失敗", s.Failed)
	for _, f := range s.Failures {
		fmt.Fprintf(&b, "  - %s: %s\n", f.URL, f.Error)
	}
	row("圖片下載", fmt.Sprintf("成功 %d / 失敗 %d", s.ImagesOK, s.ImagesFailed))
	writeMap(&b, "各 topic 有效筆數", s.PerTopic)
	writeMap(&b, "各網站有效筆數", s.PerSite)
	return b.String()
}

// pad right-pads s with spaces to the given display width, counting CJK
// and full-width characters as two columns.
func pad(s string, width int) string {
	w := 0
	for _, r := range s {
		w++
		if isWide(r) {
			w++
		}
	}
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

func isWide(r rune) bool {
	return r >= 0x1100 && (r <= 0x115f || (r >= 0x2e80 && r <= 0xa4cf) ||
		(r >= 0xac00 && r <= 0xd7a3) || (r >= 0xf900 && r <= 0xfaff) ||
		(r >= 0xfe30 && r <= 0xfe4f) || (r >= 0xff00 && r <= 0xff60) ||
		(r >= 0xffe0 && r <= 0xffe6))
}

func writeMap(b *strings.Builder, title string, m map[string]int) {
	fmt.Fprintf(b, "%s:", title)
	if len(m) == 0 {
		b.WriteString(" (無)\n")
		return
	}
	b.WriteString("\n")
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(b, "  - %s: %d\n", k, m[k])
	}
}

// JSON returns s encoded for the runs table.
func (s Stats) JSON() string {
	b, _ := json.Marshal(s)
	return string(b)
}
