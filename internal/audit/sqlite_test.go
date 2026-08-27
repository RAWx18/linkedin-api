// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package audit

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func openTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func mustInsert(t *testing.T, s *SQLiteStore, recs []Record) {
	t.Helper()
	if err := s.Insert(context.Background(), recs); err != nil {
		t.Fatalf("insert: %v", err)
	}
}

func TestStoreSummaryAndTopQueries(t *testing.T) {
	store := openTestStore(t)
	now := time.Now()
	recs := []Record{
		{Time: now, ProfileID: "alice", ClientIP: "1.1.1.1", KeyID: "key_a", Status: 200, RateDecision: DecisionAllowed, Cached: true, Latency: 5 * time.Millisecond},
		{Time: now, ProfileID: "alice", ClientIP: "1.1.1.1", KeyID: "key_a", Status: 200, RateDecision: DecisionAllowed, UpstreamCalled: true, UpstreamOutcome: OutcomeOK, Latency: 50 * time.Millisecond},
		{Time: now, ProfileID: "bob", ClientIP: "2.2.2.2", KeyID: "key_b", Status: 404, RateDecision: DecisionAllowed, UpstreamCalled: true, UpstreamOutcome: "profile_not_found", ErrorClass: "profile_not_found", Latency: 40 * time.Millisecond},
		{Time: now, ProfileID: "alice", ClientIP: "3.3.3.3", KeyID: AnonymousKey, Status: 429, RateDecision: DecisionIPLimited, ErrorClass: "rate_limited", Latency: 1 * time.Millisecond},
		{Time: now, ProfileID: "carol", ClientIP: "2.2.2.2", KeyID: "key_b", Status: 401, RateDecision: DecisionAllowed, ErrorClass: "unauthorized", Latency: 2 * time.Millisecond},
		{Time: now, ProfileID: "dave", ClientIP: "2.2.2.2", KeyID: "key_b", Status: 429, RateDecision: DecisionAllowed, UpstreamCalled: true, UpstreamOutcome: "upstream_rate_limited", Retries: 1, ErrorClass: "upstream_rate_limited", Latency: 60 * time.Millisecond},
	}
	mustInsert(t, store, recs)

	since := now.Add(-time.Hour)
	sum, err := store.Summary(context.Background(), since)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	checks := []struct {
		name string
		got  int64
		want int64
	}{
		{"total", sum.Total, 6},
		{"cache_hits", sum.CacheHits, 1},
		{"cache_misses", sum.CacheMisses, 3},
		{"upstream_hits", sum.UpstreamHits, 3},
		{"rate_limited", sum.RateLimited, 1},
		{"auth_failures", sum.AuthFailures, 1},
		{"not_found", sum.NotFound, 1},
		{"server_errors", sum.ServerErrors, 0},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("summary %s = %d, want %d", c.name, c.got, c.want)
		}
	}
	if sum.Outcomes["ok"] != 1 || sum.Outcomes["profile_not_found"] != 1 || sum.Outcomes["upstream_rate_limited"] != 1 {
		t.Errorf("outcomes = %v", sum.Outcomes)
	}
	if sum.AvgLatencyMS <= 0 {
		t.Errorf("avg latency = %v, want > 0", sum.AvgLatencyMS)
	}

	profiles, err := store.TopProfiles(context.Background(), since, 10)
	if err != nil {
		t.Fatalf("top profiles: %v", err)
	}
	if len(profiles) == 0 || profiles[0].Key != "alice" || profiles[0].Count != 3 {
		t.Errorf("top profiles = %v, want alice=3 first", profiles)
	}

	clients, err := store.TopClients(context.Background(), since, 10)
	if err != nil {
		t.Fatalf("top clients: %v", err)
	}
	if len(clients) == 0 || clients[0].Key != "2.2.2.2" || clients[0].Count != 3 {
		t.Errorf("top clients = %v, want 2.2.2.2=3 first", clients)
	}

	upstream, err := store.TopUpstreamClients(context.Background(), since, 10)
	if err != nil {
		t.Fatalf("top upstream clients: %v", err)
	}
	if len(upstream) == 0 || upstream[0].Key != "2.2.2.2" || upstream[0].Count != 2 {
		t.Errorf("top upstream clients = %v, want 2.2.2.2=2 first", upstream)
	}
}

func TestStorePurgeRetention(t *testing.T) {
	store := openTestStore(t)
	now := time.Now()
	old := now.Add(-48 * time.Hour)
	mustInsert(t, store, []Record{
		{Time: old, ProfileID: "old1", ClientIP: "1.1.1.1", KeyID: "k", RateDecision: DecisionAllowed},
		{Time: old, ProfileID: "old2", ClientIP: "1.1.1.1", KeyID: "k", RateDecision: DecisionAllowed},
		{Time: now, ProfileID: "new1", ClientIP: "1.1.1.1", KeyID: "k", RateDecision: DecisionAllowed},
	})

	deleted, err := store.Purge(context.Background(), now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if deleted != 2 {
		t.Errorf("deleted = %d, want 2", deleted)
	}
	sum, err := store.Summary(context.Background(), now.Add(-72*time.Hour))
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if sum.Total != 1 {
		t.Errorf("remaining total = %d, want 1", sum.Total)
	}
}

func TestOpenMigratesPreCredentialSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	oldSchema := `
CREATE TABLE audit_events (
	id               INTEGER PRIMARY KEY AUTOINCREMENT,
	ts               INTEGER NOT NULL,
	request_id       TEXT    NOT NULL,
	client_ip        TEXT    NOT NULL,
	key_id           TEXT    NOT NULL,
	profile_id       TEXT    NOT NULL,
	status           INTEGER NOT NULL,
	rate_decision    TEXT    NOT NULL,
	cached           INTEGER NOT NULL,
	upstream_called  INTEGER NOT NULL,
	upstream_outcome TEXT    NOT NULL,
	retries          INTEGER NOT NULL,
	latency_ms       INTEGER NOT NULL,
	error_class      TEXT    NOT NULL
);
INSERT INTO audit_events
	(ts, request_id, client_ip, key_id, profile_id, status, rate_decision,
	 cached, upstream_called, upstream_outcome, retries, latency_ms, error_class)
VALUES (1, 'r1', '1.1.1.1', 'k', 'alice', 200, 'allowed', 0, 1, 'ok', 0, 5, '');`
	if _, err := db.Exec(oldSchema); err != nil {
		t.Fatalf("create old schema: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close raw db: %v", err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatalf("open store over old schema: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	now := time.Now()
	mustInsert(t, store, []Record{{
		Time: now, ProfileID: "bob", ClientIP: "2.2.2.2", KeyID: "k",
		CredentialMode: "caller_session", CredFP: "cs_abc123",
		Status: 200, RateDecision: DecisionAllowed, Latency: 5 * time.Millisecond,
	}})

	var mode, fp string
	row := store.db.QueryRow("SELECT credential_mode, cred_fp FROM audit_events WHERE profile_id = 'bob'")
	if err := row.Scan(&mode, &fp); err != nil {
		t.Fatalf("read migrated row: %v", err)
	}
	if mode != "caller_session" || fp != "cs_abc123" {
		t.Errorf("credential_mode = %q, cred_fp = %q", mode, fp)
	}

	sum, err := store.Summary(context.Background(), now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("summary after migration: %v", err)
	}
	if sum.Total != 1 {
		t.Errorf("windowed total = %d, want 1", sum.Total)
	}
}

func TestStoreWindowExcludesOlder(t *testing.T) {
	store := openTestStore(t)
	now := time.Now()
	mustInsert(t, store, []Record{
		{Time: now.Add(-2 * time.Hour), ProfileID: "stale", ClientIP: "1.1.1.1", KeyID: "k", RateDecision: DecisionAllowed},
		{Time: now, ProfileID: "fresh", ClientIP: "1.1.1.1", KeyID: "k", RateDecision: DecisionAllowed},
	})
	sum, err := store.Summary(context.Background(), now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if sum.Total != 1 {
		t.Errorf("windowed total = %d, want 1", sum.Total)
	}
}
