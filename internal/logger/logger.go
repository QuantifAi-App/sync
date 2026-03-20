package logger

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// Level represents a log severity level.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

// String returns the lowercase name of the level for JSON output.
func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "debug"
	case LevelInfo:
		return "info"
	case LevelWarn:
		return "warn"
	case LevelError:
		return "error"
	default:
		return "unknown"
	}
}

// ParseLevel converts a string to a Level. Defaults to LevelInfo for
// unrecognized values so configuration typos do not silently suppress logs.
func ParseLevel(s string) Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return LevelDebug
	case "info":
		return LevelInfo
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

// entry is a single structured log record written as JSON.
type entry struct {
	Time    string         `json:"time"`
	Level   string         `json:"level"`
	Message string         `json:"msg"`
	Fields  map[string]any `json:"fields,omitempty"`
}

// sensitiveKeys are field names that must never appear in log output.
var sensitiveKeys = map[string]bool{
	"api_key":  true,
	"apikey":   true,
	"api-key":  true,
	"password": true,
	"secret":   true,
	"token":    true,
}

// Logger provides structured JSON logging to stderr and an optional file.
type Logger struct {
	mu       sync.Mutex
	level    Level
	writers  []io.Writer
	fileOut  *os.File // non-nil when a log file is open; closed on Close()
}

// New creates a Logger that writes JSON to stderr at the given level.
// If logFilePath is non-empty, log output is also written to that file.
func New(level Level, logFilePath string) (*Logger, error) {
	l := &Logger{
		level:   level,
		writers: []io.Writer{os.Stderr},
	}
	if logFilePath != "" {
		f, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			return nil, fmt.Errorf("open log file: %w", err)
		}
		l.fileOut = f
		l.writers = append(l.writers, f)
	}
	return l, nil
}

// Close releases any open log file handle.
func (l *Logger) Close() error {
	if l.fileOut != nil {
		return l.fileOut.Close()
	}
	return nil
}

// log is the core emit method. It filters by level, redacts sensitive fields,
// and writes a single JSON line to all configured writers.
func (l *Logger) log(lvl Level, msg string, fields map[string]any) {
	if lvl < l.level {
		return
	}

	safe := redactSensitive(fields)
	e := entry{
		Time:    time.Now().UTC().Format(time.RFC3339),
		Level:   lvl.String(),
		Message: msg,
		Fields:  safe,
	}

	data, err := json.Marshal(e)
	if err != nil {
		// Best-effort fallback when marshalling fails
		data = []byte(fmt.Sprintf(`{"time":"%s","level":"%s","msg":"log marshal error"}`, e.Time, e.Level))
	}
	data = append(data, '\n')

	l.mu.Lock()
	defer l.mu.Unlock()
	for _, w := range l.writers {
		_, _ = w.Write(data)
	}
}

// Debug logs at debug level.
func (l *Logger) Debug(msg string, fields map[string]any) {
	l.log(LevelDebug, msg, fields)
}

// Info logs at info level.
func (l *Logger) Info(msg string, fields map[string]any) {
	l.log(LevelInfo, msg, fields)
}

// Warn logs at warn level.
func (l *Logger) Warn(msg string, fields map[string]any) {
	l.log(LevelWarn, msg, fields)
}

// Error logs at error level.
func (l *Logger) Error(msg string, fields map[string]any) {
	l.log(LevelError, msg, fields)
}

// redactSensitive returns a copy of fields with sensitive values replaced.
func redactSensitive(fields map[string]any) map[string]any {
	if len(fields) == 0 {
		return nil
	}
	safe := make(map[string]any, len(fields))
	for k, v := range fields {
		if sensitiveKeys[strings.ToLower(k)] {
			safe[k] = "[REDACTED]"
		} else {
			safe[k] = v
		}
	}
	return safe
}
