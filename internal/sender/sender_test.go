package sender

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quantifai/sync/internal/logger"
	"github.com/quantifai/sync/internal/parser"
)

// makeRecords builds n dummy MessageRecord values for testing.
func makeRecords(n int) []parser.MessageRecord {
	records := make([]parser.MessageRecord, n)
	for i := range records {
		records[i] = parser.MessageRecord{
			MessageID:   fmt.Sprintf("msg_%05d", i),
			SessionID:   "test-session",
			Timestamp:   "2026-03-01T12:00:00Z",
			Model:       "claude-opus-4-6",
			InputTokens: 10,
		}
	}
	return records
}

// newTestLogger creates a logger that discards output (debug level so all
// messages are processed but written to nowhere meaningful in tests).
func newTestLogger() *logger.Logger {
	l, _ := logger.New(logger.LevelDebug, "")
	return l
}

// noopSleep replaces time.Sleep in tests to avoid real delays.
func noopSleep(_ time.Duration) {}

// ---------------------------------------------------------------------------
// Test 1: Buffer flushes at batch_size threshold
// ---------------------------------------------------------------------------

func TestBufferFlushesAtBatchSize(t *testing.T) {
	const batchSize = 10
	var flushedCount int
	var mu sync.Mutex

	flushFn := func(_ context.Context, records []parser.MessageRecord) bool {
		mu.Lock()
		flushedCount += len(records)
		mu.Unlock()
		return true
	}

	buf := NewBuffer(batchSize, 1*time.Hour, flushFn)
	ctx := context.Background()

	// Add exactly batchSize records -- should trigger flush
	for i := 0; i < batchSize; i++ {
		buf.Add(ctx, parser.MessageRecord{MessageID: fmt.Sprintf("msg_%d", i)})
	}

	mu.Lock()
	got := flushedCount
	mu.Unlock()

	if got != batchSize {
		t.Errorf("expected %d records flushed, got %d", batchSize, got)
	}
	if buf.Len() != 0 {
		t.Errorf("expected buffer to be empty after flush, got %d records", buf.Len())
	}
}

// ---------------------------------------------------------------------------
// Test 2: Buffer flushes at flush_interval timeout
// ---------------------------------------------------------------------------

func TestBufferFlushesAtInterval(t *testing.T) {
	var flushedCount atomic.Int32

	flushFn := func(_ context.Context, records []parser.MessageRecord) bool {
		flushedCount.Add(int32(len(records)))
		return true
	}

	// Very short interval so the timer fires quickly in tests
	buf := NewBuffer(1000, 50*time.Millisecond, flushFn)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Add fewer records than batch size
	buf.Add(ctx, parser.MessageRecord{MessageID: "msg_0"})
	buf.Add(ctx, parser.MessageRecord{MessageID: "msg_1"})

	// Start the run loop in a goroutine
	go buf.Run(ctx)

	// Wait for the timer to fire
	time.Sleep(200 * time.Millisecond)
	cancel()

	if flushedCount.Load() < 2 {
		t.Errorf("expected at least 2 records flushed via timer, got %d", flushedCount.Load())
	}
}

// ---------------------------------------------------------------------------
// Test 3: Buffer flushes whichever trigger fires first (count vs timer)
// ---------------------------------------------------------------------------

func TestBufferFlushesWhicheverFirst(t *testing.T) {
	// Batch size of 3, very long interval -- count trigger should fire first
	var flushed atomic.Int32

	flushFn := func(_ context.Context, records []parser.MessageRecord) bool {
		flushed.Add(int32(len(records)))
		return true
	}

	buf := NewBuffer(3, 1*time.Hour, flushFn)
	ctx := context.Background()

	buf.Add(ctx, parser.MessageRecord{MessageID: "a"})
	buf.Add(ctx, parser.MessageRecord{MessageID: "b"})

	// Two records -- no flush yet (below batch size, timer is 1 hour away)
	if flushed.Load() != 0 {
		t.Fatal("expected no flush yet")
	}

	// Third record triggers the count-based flush before the timer fires
	buf.Add(ctx, parser.MessageRecord{MessageID: "c"})

	if flushed.Load() != 3 {
		t.Errorf("expected 3 records flushed by count trigger, got %d", flushed.Load())
	}
}

// ---------------------------------------------------------------------------
// Test 4: Sender retries on HTTP 429 with exponential backoff
// ---------------------------------------------------------------------------

func TestSenderRetriesOn429(t *testing.T) {
	var attempts atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		// Third attempt succeeds
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(parser.IngestResponse{Accepted: 1})
	}))
	defer srv.Close()

	var delays []time.Duration
	var mu sync.Mutex
	recordSleep := func(d time.Duration) {
		mu.Lock()
		delays = append(delays, d)
		mu.Unlock()
	}

	s, err := New(srv.URL, "test-key", newTestLogger(),
		WithHTTPClient(srv.Client()),
		WithSleepFunc(recordSleep),
	)
	if err != nil {
		t.Fatal(err)
	}

	ok := s.Send(context.Background(), makeRecords(1))
	if !ok {
		t.Fatal("expected Send to succeed after retries")
	}

	if attempts.Load() != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts.Load())
	}

	mu.Lock()
	defer mu.Unlock()
	// Backoff: attempt 0 -> 1s, attempt 1 -> 2s
	if len(delays) != 2 {
		t.Fatalf("expected 2 backoff delays, got %d", len(delays))
	}
	if delays[0] != 1*time.Second {
		t.Errorf("first backoff: got %v, want 1s", delays[0])
	}
	if delays[1] != 2*time.Second {
		t.Errorf("second backoff: got %v, want 2s", delays[1])
	}
}

// ---------------------------------------------------------------------------
// Test 5: Sender retries on HTTP 5xx with exponential backoff
// ---------------------------------------------------------------------------

func TestSenderRetriesOn5xx(t *testing.T) {
	var attempts atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable) // 503
			return
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(parser.IngestResponse{Accepted: 1})
	}))
	defer srv.Close()

	var delays []time.Duration
	var mu sync.Mutex
	recordSleep := func(d time.Duration) {
		mu.Lock()
		delays = append(delays, d)
		mu.Unlock()
	}

	s, err := New(srv.URL, "test-key", newTestLogger(),
		WithHTTPClient(srv.Client()),
		WithSleepFunc(recordSleep),
	)
	if err != nil {
		t.Fatal(err)
	}

	ok := s.Send(context.Background(), makeRecords(2))
	if !ok {
		t.Fatal("expected Send to succeed after 5xx retries")
	}

	if attempts.Load() != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts.Load())
	}

	mu.Lock()
	defer mu.Unlock()
	// Verify exponential backoff: 1s, 2s
	if len(delays) != 2 {
		t.Fatalf("expected 2 backoff delays, got %d", len(delays))
	}
	if delays[0] != 1*time.Second {
		t.Errorf("first delay: got %v, want 1s", delays[0])
	}
	if delays[1] != 2*time.Second {
		t.Errorf("second delay: got %v, want 2s", delays[1])
	}
}

// ---------------------------------------------------------------------------
// Test 6: Sender fails immediately on non-429 4xx
// ---------------------------------------------------------------------------

func TestSenderFailsImmediatelyOnNonRetryable4xx(t *testing.T) {
	var attempts atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadRequest) // 400
	}))
	defer srv.Close()

	s, err := New(srv.URL, "test-key", newTestLogger(),
		WithHTTPClient(srv.Client()),
		WithSleepFunc(noopSleep),
	)
	if err != nil {
		t.Fatal(err)
	}

	ok := s.Send(context.Background(), makeRecords(3))
	if ok {
		t.Fatal("expected Send to fail on 400")
	}

	// Only one attempt -- no retries on non-retryable 4xx
	if attempts.Load() != 1 {
		t.Errorf("expected exactly 1 attempt (no retry), got %d", attempts.Load())
	}
}

// ---------------------------------------------------------------------------
// Test 7: Sender respects 10,000-record maximum (splits oversized batches)
// ---------------------------------------------------------------------------

func TestBufferSplitsOversizedBatches(t *testing.T) {
	var mu sync.Mutex
	var batchSizes []int

	flushFn := func(_ context.Context, records []parser.MessageRecord) bool {
		mu.Lock()
		batchSizes = append(batchSizes, len(records))
		mu.Unlock()
		return true
	}

	// Batch size of 15,000 -- bigger than the 10,000 server limit
	buf := NewBuffer(15_000, 1*time.Hour, flushFn)
	ctx := context.Background()

	// Add 12,000 records -- enough to trigger a flush by count (>= 15,000 won't trigger,
	// so we manually call Flush to test splitting)
	recs := makeRecords(12_000)
	for _, r := range recs {
		buf.Add(ctx, r)
	}

	// Manually flush to verify the split behavior
	buf.Flush(ctx)

	mu.Lock()
	defer mu.Unlock()

	// 12,000 records should be split into 10,000 + 2,000
	if len(batchSizes) != 2 {
		t.Fatalf("expected 2 HTTP requests for 12,000 records, got %d: %v", len(batchSizes), batchSizes)
	}
	if batchSizes[0] != 10_000 {
		t.Errorf("first batch: got %d, want 10,000", batchSizes[0])
	}
	if batchSizes[1] != 2_000 {
		t.Errorf("second batch: got %d, want 2,000", batchSizes[1])
	}
}

// ---------------------------------------------------------------------------
// Additional: Verify sender sets correct headers (Bearer auth, Content-Type)
// ---------------------------------------------------------------------------

func TestSenderSetsCorrectHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type: got %q, want application/json", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-api-key" {
			t.Errorf("Authorization: got %q, want Bearer test-api-key", got)
		}
		if r.Method != http.MethodPost {
			t.Errorf("Method: got %q, want POST", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(parser.IngestResponse{Accepted: 1})
	}))
	defer srv.Close()

	s, err := New(srv.URL, "test-api-key", newTestLogger(),
		WithHTTPClient(srv.Client()),
		WithSleepFunc(noopSleep),
	)
	if err != nil {
		t.Fatal(err)
	}

	ok := s.Send(context.Background(), makeRecords(1))
	if !ok {
		t.Fatal("expected Send to succeed")
	}
}

// ---------------------------------------------------------------------------
// Verify graceful shutdown flushes remaining records
// ---------------------------------------------------------------------------

func TestBufferFlushesOnGracefulShutdown(t *testing.T) {
	var flushedCount atomic.Int32

	flushFn := func(_ context.Context, records []parser.MessageRecord) bool {
		flushedCount.Add(int32(len(records)))
		return true
	}

	// Large batch size and long interval so neither trigger fires naturally
	buf := NewBuffer(1000, 1*time.Hour, flushFn)
	ctx, cancel := context.WithCancel(context.Background())

	// Add some records without reaching batch size
	buf.Add(ctx, parser.MessageRecord{MessageID: "a"})
	buf.Add(ctx, parser.MessageRecord{MessageID: "b"})
	buf.Add(ctx, parser.MessageRecord{MessageID: "c"})

	// Start the Run loop in a goroutine
	done := make(chan struct{})
	go func() {
		buf.Run(ctx)
		close(done)
	}()

	// Cancel the context to trigger graceful shutdown
	cancel()
	<-done

	if flushedCount.Load() != 3 {
		t.Errorf("expected 3 records flushed on shutdown, got %d", flushedCount.Load())
	}
}

// ---------------------------------------------------------------------------
// Verify TLS enforcement rejects plaintext HTTP for non-localhost
// ---------------------------------------------------------------------------

func TestSenderRejectPlaintextHTTP(t *testing.T) {
	_, err := New("http://example.com", "key", newTestLogger())
	if err == nil {
		t.Fatal("expected error for plaintext HTTP to non-localhost")
	}

	// Localhost should be allowed with plaintext
	s, err := New("http://localhost:8080", "key", newTestLogger())
	if err != nil {
		t.Fatalf("localhost plaintext should be allowed: %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil sender for localhost")
	}
}
