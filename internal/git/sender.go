package git

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/quantifai/sync/internal/logger"
)

// defaultQueuePath returns the path to the commit events queue file.
func defaultQueuePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "quantifai", "commit-events.jsonl")
}

// QueueCommitEvent appends a commit event as a JSON line to the local
// queue file.  File locking (flock) prevents concurrent writes from
// multiple repos' post-commit hooks.  This function does no network
// I/O and returns immediately.
func QueueCommitEvent(event *CommitEvent) error {
	path := defaultQueuePath()

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create queue dir: %w", err)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return fmt.Errorf("open queue file: %w", err)
	}
	defer f.Close()

	// Acquire exclusive lock
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("lock queue file: %w", err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	data = append(data, '\n')

	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write event: %w", err)
	}

	return nil
}

// ReadAndClearQueue reads all commit events from the queue file and
// truncates it.  It acquires an exclusive lock to coordinate with
// concurrent QueueCommitEvent calls.
func ReadAndClearQueue(path string) ([]*CommitEvent, error) {
	if path == "" {
		path = defaultQueuePath()
	}

	f, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // no queue file, no events
		}
		return nil, fmt.Errorf("open queue file: %w", err)
	}
	defer f.Close()

	// Acquire exclusive lock
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return nil, fmt.Errorf("lock queue file: %w", err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("read queue file: %w", err)
	}

	// Truncate the file now that we have its contents
	if err := f.Truncate(0); err != nil {
		return nil, fmt.Errorf("truncate queue file: %w", err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		return nil, fmt.Errorf("seek queue file: %w", err)
	}

	// Parse JSONL
	var events []*CommitEvent
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev CommitEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue // skip malformed lines
		}
		events = append(events, &ev)
	}

	return events, nil
}

// commitIngestRequest is the payload sent to /api/v1/ingest/commits.
type commitIngestRequest struct {
	Commits []*CommitEvent `json:"commits"`
}

// FlushCommitQueue reads queued commit events, POSTs them to the
// ingest/commits endpoint, and returns the number of events sent.
// Errors are logged but do not cause a non-zero return; unsent events
// are re-queued on the next flush cycle.
func FlushCommitQueue(apiURL, apiKey string, log *logger.Logger) int {
	events, err := ReadAndClearQueue("")
	if err != nil {
		log.Warn("failed to read commit queue", map[string]any{"error": err.Error()})
		return 0
	}
	if len(events) == 0 {
		return 0
	}

	url := strings.TrimRight(apiURL, "/") + "/api/v1/ingest/commits"
	payload := commitIngestRequest{Commits: events}

	body, err := json.Marshal(payload)
	if err != nil {
		log.Error("failed to marshal commit events", map[string]any{"error": err.Error()})
		return 0
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		log.Error("failed to create commit ingest request", map[string]any{"error": err.Error()})
		return 0
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Warn("failed to send commit events", map[string]any{
			"error":  err.Error(),
			"events": len(events),
		})
		// Re-queue events for next flush
		requeueEvents(events, log)
		return 0
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		log.Info("commit events sent", map[string]any{"count": len(events)})
		return len(events)
	}

	log.Warn("commit ingest returned non-2xx", map[string]any{
		"status": resp.StatusCode,
		"events": len(events),
	})
	// Re-queue on server error (5xx / 429), drop on client error (4xx)
	if resp.StatusCode == 429 || resp.StatusCode >= 500 {
		requeueEvents(events, log)
	}
	return 0
}

// requeueEvents writes events back to the queue file for retry.
func requeueEvents(events []*CommitEvent, log *logger.Logger) {
	for _, ev := range events {
		if err := QueueCommitEvent(ev); err != nil {
			log.Warn("failed to re-queue commit event", map[string]any{
				"error": err.Error(),
				"sha":   ev.CommitSHA,
			})
		}
	}
}
