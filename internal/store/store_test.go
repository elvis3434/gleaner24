package store

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func openTemp(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sub", "index.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}

func TestPagesUpsert(t *testing.T) {
	s, _ := openTemp(t)
	if p, err := s.Page("https://a.example/x"); err != nil || p != nil {
		t.Fatalf("missing page = %v, %v", p, err)
	}
	p := &Page{
		URL: "https://a.example/x", Site: "a", Title: "T", ContentHash: "h1",
		Status: StatusArchived, FirstSeenAt: "2026-10-19T09:00:00+08:00",
		LastFetchAt: "2026-10-19T09:00:00+08:00", LastChangeAt: "2026-10-19T09:00:00+08:00",
	}
	if err := s.SavePage(p); err != nil {
		t.Fatal(err)
	}
	id := p.ID
	if id == 0 {
		t.Fatal("ID not set")
	}

	p2 := *p
	p2.ID = 0
	p2.ContentHash = "h2"
	p2.FirstSeenAt = "2030-01-01T00:00:00+08:00" // must be ignored on update
	p2.LastFetchAt = "2026-10-20T09:00:00+08:00"
	if err := s.SavePage(&p2); err != nil {
		t.Fatal(err)
	}
	if p2.ID != id {
		t.Errorf("upsert changed ID %d -> %d", id, p2.ID)
	}
	got, err := s.Page("https://a.example/x")
	if err != nil {
		t.Fatal(err)
	}
	if got.ContentHash != "h2" || got.FirstSeenAt != "2026-10-19T09:00:00+08:00" || got.LastFetchAt != "2026-10-20T09:00:00+08:00" || got.Title != "T" {
		t.Errorf("page = %+v", got)
	}
}

func TestArchivesAndRuns(t *testing.T) {
	s, _ := openTemp(t)
	p := &Page{URL: "u", Site: "s", Status: StatusArchived, FirstSeenAt: "t", LastFetchAt: "t"}
	if err := s.SavePage(p); err != nil {
		t.Fatal(err)
	}
	runID, err := s.StartRun("2026-10-19T09:00:00+08:00", "logs/x.log")
	if err != nil {
		t.Fatal(err)
	}
	if a, err := s.LatestArchive(p.ID); err != nil || a != nil {
		t.Fatalf("no archive yet: %v %v", a, err)
	}
	for v := 1; v <= 2; v++ {
		a := &Archive{PageID: p.ID, Version: v, FilePath: "2026/10/19/t/x.md", Topics: []string{"t"}, Keywords: []string{"k", "台積電"}, ArchivedAt: "t", RunID: runID}
		if err := s.AddArchive(a); err != nil {
			t.Fatal(err)
		}
	}
	a, err := s.LatestArchive(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if a.Version != 2 || !reflect.DeepEqual(a.Keywords, []string{"k", "台積電"}) || !reflect.DeepEqual(a.Topics, []string{"t"}) {
		t.Errorf("latest = %+v", a)
	}
	// Foreign keys are enforced.
	if err := s.AddArchive(&Archive{PageID: 999, Version: 1, FilePath: "x", ArchivedAt: "t", RunID: runID}); err == nil {
		t.Error("archive for unknown page accepted")
	}

	if err := s.FinishRun(runID, "2026-10-19T09:05:00+08:00", "completed", `{"new":1}`); err != nil {
		t.Fatal(err)
	}
	runs, err := s.RecentRuns(5)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].EndReason != "completed" || runs[0].StatsJSON != `{"new":1}` || runs[0].LogFile != "logs/x.log" {
		t.Errorf("runs = %+v", runs)
	}
}

func TestReopenAndReadOnly(t *testing.T) {
	s, path := openTemp(t)
	p := &Page{URL: "u", Site: "s", Status: StatusSkipped, FirstSeenAt: "t", LastFetchAt: "t"}
	if err := s.SavePage(p); err != nil {
		t.Fatal(err)
	}
	s.Close()

	// Reopening must not re-run migrations.
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s2.Close()

	ro, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	if got, err := ro.Page("u"); err != nil || got == nil || got.Status != StatusSkipped {
		t.Errorf("read-only page = %+v, %v", got, err)
	}
	if err := ro.SavePage(&Page{URL: "v", Site: "s", Status: "x", FirstSeenAt: "t", LastFetchAt: "t"}); err == nil {
		t.Error("write succeeded on read-only store")
	}

	if _, err := OpenReadOnly(filepath.Join(t.TempDir(), "none.db")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing db err = %v", err)
	}
}

func TestOpenUnusualPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "王 小明", "資料 #1 %20", "index.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("database not at the requested path: %v", err)
	}
}
