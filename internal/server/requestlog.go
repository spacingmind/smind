package server

import (
	"log"
	"sync"
	"sync/atomic"

	"github.com/spacingmind/smind/internal/store"
)

// defaultRequestLogQueueSize bounds the async writer's channel (cliproxyapi
// precedent: sdk/cliproxy/usage/manager.go uses NewManager(512)). A queue
// this deep absorbs any ordinary burst; if it's ever actually full, the
// database is the bottleneck and dropping is the right call -- see
// requestLogWriter's doc comment.
const defaultRequestLogQueueSize = 512

// requestLogStore is the persistence dependency requestLogWriter needs --
// exactly *store.Store's CreateRequestLog method. Depending on this
// narrow interface, rather than *store.Store directly, lets tests swap in
// a fake that blocks or errors on demand (requestlog_test.go), so the
// queue-full and write-failure paths can be exercised deterministically
// instead of racing a real SQLite database.
type requestLogStore interface {
	CreateRequestLog(store.RequestLog) (store.RequestLog, error)
}

// requestLogWriter is the bounded async writer M1 requires: proxy.serve
// enqueues a row per request and returns immediately, never waiting on the
// database. A full queue or a failed insert is never surfaced to the
// caller -- it only bumps Dropped and logs, per docs/plans/active/
// orchestration-and-metering.md's "Rows go through a bounded async
// writer" acceptance criterion. One goroutine drains the channel
// sequentially, so writes are never concurrent against db.
type requestLogWriter struct {
	db      requestLogStore
	ch      chan store.RequestLog
	dropped atomic.Int64
	wg      sync.WaitGroup
}

// newRequestLogWriter starts the writer's drain goroutine. Close must be
// called exactly once, typically on daemon shutdown (see Server.Close),
// so the goroutine doesn't leak and in-flight rows get a chance to drain.
func newRequestLogWriter(db requestLogStore, queueSize int) *requestLogWriter {
	if queueSize <= 0 {
		queueSize = defaultRequestLogQueueSize
	}
	w := &requestLogWriter{db: db, ch: make(chan store.RequestLog, queueSize)}
	w.wg.Add(1)
	go w.run()
	return w
}

func (w *requestLogWriter) run() {
	defer w.wg.Done()
	for row := range w.ch {
		if _, err := w.db.CreateRequestLog(row); err != nil {
			w.dropped.Add(1)
			log.Printf("request log: write failed: %v", err)
		}
	}
}

// enqueue submits row for async persistence. Never blocks: a full queue
// drops row, counts it, and logs -- the request path must never wait on
// this.
func (w *requestLogWriter) enqueue(row store.RequestLog) {
	select {
	case w.ch <- row:
	default:
		w.dropped.Add(1)
		log.Printf("request log: queue full (size %d), dropping row", cap(w.ch))
	}
}

// Dropped returns the number of rows lost so far, to either a full queue
// or a failed insert.
func (w *requestLogWriter) Dropped() int64 {
	return w.dropped.Load()
}

// Close stops accepting new rows and blocks until every already-enqueued
// row has been written (or failed and counted). Safe to call once.
func (w *requestLogWriter) Close() {
	close(w.ch)
	w.wg.Wait()
}
