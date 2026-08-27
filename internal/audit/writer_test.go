// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package audit

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/garudexlabs/linkedin-api/internal/observability"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// blockingStore holds every Insert until block is closed, so a test can fill the
// writer's buffer while the single consumer is stuck.
type blockingStore struct {
	block chan struct{}
}

func (b *blockingStore) Insert(context.Context, []Record) error {
	<-b.block
	return nil
}
func (b *blockingStore) Purge(context.Context, time.Time) (int64, error) { return 0, nil }
func (b *blockingStore) Close() error                                    { return nil }

type errStore struct{}

func (errStore) Insert(context.Context, []Record) error          { return errors.New("insert failed") }
func (errStore) Purge(context.Context, time.Time) (int64, error) { return 0, nil }
func (errStore) Close() error                                    { return nil }

func TestWriterPersistsConcurrentWrites(t *testing.T) {
	store := openTestStore(t)
	m := observability.NewMetrics()
	w := NewWriter(store, WriterConfig{
		BufferSize: 8192, BatchSize: 64, FlushInterval: 5 * time.Millisecond,
		Retention: time.Hour, PurgeInterval: time.Hour,
	}, m, testLogger())

	const goroutines, per = 8, 250
	now := time.Now()
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < per; i++ {
				w.Record(Record{Time: now, ProfileID: "p", ClientIP: "1.1.1.1", KeyID: "k", RateDecision: DecisionAllowed})
			}
		}()
	}
	wg.Wait()

	want := int64(goroutines * per)
	since := now.Add(-time.Hour)
	var total int64
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if sum, err := store.Summary(context.Background(), since); err == nil {
			if total = sum.Total; total >= want {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if total != want {
		t.Fatalf("persisted %d records, want %d", total, want)
	}
	if dropped := testutil.ToFloat64(m.AuditDropped); dropped != 0 {
		t.Errorf("dropped = %v, want 0", dropped)
	}
	_ = w.Close(context.Background())
}

func TestWriterDropsWhenBufferFull(t *testing.T) {
	m := observability.NewMetrics()
	store := &blockingStore{block: make(chan struct{})}
	w := NewWriter(store, WriterConfig{
		BufferSize: 4, BatchSize: 1, FlushInterval: time.Hour, PurgeInterval: time.Hour,
	}, m, testLogger())

	for i := 0; i < 200; i++ {
		w.Record(Record{ProfileID: "p", ClientIP: "1.1.1.1", KeyID: "k", RateDecision: DecisionAllowed})
	}
	if dropped := testutil.ToFloat64(m.AuditDropped); dropped == 0 {
		t.Errorf("expected records to be dropped when buffer is full, got 0")
	}

	close(store.block)
	_ = w.Close(context.Background())
}

func TestWriterCountsWriteErrorsWithoutBlocking(t *testing.T) {
	m := observability.NewMetrics()
	w := NewWriter(errStore{}, WriterConfig{
		BufferSize: 64, BatchSize: 4, FlushInterval: 5 * time.Millisecond, PurgeInterval: time.Hour,
	}, m, testLogger())

	for i := 0; i < 16; i++ {
		w.Record(Record{ProfileID: "p", ClientIP: "1.1.1.1", KeyID: "k", RateDecision: DecisionAllowed})
	}
	_ = w.Close(context.Background())

	if errs := testutil.ToFloat64(m.AuditWriteErrors); errs == 0 {
		t.Errorf("expected write errors to be counted, got 0")
	}
}

func TestWriterPurgesOnStart(t *testing.T) {
	store := openTestStore(t)
	old := time.Now().Add(-48 * time.Hour)
	mustInsert(t, store, []Record{
		{Time: old, ProfileID: "old", ClientIP: "1.1.1.1", KeyID: "k", RateDecision: DecisionAllowed},
	})

	m := observability.NewMetrics()
	w := NewWriter(store, WriterConfig{
		BufferSize: 8, BatchSize: 8, FlushInterval: time.Hour,
		Retention: 24 * time.Hour, PurgeInterval: time.Hour,
	}, m, testLogger())
	defer func() { _ = w.Close(context.Background()) }()

	since := time.Now().Add(-72 * time.Hour)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if sum, err := store.Summary(context.Background(), since); err == nil && sum.Total == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("retention did not purge expired records on start")
}
