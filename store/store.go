package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type Metric struct {
	Source string
	Name   string
	Metric string
	Value  float64
	Ts     int64
}

func New(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, err
	}

	dbPath := filepath.Join(dataDir, "vitalsvg.db")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS metrics (
			id     INTEGER PRIMARY KEY AUTOINCREMENT,
			source TEXT NOT NULL,
			name   TEXT NOT NULL,
			metric TEXT NOT NULL,
			value  REAL NOT NULL,
			ts     INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_metrics_lookup
			ON metrics (source, name, metric, ts);
	`); err != nil {
		db.Close()
		return nil, err
	}

	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Insert(m Metric) error {
	_, err := s.db.Exec(
		"INSERT INTO metrics (source, name, metric, value, ts) VALUES (?, ?, ?, ?, ?)",
		m.Source, m.Name, m.Metric, m.Value, m.Ts,
	)
	return err
}

func (s *Store) InsertBatch(metrics []Metric) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare("INSERT INTO metrics (source, name, metric, value, ts) VALUES (?, ?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, m := range metrics {
		if _, err := stmt.Exec(m.Source, m.Name, m.Metric, m.Value, m.Ts); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// QueryLatest returns the most recent metric matching the given source, name, and metric type.
func (s *Store) QueryLatest(source, name, metric string) (*Metric, error) {
	row := s.db.QueryRow(
		"SELECT source, name, metric, value, ts FROM metrics WHERE source = ? AND name = ? AND metric = ? ORDER BY ts DESC LIMIT 1",
		source, name, metric,
	)

	var m Metric
	err := row.Scan(&m.Source, &m.Name, &m.Metric, &m.Value, &m.Ts)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// QuerySeries returns all metrics for the last 24 hours, ordered by time.
func (s *Store) QuerySeries(source, name, metric string) ([]Metric, error) {
	cutoff := time.Now().Unix() - 86400

	rows, err := s.db.Query(
		"SELECT source, name, metric, value, ts FROM metrics WHERE source = ? AND name = ? AND metric = ? AND ts > ? ORDER BY ts ASC",
		source, name, metric, cutoff,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []Metric
	for rows.Next() {
		var m Metric
		if err := rows.Scan(&m.Source, &m.Name, &m.Metric, &m.Value, &m.Ts); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

// Resource represents a unique source+name combination.
type Resource struct {
	Source string
	Name   string
}

// ListResources returns all unique source+name pairs that have recent data.
func (s *Store) ListResources() ([]Resource, error) {
	cutoff := time.Now().Unix() - 86400
	rows, err := s.db.Query(
		"SELECT DISTINCT source, name FROM metrics WHERE ts > ? ORDER BY source, name",
		cutoff,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []Resource
	for rows.Next() {
		var r Resource
		if err := rows.Scan(&r.Source, &r.Name); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// Prune deletes metrics older than 24 hours.
func (s *Store) Prune() error {
	cutoff := time.Now().Unix() - 86400
	_, err := s.db.Exec("DELETE FROM metrics WHERE ts < ?", cutoff)
	return err
}
