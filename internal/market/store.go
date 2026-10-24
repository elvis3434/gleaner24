package market

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// migrations[i] upgrades the schema from user_version i to i+1.
var migrations = []string{
	`CREATE TABLE stock_daily (
  date         TEXT NOT NULL,
  symbol       TEXT NOT NULL,
  market       TEXT NOT NULL,
  volume       INTEGER,
  turnover     INTEGER,
  open REAL, high REAL, low REAL, close REAL,
  change       REAL,
  transactions INTEGER,
  note         TEXT,
  row_hash     TEXT NOT NULL,
  fetched_at   TEXT NOT NULL,
  PRIMARY KEY (date, symbol)
);
CREATE TABLE futures_daily (
  date           TEXT NOT NULL,
  product        TEXT NOT NULL,
  contract_month TEXT NOT NULL,
  session        TEXT NOT NULL,
  open REAL, high REAL, low REAL, close REAL, change REAL,
  volume         INTEGER,
  settlement     REAL,
  open_interest  INTEGER,
  best_bid REAL, best_ask REAL,
  row_hash       TEXT NOT NULL,
  fetched_at     TEXT NOT NULL,
  PRIMARY KEY (date, product, contract_month, session)
);
CREATE TABLE options_daily (
  date           TEXT NOT NULL,
  product        TEXT NOT NULL,
  contract_month TEXT NOT NULL,
  strike         REAL NOT NULL,
  call_put       TEXT NOT NULL,
  session        TEXT NOT NULL,
  expiry_date    TEXT,
  open REAL, high REAL, low REAL, close REAL, change REAL,
  volume         INTEGER,
  settlement     REAL,
  open_interest  INTEGER,
  best_bid REAL, best_ask REAL,
  row_hash       TEXT NOT NULL,
  fetched_at     TEXT NOT NULL,
  PRIMARY KEY (date, product, contract_month, strike, call_put, session)
);
CREATE TABLE runs (
  id          INTEGER PRIMARY KEY,
  started_at  TEXT NOT NULL,
  finished_at TEXT,
  end_reason  TEXT,
  log_file    TEXT,
  stats_json  TEXT
);`,
	// v2 (stage Q3): stock names for the K-line page. Kept out of
	// stock_daily so that adding them does not mark every row as updated.
	`CREATE TABLE stock_names (
  symbol     TEXT PRIMARY KEY,
  market     TEXT NOT NULL,
  name       TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`,
}

// Store is the quotes database. A Store opened read-only (for dry runs)
// answers Upsert by comparing only; it never writes.
type Store struct {
	db       *sql.DB
	readOnly bool
}

// OpenStore opens (creating if needed) the database and migrates it.
func OpenStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	s, err := openDB(path, "rwc")
	if err != nil {
		return nil, err
	}
	if err := s.migrate(); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

// OpenStoreReadOnly opens an existing database without changing it. A
// missing file gives an error matching os.ErrNotExist.
func OpenStoreReadOnly(path string) (*Store, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	s, err := openDB(path, "ro")
	if err != nil {
		return nil, err
	}
	s.readOnly = true
	return s, nil
}

func openDB(path, mode string) (*Store, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
	if filepath.VolumeName(abs) != "" {
		u.Path = "/" + u.Path // file:///C:/...
	}
	q := url.Values{}
	q.Set("mode", mode)
	q.Add("_pragma", "busy_timeout(5000)")
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

// Outcome of writing one row.
type Outcome int

const (
	New Outcome = iota
	Updated
	Unchanged
)

// Col is a column name and value. Values are string, int64, float64,
// *int64, *float64 or nil.
type Col struct {
	Name  string
	Value any
}

// Row is one record for Upsert: Key identifies it (the primary key) and
// Data holds every other column except row_hash and fetched_at.
type Row struct {
	Key  []Col
	Data []Col
}

// Counts tallies outcomes.
type Counts struct {
	New, Updated, Unchanged int
}

func (c *Counts) add(o Outcome) {
	switch o {
	case New:
		c.New++
	case Updated:
		c.Updated++
	default:
		c.Unchanged++
	}
}

func driverValue(v any) any {
	switch x := v.(type) {
	case *int64:
		if x == nil {
			return nil
		}
		return *x
	case *float64:
		if x == nil {
			return nil
		}
		return *x
	}
	return v
}

func rowHash(cols []Col) string {
	h := sha256.New()
	for _, c := range cols {
		v := driverValue(c.Value)
		if v == nil {
			fmt.Fprintf(h, "%s=\x00;", c.Name)
		} else {
			fmt.Fprintf(h, "%s=%v;", c.Name, v)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// UpsertAll writes rows to table in one transaction, inserting new keys
// and updating rows whose data changed. In a read-only store it only
// compares. fetchedAt is stored on every written row.
func (s *Store) UpsertAll(table string, rows []Row, fetchedAt string) (Counts, error) {
	var c Counts
	if s == nil {
		c.New = len(rows)
		return c, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return c, err
	}
	defer tx.Rollback()
	for _, r := range rows {
		o, err := upsert(tx, table, r, fetchedAt, s.readOnly)
		if err != nil {
			return Counts{}, err
		}
		c.add(o)
	}
	if s.readOnly {
		return c, nil
	}
	return c, tx.Commit()
}

func upsert(tx *sql.Tx, table string, r Row, fetchedAt string, compareOnly bool) (Outcome, error) {
	where := make([]string, len(r.Key))
	keyVals := make([]any, len(r.Key))
	for i, k := range r.Key {
		where[i] = k.Name + " = ?"
		keyVals[i] = driverValue(k.Value)
	}
	hash := rowHash(r.Data)
	var old string
	err := tx.QueryRow(`SELECT row_hash FROM `+table+` WHERE `+strings.Join(where, " AND "), keyVals...).Scan(&old)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if compareOnly {
			return New, nil
		}
		cols := append(append([]Col{}, r.Key...), r.Data...)
		cols = append(cols, Col{"row_hash", hash}, Col{"fetched_at", fetchedAt})
		names := make([]string, len(cols))
		marks := make([]string, len(cols))
		vals := make([]any, len(cols))
		for i, c := range cols {
			names[i], marks[i], vals[i] = c.Name, "?", driverValue(c.Value)
		}
		_, err := tx.Exec(`INSERT INTO `+table+` (`+strings.Join(names, ", ")+`) VALUES (`+strings.Join(marks, ", ")+`)`, vals...)
		return New, err
	case err != nil:
		return 0, err
	case old == hash:
		return Unchanged, nil
	}
	if compareOnly {
		return Updated, nil
	}
	cols := append(append([]Col{}, r.Data...), Col{"row_hash", hash}, Col{"fetched_at", fetchedAt})
	sets := make([]string, len(cols))
	vals := make([]any, 0, len(cols)+len(keyVals))
	for i, c := range cols {
		sets[i] = c.Name + " = ?"
		vals = append(vals, driverValue(c.Value))
	}
	vals = append(vals, keyVals...)
	_, err = tx.Exec(`UPDATE `+table+` SET `+strings.Join(sets, ", ")+` WHERE `+strings.Join(where, " AND "), vals...)
	return Updated, err
}

// StartRun records the start of a run (no-op for a read-only store).
func (s *Store) StartRun(startedAt, logFile string) (int64, error) {
	if s == nil || s.readOnly {
		return 0, nil
	}
	res, err := s.db.Exec(`INSERT INTO runs (started_at, log_file) VALUES (?, ?)`, startedAt, logFile)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// FinishRun stores the end of a run.
func (s *Store) FinishRun(id int64, finishedAt, endReason, statsJSON string) error {
	if s == nil || s.readOnly {
		return nil
	}
	_, err := s.db.Exec(`UPDATE runs SET finished_at = ?, end_reason = ?, stats_json = ? WHERE id = ?`,
		finishedAt, endReason, statsJSON, id)
	return err
}

// SetStockName records the display name of a stock (no-op when read-only
// or when name is empty).
func (s *Store) SetStockName(symbol, market, name, at string) error {
	if s == nil || s.readOnly || name == "" {
		return nil
	}
	_, err := s.db.Exec(`INSERT INTO stock_names (symbol, market, name, updated_at) VALUES (?, ?, ?, ?)
ON CONFLICT(symbol) DO UPDATE SET market = excluded.market, name = excluded.name, updated_at = excluded.updated_at`,
		symbol, market, name, at)
	return err
}

// Count returns the number of rows in table (for tests and summaries).
func (s *Store) Count(table string) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n)
	return n, err
}

// DB exposes the underlying handle for read queries.
func (s *Store) DB() *sql.DB { return s.db }
