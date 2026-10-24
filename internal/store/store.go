// Package store keeps the page index, archive records and run history in a
// single SQLite file, using the pure-Go modernc.org/sqlite driver.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Page status values.
const (
	StatusArchived = "archived"
	StatusSkipped  = "skipped"
	StatusFailed   = "failed"
)

// migrations[i] upgrades the schema from user_version i to i+1.
var migrations = []string{
	`CREATE TABLE pages (
  id             INTEGER PRIMARY KEY,
  url            TEXT NOT NULL UNIQUE,
  site           TEXT NOT NULL,
  title          TEXT,
  language       TEXT,
  published_at   TEXT,
  etag           TEXT,
  last_modified  TEXT,
  content_hash   TEXT,
  status         TEXT NOT NULL,
  first_seen_at  TEXT NOT NULL,
  last_fetch_at  TEXT NOT NULL,
  last_change_at TEXT
);
CREATE TABLE runs (
  id          INTEGER PRIMARY KEY,
  started_at  TEXT NOT NULL,
  finished_at TEXT,
  end_reason  TEXT,
  log_file    TEXT,
  stats_json  TEXT
);
CREATE TABLE archives (
  id          INTEGER PRIMARY KEY,
  page_id     INTEGER NOT NULL REFERENCES pages(id),
  version     INTEGER NOT NULL,
  file_path   TEXT NOT NULL,
  topics      TEXT NOT NULL,
  keywords    TEXT NOT NULL,
  archived_at TEXT NOT NULL,
  run_id      INTEGER NOT NULL REFERENCES runs(id)
);
CREATE INDEX archives_page ON archives(page_id, version);
CREATE TABLE images (
  hash      TEXT PRIMARY KEY,
  src_url   TEXT NOT NULL,
  file_path TEXT NOT NULL,
  width     INTEGER,
  height    INTEGER
);
CREATE TABLE archive_images (
  archive_id INTEGER NOT NULL REFERENCES archives(id),
  image_hash TEXT NOT NULL REFERENCES images(hash),
  PRIMARY KEY (archive_id, image_hash)
);`,
}

// Store is an open index database.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the database at path and brings its
// schema up to date.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	s, err := open(path, "rwc")
	if err != nil {
		return nil, err
	}
	if err := s.migrate(); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

// OpenReadOnly opens an existing database without modifying it. It returns
// os.ErrNotExist (wrapped) if the file does not exist.
func OpenReadOnly(path string) (*Store, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	return open(path, "ro")
}

func open(path, mode string) (*Store, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	// Build a file: URI; ToSlash keeps Windows drive paths (C:\...) valid.
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
	if filepath.VolumeName(abs) != "" {
		u.Path = "/" + u.Path
	}
	q := url.Values{}
	q.Set("mode", mode)
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "foreign_keys(1)")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("open database %s: %w", path, err)
	}
	return &Store{db: db}, nil
}

func (s *Store) migrate() error {
	var v int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		return err
	}
	for ; v < len(migrations); v++ {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[v]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migrate schema to v%d: %w", v+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, v+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Page is a row of the pages table. Time fields are RFC 3339 strings.
type Page struct {
	ID           int64
	URL          string
	Site         string
	Title        string
	Language     string
	PublishedAt  string
	ETag         string
	LastModified string
	ContentHash  string
	Status       string
	FirstSeenAt  string
	LastFetchAt  string
	LastChangeAt string
}

// Page returns the page with the given normalised URL, or nil if unknown.
func (s *Store) Page(rawURL string) (*Page, error) {
	var p Page
	var title, lang, pub, etag, lm, hash, change sql.NullString
	err := s.db.QueryRow(`SELECT id, url, site, title, language, published_at, etag, last_modified,
	content_hash, status, first_seen_at, last_fetch_at, last_change_at FROM pages WHERE url = ?`, rawURL).
		Scan(&p.ID, &p.URL, &p.Site, &title, &lang, &pub, &etag, &lm, &hash, &p.Status, &p.FirstSeenAt, &p.LastFetchAt, &change)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.Title, p.Language, p.PublishedAt = title.String, lang.String, pub.String
	p.ETag, p.LastModified, p.ContentHash, p.LastChangeAt = etag.String, lm.String, hash.String, change.String
	return &p, nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// SavePage inserts p or updates the row with the same URL, and sets p.ID.
// FirstSeenAt is kept from the existing row.
func (s *Store) SavePage(p *Page) error {
	err := s.db.QueryRow(`INSERT INTO pages (url, site, title, language, published_at, etag, last_modified,
	content_hash, status, first_seen_at, last_fetch_at, last_change_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(url) DO UPDATE SET
	site = excluded.site, title = excluded.title, language = excluded.language,
	published_at = excluded.published_at, etag = excluded.etag, last_modified = excluded.last_modified,
	content_hash = excluded.content_hash, status = excluded.status,
	last_fetch_at = excluded.last_fetch_at, last_change_at = excluded.last_change_at
RETURNING id`,
		p.URL, p.Site, nullable(p.Title), nullable(p.Language), nullable(p.PublishedAt), nullable(p.ETag),
		nullable(p.LastModified), nullable(p.ContentHash), p.Status, p.FirstSeenAt, p.LastFetchAt,
		nullable(p.LastChangeAt)).Scan(&p.ID)
	return err
}

// Archive is a row of the archives table. FilePath uses forward slashes and
// is relative to the archive directory.
type Archive struct {
	ID         int64
	PageID     int64
	Version    int
	FilePath   string
	Topics     []string
	Keywords   []string
	ArchivedAt string
	RunID      int64
}

// LatestArchive returns the highest-version archive of a page, or nil.
func (s *Store) LatestArchive(pageID int64) (*Archive, error) {
	var a Archive
	var topics, kws string
	err := s.db.QueryRow(`SELECT id, page_id, version, file_path, topics, keywords, archived_at, run_id
FROM archives WHERE page_id = ? ORDER BY version DESC LIMIT 1`, pageID).
		Scan(&a.ID, &a.PageID, &a.Version, &a.FilePath, &topics, &kws, &a.ArchivedAt, &a.RunID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(topics), &a.Topics); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(kws), &a.Keywords); err != nil {
		return nil, err
	}
	return &a, nil
}

// AddArchive inserts a and sets a.ID.
func (s *Store) AddArchive(a *Archive) error {
	topics, err := json.Marshal(nonNil(a.Topics))
	if err != nil {
		return err
	}
	kws, err := json.Marshal(nonNil(a.Keywords))
	if err != nil {
		return err
	}
	res, err := s.db.Exec(`INSERT INTO archives (page_id, version, file_path, topics, keywords, archived_at, run_id)
VALUES (?, ?, ?, ?, ?, ?, ?)`, a.PageID, a.Version, a.FilePath, string(topics), string(kws), a.ArchivedAt, a.RunID)
	if err != nil {
		return err
	}
	a.ID, err = res.LastInsertId()
	return err
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// StartRun records the start of a run and returns its ID.
func (s *Store) StartRun(startedAt, logFile string) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO runs (started_at, log_file) VALUES (?, ?)`, startedAt, nullable(logFile))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// FinishRun stores the end of a run and its summary.
func (s *Store) FinishRun(id int64, finishedAt, endReason, statsJSON string) error {
	_, err := s.db.Exec(`UPDATE runs SET finished_at = ?, end_reason = ?, stats_json = ? WHERE id = ?`,
		finishedAt, endReason, statsJSON, id)
	return err
}

// Run is a row of the runs table.
type Run struct {
	ID         int64
	StartedAt  string
	FinishedAt string
	EndReason  string
	LogFile    string
	StatsJSON  string
}

// RecentRuns returns up to n runs, newest first.
func (s *Store) RecentRuns(n int) ([]Run, error) {
	rows, err := s.db.Query(`SELECT id, started_at, finished_at, end_reason, log_file, stats_json
FROM runs ORDER BY id DESC LIMIT ?`, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var runs []Run
	for rows.Next() {
		var r Run
		var fin, reason, logf, stats sql.NullString
		if err := rows.Scan(&r.ID, &r.StartedAt, &fin, &reason, &logf, &stats); err != nil {
			return nil, err
		}
		r.FinishedAt, r.EndReason, r.LogFile, r.StatsJSON = fin.String, reason.String, logf.String, stats.String
		runs = append(runs, r)
	}
	return runs, rows.Err()
}
