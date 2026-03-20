package sender

import (
	"context"
	"sync"
	"time"

	"github.com/quantifai/sync/internal/parser"
)

// FlushFunc is called by the buffer when a flush trigger fires.  It
// receives the batch of records to send and returns true if the send
// succeeded (so the buffer can clear those records).
type FlushFunc func(ctx context.Context, records []parser.MessageRecord) bool

// Buffer accumulates enriched MessageRecord structs and flushes them
// when either the batch size threshold or the flush interval timer fires,
// whichever comes first.  It is safe for concurrent use.
type Buffer struct {
	mu            sync.Mutex
	records       []parser.MessageRecord
	batchSize     int
	flushInterval time.Duration
	flushFn       FlushFunc
	maxBatchSize  int // server-enforced maximum per request (10,000)
}

// NewBuffer creates a buffer that flushes at batchSize records or every
// flushInterval, whichever comes first.  The flushFn callback handles
// the actual HTTP send.
func NewBuffer(batchSize int, flushInterval time.Duration, flushFn FlushFunc) *Buffer {
	return &Buffer{
		records:       make([]parser.MessageRecord, 0, batchSize),
		batchSize:     batchSize,
		flushInterval: flushInterval,
		flushFn:       flushFn,
		maxBatchSize:  10_000,
	}
}

// Add appends a record to the buffer.  If the buffer reaches batchSize,
// a flush is triggered synchronously before returning.
func (b *Buffer) Add(ctx context.Context, rec parser.MessageRecord) {
	b.mu.Lock()
	b.records = append(b.records, rec)
	shouldFlush := len(b.records) >= b.batchSize
	b.mu.Unlock()

	if shouldFlush {
		b.Flush(ctx)
	}
}

// Len returns the current number of buffered records.
func (b *Buffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.records)
}

// Flush sends all buffered records via the flush function, splitting into
// chunks of maxBatchSize (10,000) if the buffer exceeds the server limit.
// Records are only cleared after a successful send.
func (b *Buffer) Flush(ctx context.Context) {
	b.mu.Lock()
	if len(b.records) == 0 {
		b.mu.Unlock()
		return
	}

	// Take ownership of the current records slice
	pending := b.records
	b.records = make([]parser.MessageRecord, 0, b.batchSize)
	b.mu.Unlock()

	// Split oversized batches to respect the 10,000-record server limit
	for len(pending) > 0 {
		end := len(pending)
		if end > b.maxBatchSize {
			end = b.maxBatchSize
		}
		chunk := pending[:end]
		pending = pending[end:]

		if !b.flushFn(ctx, chunk) {
			// On failure, put unsent records back into the buffer so they
			// are retried on the next flush cycle.
			b.mu.Lock()
			b.records = append(chunk, b.records...)
			b.records = append(b.records, pending...)
			b.mu.Unlock()
			return
		}
	}
}

// Run starts the timer-based flush loop.  It blocks until ctx is
// cancelled, at which point it performs a final flush of any remaining
// records (graceful shutdown).
func (b *Buffer) Run(ctx context.Context) {
	ticker := time.NewTicker(b.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			b.Flush(ctx)
		case <-ctx.Done():
			// Graceful shutdown: flush remaining records with a background
			// context so the send is not immediately cancelled.
			b.Flush(context.Background())
			return
		}
	}
}
