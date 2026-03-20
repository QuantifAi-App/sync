package logger

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

// TestLoggerRedactsSensitiveKeys verifies that API keys and other sensitive
// values are never present in log output, even at debug level.
func TestLoggerRedactsSensitiveKeys(t *testing.T) {
	var buf bytes.Buffer

	l := &Logger{
		level:   LevelDebug,
		writers: []io.Writer{&buf},
	}

	l.Info("config loaded", map[string]any{
		"api_url": "https://example.com",
		"api_key": "sk-secret-12345",
		"token":   "bearer-abc",
	})

	output := buf.String()

	// The actual secret value must not appear
	if strings.Contains(output, "sk-secret-12345") {
		t.Error("api_key value leaked into log output")
	}
	if strings.Contains(output, "bearer-abc") {
		t.Error("token value leaked into log output")
	}

	// The redacted placeholder should appear instead
	if !strings.Contains(output, "[REDACTED]") {
		t.Error("expected [REDACTED] placeholder in log output")
	}

	// Non-sensitive values should still be present
	if !strings.Contains(output, "https://example.com") {
		t.Error("non-sensitive api_url value was incorrectly redacted")
	}
}

// TestLoggerLevelFiltering verifies that messages below the configured level
// are suppressed.
func TestLoggerLevelFiltering(t *testing.T) {
	var buf bytes.Buffer

	l := &Logger{
		level:   LevelWarn,
		writers: []io.Writer{&buf},
	}

	l.Debug("debug msg", nil)
	l.Info("info msg", nil)
	l.Warn("warn msg", nil)
	l.Error("error msg", nil)

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	if len(lines) != 2 {
		t.Fatalf("expected 2 log lines (warn + error), got %d: %s", len(lines), output)
	}

	// Verify the messages that got through
	if !strings.Contains(output, "warn msg") {
		t.Error("warn message was suppressed")
	}
	if !strings.Contains(output, "error msg") {
		t.Error("error message was suppressed")
	}
}

// TestLoggerOutputFormat verifies the JSON structure of log entries.
func TestLoggerOutputFormat(t *testing.T) {
	var buf bytes.Buffer

	l := &Logger{
		level:   LevelDebug,
		writers: []io.Writer{&buf},
	}

	l.Info("test message", map[string]any{"count": 42})

	var entry struct {
		Time    string         `json:"time"`
		Level   string         `json:"level"`
		Message string         `json:"msg"`
		Fields  map[string]any `json:"fields"`
	}

	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("log output is not valid JSON: %v\noutput: %s", err, buf.String())
	}

	if entry.Level != "info" {
		t.Errorf("level: got %q, want %q", entry.Level, "info")
	}
	if entry.Message != "test message" {
		t.Errorf("msg: got %q, want %q", entry.Message, "test message")
	}
	if entry.Time == "" {
		t.Error("time field is empty")
	}
	if entry.Fields["count"] != float64(42) {
		t.Errorf("fields.count: got %v, want 42", entry.Fields["count"])
	}
}
