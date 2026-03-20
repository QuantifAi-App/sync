// Package sender provides the HTTP batch sender with retry/backoff
// and an in-memory record buffer.  The sender replicates the retry
// strategy from src/sync.py send_batch(): retry on HTTP 429 and 5xx
// with exponential backoff (3 attempts, base delay 1s), fail immediately
// on other 4xx errors.
package sender

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/quantifai/sync/internal/logger"
	"github.com/quantifai/sync/internal/parser"
)

// Sender posts batches of MessageRecord to the central API's ingest
// endpoint with Bearer token authentication and automatic retry on
// transient failures.
type Sender struct {
	apiURL     string
	apiKey     string
	client     *http.Client
	log        *logger.Logger
	maxRetries int
	baseDelay  time.Duration

	// sleepFn is the function used to wait between retries.  It defaults
	// to time.Sleep but can be replaced in tests to avoid real delays.
	sleepFn func(time.Duration)
}

// Option configures optional Sender parameters.
type Option func(*Sender)

// WithHTTPClient overrides the default http.Client (useful for testing).
func WithHTTPClient(c *http.Client) Option {
	return func(s *Sender) { s.client = c }
}

// WithMaxRetries overrides the default retry count (3).
func WithMaxRetries(n int) Option {
	return func(s *Sender) { s.maxRetries = n }
}

// WithBaseDelay overrides the default base backoff delay (1s).
func WithBaseDelay(d time.Duration) Option {
	return func(s *Sender) { s.baseDelay = d }
}

// WithSleepFunc overrides time.Sleep for testing to avoid real delays.
func WithSleepFunc(fn func(time.Duration)) Option {
	return func(s *Sender) { s.sleepFn = fn }
}

// New creates a Sender that posts to {apiURL}/api/v1/ingest.
// TLS is enforced unless the URL targets localhost (for local dev).
func New(apiURL, apiKey string, log *logger.Logger, opts ...Option) (*Sender, error) {
	if apiURL == "" {
		return nil, fmt.Errorf("sender: api_url is required")
	}

	// Enforce TLS unless the target is localhost (for local development)
	lower := strings.ToLower(apiURL)
	if strings.HasPrefix(lower, "http://") && !isLocalhost(lower) {
		return nil, fmt.Errorf("sender: plaintext HTTP is not allowed for non-localhost api_url %q; use HTTPS", apiURL)
	}

	s := &Sender{
		apiURL:     strings.TrimRight(apiURL, "/"),
		apiKey:     apiKey,
		client:     &http.Client{Timeout: 30 * time.Second},
		log:        log,
		maxRetries: 3,
		baseDelay:  1 * time.Second,
		sleepFn:    time.Sleep,
	}

	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

// Send posts a batch of records to the ingest endpoint with retry on
// transient failures.  It returns true when the server returns a 2xx
// response, meaning the caller should advance offsets.
//
// Retry strategy (ported from src/sync.py send_batch):
//   - HTTP 429 or 5xx: retry up to maxRetries times with exponential
//     backoff (baseDelay * 2^attempt).
//   - Other HTTP 4xx: log error, drop batch, return false (no retry).
//   - Network error: retry with the same backoff strategy.
//   - 2xx: return true.
func (s *Sender) Send(ctx context.Context, records []parser.MessageRecord) bool {
	url := s.apiURL + "/api/v1/ingest"
	payload := parser.IngestRequest{Records: records}

	body, err := json.Marshal(payload)
	if err != nil {
		s.log.Error("failed to marshal ingest request", map[string]any{
			"error": err.Error(),
		})
		return false
	}

	for attempt := 0; attempt <= s.maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			s.log.Error("failed to create HTTP request", map[string]any{
				"error": err.Error(),
			})
			return false
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+s.apiKey)

		resp, err := s.client.Do(req)
		if err != nil {
			// Network error -- retry
			if attempt < s.maxRetries {
				delay := s.backoffDelay(attempt)
				s.log.Info("retry after network error", map[string]any{
					"attempt":     attempt + 1,
					"max_retries": s.maxRetries,
					"error":       err.Error(),
					"delay_ms":    delay.Milliseconds(),
				})
				s.sleepFn(delay)
				continue
			}
			s.log.Error("all retries exhausted after network error", map[string]any{
				"error":   err.Error(),
				"records": len(records),
			})
			return false
		}

		// Read and close the body
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		status := resp.StatusCode

		// Success
		if status >= 200 && status < 300 {
			var ingestResp parser.IngestResponse
			if err := json.Unmarshal(respBody, &ingestResp); err == nil {
				s.log.Info("batch accepted", map[string]any{
					"accepted": ingestResp.Accepted,
					"errors":   ingestResp.Errors,
				})
			}
			return true
		}

		// Retryable: 429 (rate limit) or 5xx (server error)
		if status == 429 || status >= 500 {
			if attempt < s.maxRetries {
				delay := s.backoffDelay(attempt)
				s.log.Info("retry after HTTP error", map[string]any{
					"attempt":     attempt + 1,
					"max_retries": s.maxRetries,
					"status":      status,
					"delay_ms":    delay.Milliseconds(),
				})
				s.sleepFn(delay)
				continue
			}
			s.log.Error("all retries exhausted", map[string]any{
				"status":  status,
				"records": len(records),
			})
			return false
		}

		// Non-retryable 4xx -- drop batch, advance offsets
		s.log.Error("non-retryable client error, dropping batch", map[string]any{
			"status":  status,
			"records": len(records),
			"body":    string(respBody),
		})
		return false
	}

	return false
}

// backoffDelay calculates the exponential backoff delay for a given attempt.
// Formula: baseDelay * 2^attempt  (matching src/sync.py: base_delay * 2**attempt)
func (s *Sender) backoffDelay(attempt int) time.Duration {
	multiplier := 1 << attempt // 2^attempt
	return s.baseDelay * time.Duration(multiplier)
}

// isLocalhost returns true if the URL targets a localhost address,
// which is the only case where plaintext HTTP is permitted.
func isLocalhost(url string) bool {
	return strings.Contains(url, "://localhost") ||
		strings.Contains(url, "://127.0.0.1") ||
		strings.Contains(url, "://[::1]")
}
