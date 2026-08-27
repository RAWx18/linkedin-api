// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package audit

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// schema defines the audit table and the indexes that keep the usage queries
// bounded: a time index for windowed scans and retention, plus composite indexes
// for the per-profile, per-client, and per-key lookups the queries perform.
const schema = `
CREATE TABLE IF NOT EXISTS audit_events (
	id               INTEGER PRIMARY KEY AUTOINCREMENT,
	ts               INTEGER NOT NULL,
	request_id       TEXT    NOT NULL,
	client_ip        TEXT    NOT NULL,
	key_id           TEXT    NOT NULL,
	profile_id       TEXT    NOT NULL,
	credential_mode  TEXT    NOT NULL,
	cred_fp          TEXT    NOT NULL,
	status           INTEGER NOT NULL,
	rate_decision    TEXT    NOT NULL,
	cached           INTEGER NOT NULL,
	upstream_called  INTEGER NOT NULL,
	upstream_outcome TEXT    NOT NULL,
	retries          INTEGER NOT NULL,
	latency_ms       INTEGER NOT NULL,
	error_class      TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_ts ON audit_events(ts);
CREATE INDEX IF NOT EXISTS idx_audit_profile ON audit_events(profile_id, ts);
CREATE INDEX IF NOT EXISTS idx_audit_client ON audit_events(client_ip, ts);
CREATE INDEX IF NOT EXISTS idx_audit_key ON audit_events(key_id, ts);
`

// addedColumns lists columns introduced after the table first shipped.
// CREATE TABLE IF NOT EXISTS never alters an existing table, so a database
// created before these columns existed gains them on open.
var addedColumns = map[string]string{
	"credential_mode": "ALTER TABLE audit_events ADD COLUMN credential_mode TEXT NOT NULL DEFAULT ''",
	"cred_fp":         "ALTER TABLE audit_events ADD COLUMN cred_fp TEXT NOT NULL DEFAULT ''",
}

const insertSQL = `
INSERT INTO audit_events
	(ts, request_id, client_ip, key_id, profile_id, credential_mode, cred_fp, status, rate_decision,
	 cached, upstream_called, upstream_outcome, retries, latency_ms, error_class)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

// Count is a single grouped tally, such as one profile or client and how many
// requests it accounts for in the window.
type Count struct {
	Key   string `json:"key"`
	Count int64  `json:"count"`
}

// Summary aggregates the headline usage numbers for a time window.
type Summary struct {
	Since        time.Time        `json:"since"`
	Total        int64            `json:"total_requests"`
	CacheHits    int64            `json:"cache_hits"`
	CacheMisses  int64            `json:"cache_misses"`
	UpstreamHits int64            `json:"upstream_requests"`
	RateLimited  int64            `json:"rate_limited"`
	AuthFailures int64            `json:"auth_failures"`
	NotFound     int64            `json:"not_found"`
	ServerErrors int64            `json:"server_errors"`
	AvgLatencyMS float64          `json:"avg_latency_ms"`
	P95LatencyMS int64            `json:"p95_latency_ms"`
	Outcomes     map[string]int64 `json:"upstream_outcomes"`
}

// SQLiteStore is a pure-Go SQLite backend. A single writer goroutine performs all
// inserts, so the store stays contention-free; the admin queries are the only
// concurrent readers and rely on a busy timeout to serialize safely.
type SQLiteStore struct {
	db *sql.DB
}

// Open opens or creates the audit database at path and applies the schema.
//
// SQLite cannot take the POSIX byte-range locks it normally relies on when the
// file lives on an SMB/CIFS network mount such as Azure Files, which fails every
// write with SQLITE_BUSY. `nolock=1` disables that locking; it is safe here
// because each replica owns its own database file (the path carries the replica
// name) and a single goroutine performs every write. A single open connection
// serializes that writer with the occasional admin reader, and an in-memory
// rollback journal avoids creating lock/journal sidecar files on the share.
func Open(path string) (*SQLiteStore, error) {
	dsn := "file:" + path + "?nolock=1&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)&_pragma=journal_mode(MEMORY)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open audit db: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply audit schema: %w", err)
	}
	if err := reconcileColumns(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate audit schema: %w", err)
	}
	return &SQLiteStore{db: db}, nil
}

// reconcileColumns adds any columns missing from an audit table created by an
// earlier version of the schema.
func reconcileColumns(db *sql.DB) error {
	rows, err := db.Query("SELECT name FROM pragma_table_info('audit_events')")
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	present := make(map[string]bool, len(addedColumns))
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		present[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for name, alter := range addedColumns {
		if present[name] {
			continue
		}
		if _, err := db.Exec(alter); err != nil {
			return fmt.Errorf("add column %s: %w", name, err)
		}
	}
	return nil
}

// Insert writes a batch of records in a single transaction.
func (s *SQLiteStore) Insert(ctx context.Context, batch []Record) error {
	if len(batch) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, insertSQL)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer func() { _ = stmt.Close() }()

	for i := range batch {
		r := &batch[i]
		if _, err := stmt.ExecContext(ctx,
			r.Time.UnixMilli(), r.RequestID, r.ClientIP, r.KeyID, r.ProfileID,
			r.CredentialMode, r.CredFP,
			r.Status, r.RateDecision, boolToInt(r.Cached), boolToInt(r.UpstreamCalled),
			r.UpstreamOutcome, r.Retries, r.Latency.Milliseconds(), r.ErrorClass,
		); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// Purge deletes every record older than before and reports how many were removed.
func (s *SQLiteStore) Purge(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, "DELETE FROM audit_events WHERE ts < ?", before.UnixMilli())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Close closes the underlying database.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// Summary computes the windowed aggregate counts since the given instant.
func (s *SQLiteStore) Summary(ctx context.Context, since time.Time) (Summary, error) {
	sum := Summary{Since: since, Outcomes: map[string]int64{}}
	var avg sql.NullFloat64
	row := s.db.QueryRowContext(ctx, `
SELECT
	COUNT(*),
	COALESCE(SUM(cached), 0),
	COALESCE(SUM(CASE WHEN cached = 0 AND upstream_called = 1 THEN 1 ELSE 0 END), 0),
	COALESCE(SUM(upstream_called), 0),
	COALESCE(SUM(CASE WHEN rate_decision != 'allowed' THEN 1 ELSE 0 END), 0),
	COALESCE(SUM(CASE WHEN error_class = 'unauthorized' THEN 1 ELSE 0 END), 0),
	COALESCE(SUM(CASE WHEN error_class = 'profile_not_found' THEN 1 ELSE 0 END), 0),
	COALESCE(SUM(CASE WHEN status >= 500 THEN 1 ELSE 0 END), 0),
	AVG(latency_ms)
FROM audit_events WHERE ts >= ?`, since.UnixMilli())
	if err := row.Scan(&sum.Total, &sum.CacheHits, &sum.CacheMisses, &sum.UpstreamHits,
		&sum.RateLimited, &sum.AuthFailures, &sum.NotFound, &sum.ServerErrors, &avg); err != nil {
		return Summary{}, err
	}
	sum.AvgLatencyMS = avg.Float64

	p95, err := s.percentile(ctx, since, 95)
	if err != nil {
		return Summary{}, err
	}
	sum.P95LatencyMS = p95

	if err := s.outcomes(ctx, since, sum.Outcomes); err != nil {
		return Summary{}, err
	}
	return sum, nil
}

// percentile returns the latency at the given percentile within the window.
func (s *SQLiteStore) percentile(ctx context.Context, since time.Time, p int) (int64, error) {
	var v int64
	err := s.db.QueryRowContext(ctx, `
SELECT latency_ms FROM audit_events WHERE ts >= ? ORDER BY latency_ms
LIMIT 1 OFFSET (SELECT CAST(COUNT(*) * ? / 100 AS INTEGER) FROM audit_events WHERE ts >= ?)`,
		since.UnixMilli(), p, since.UnixMilli()).Scan(&v)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return v, err
}

// outcomes fills the map with a per-outcome tally of upstream calls.
func (s *SQLiteStore) outcomes(ctx context.Context, since time.Time, into map[string]int64) error {
	rows, err := s.db.QueryContext(ctx, `
SELECT upstream_outcome, COUNT(*) FROM audit_events
WHERE ts >= ? AND upstream_outcome != '' GROUP BY upstream_outcome`, since.UnixMilli())
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var name string
		var n int64
		if err := rows.Scan(&name, &n); err != nil {
			return err
		}
		into[name] = n
	}
	return rows.Err()
}

// TopProfiles returns the most frequently requested profiles in the window.
func (s *SQLiteStore) TopProfiles(ctx context.Context, since time.Time, limit int) ([]Count, error) {
	return s.topBy(ctx, `
SELECT profile_id, COUNT(*) c FROM audit_events
WHERE ts >= ? AND profile_id != '' GROUP BY profile_id ORDER BY c DESC LIMIT ?`, since, limit)
}

// TopClients returns the client IPs generating the most requests in the window.
func (s *SQLiteStore) TopClients(ctx context.Context, since time.Time, limit int) ([]Count, error) {
	return s.topBy(ctx, `
SELECT client_ip, COUNT(*) c FROM audit_events
WHERE ts >= ? GROUP BY client_ip ORDER BY c DESC LIMIT ?`, since, limit)
}

// TopUpstreamClients returns the client IPs driving the most LinkedIn requests,
// the signal for a caller attempting to generate excessive upstream traffic.
func (s *SQLiteStore) TopUpstreamClients(ctx context.Context, since time.Time, limit int) ([]Count, error) {
	return s.topBy(ctx, `
SELECT client_ip, COUNT(*) c FROM audit_events
WHERE ts >= ? AND upstream_called = 1 GROUP BY client_ip ORDER BY c DESC LIMIT ?`, since, limit)
}

func (s *SQLiteStore) topBy(ctx context.Context, query string, since time.Time, limit int) ([]Count, error) {
	rows, err := s.db.QueryContext(ctx, query, since.UnixMilli(), limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Count
	for rows.Next() {
		var c Count
		if err := rows.Scan(&c.Key, &c.Count); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
