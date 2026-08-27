// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package audit

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/garudexlabs/linkedin-api/internal/observability"
)

// Recorder accepts finished request records. Implementations must not block the
// caller: recording is best-effort so it can never slow or fail a profile lookup.
type Recorder interface {
	Record(rec Record)
}

// Store is the durable backend the writer flushes batches to and prunes.
type Store interface {
	Insert(ctx context.Context, batch []Record) error
	Purge(ctx context.Context, before time.Time) (int64, error)
	Close() error
}

// WriterConfig tunes the asynchronous writer.
type WriterConfig struct {
	BufferSize    int
	BatchSize     int
	FlushInterval time.Duration
	Retention     time.Duration
	PurgeInterval time.Duration
}

const (
	flushTimeout = 5 * time.Second
	purgeTimeout = 30 * time.Second
)

// Writer buffers records and flushes them to the store in batches from a single
// background goroutine. One writer keeps the store contention-free, and a bounded
// buffer that drops on overflow ensures a traffic burst can never exhaust memory
// or turn auditing into a denial-of-service vector against the API.
type Writer struct {
	store     Store
	ch        chan Record
	cfg       WriterConfig
	metrics   *observability.Metrics
	logger    *slog.Logger
	now       func() time.Time
	wg        sync.WaitGroup
	closeOnce sync.Once
	done      chan struct{}
}

// NewWriter starts the background writer. It takes ownership of store and closes
// it on Close.
func NewWriter(store Store, cfg WriterConfig, metrics *observability.Metrics, logger *slog.Logger) *Writer {
	if cfg.BufferSize < 1 {
		cfg.BufferSize = 1
	}
	if cfg.BatchSize < 1 {
		cfg.BatchSize = 1
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = time.Second
	}
	if cfg.PurgeInterval <= 0 {
		cfg.PurgeInterval = time.Hour
	}
	w := &Writer{
		store:   store,
		ch:      make(chan Record, cfg.BufferSize),
		cfg:     cfg,
		metrics: metrics,
		logger:  logger,
		now:     time.Now,
		done:    make(chan struct{}),
	}
	w.wg.Add(1)
	go w.run()
	return w
}

// Record enqueues rec without blocking. When the buffer is full the record is
// dropped and counted so a flood of requests cannot back up onto the request path.
func (w *Writer) Record(rec Record) {
	select {
	case w.ch <- rec:
	default:
		if w.metrics != nil {
			w.metrics.AuditDropped.Inc()
		}
	}
}

// Close stops the writer, flushes buffered records within ctx, and closes the
// store.
func (w *Writer) Close(ctx context.Context) error {
	w.closeOnce.Do(func() { close(w.done) })
	drained := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(drained)
	}()
	select {
	case <-drained:
	case <-ctx.Done():
	}
	return w.store.Close()
}

func (w *Writer) run() {
	defer w.wg.Done()

	flush := time.NewTicker(w.cfg.FlushInterval)
	defer flush.Stop()
	purge := time.NewTicker(w.cfg.PurgeInterval)
	defer purge.Stop()

	w.purge()

	batch := make([]Record, 0, w.cfg.BatchSize)
	for {
		select {
		case rec := <-w.ch:
			batch = append(batch, rec)
			if len(batch) >= w.cfg.BatchSize {
				batch = w.flush(batch)
			}
		case <-flush.C:
			batch = w.flush(batch)
		case <-purge.C:
			w.purge()
		case <-w.done:
			batch = w.drain(batch)
			w.flush(batch)
			return
		}
	}
}

// drain pulls every buffered record so nothing queued before shutdown is lost.
func (w *Writer) drain(batch []Record) []Record {
	for {
		select {
		case rec := <-w.ch:
			batch = append(batch, rec)
			if len(batch) >= w.cfg.BatchSize {
				batch = w.flush(batch)
			}
		default:
			return batch
		}
	}
}

// flush writes the batch and resets it. A failed write drops the batch rather
// than retrying so a broken store never stalls the writer; the loss is counted.
func (w *Writer) flush(batch []Record) []Record {
	if len(batch) == 0 {
		return batch
	}
	ctx, cancel := context.WithTimeout(context.Background(), flushTimeout)
	defer cancel()
	if err := w.store.Insert(ctx, batch); err != nil {
		if w.metrics != nil {
			w.metrics.AuditWriteErrors.Inc()
		}
		w.logger.Warn("audit batch write failed", "records", len(batch), "error", err.Error())
	} else if w.metrics != nil {
		w.metrics.AuditWritten.Add(float64(len(batch)))
	}
	return batch[:0]
}

// purge deletes records older than the retention window so the store stays
// bounded over time.
func (w *Writer) purge() {
	if w.cfg.Retention <= 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), purgeTimeout)
	defer cancel()
	before := w.now().Add(-w.cfg.Retention)
	n, err := w.store.Purge(ctx, before)
	if err != nil {
		w.logger.Warn("audit retention purge failed", "error", err.Error())
		return
	}
	if n > 0 {
		w.logger.Info("audit retention purge", "deleted", n, "before", before.UTC().Format(time.RFC3339))
	}
}
