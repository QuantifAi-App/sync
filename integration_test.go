package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quantifai/sync/internal/config"
	"github.com/quantifai/sync/internal/parser"
	"github.com/quantifai/sync/internal/reader"
	"github.com/quantifai/sync/internal/sender"
	"github.com/quantifai/sync/internal/state"
	"github.com/quantifai/sync/internal/watcher"

	"github.com/quantifai/sync/internal/logger"
)

// newIntegrationLogger creates a logger that writes to stderr at debug level.
// Integration tests use this to avoid nil-pointer issues in the sender.
func newIntegrationLogger() *logger.Logger {
	l, _ := logger.New(logger.LevelDebug, "")
	return l
}

// makeAssistantJSONL builds a single JSONL line representing an assistant
// record with usage data, tool_use content blocks, and all metadata fields
// the parser expects.  The n parameter provides per-record uniqueness.
func makeAssistantJSONL(n int) string {
	return fmt.Sprintf(`{"type":"assistant","sessionId":"session-%04d","timestamp":"2026-03-01T12:%02d:00.000Z","cwd":"/Users/test/project","version":"2.1.50","gitBranch":"main","message":{"id":"msg_%04d","model":"claude-sonnet-4-6","usage":{"input_tokens":%d,"output_tokens":%d,"cache_read_input_tokens":500,"cache_creation_input_tokens":100},"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/src/main.go"}},{"type":"tool_use","name":"Write","input":{"file_path":"/src/util.go","content":"..."}}]}}`,
		n, n%60, n, 100+n, 50+n)
}

// ---------------------------------------------------------------------------
// Test 1: End-to-end pipeline (watcher -> reader -> parser -> buffer -> sender)
// ---------------------------------------------------------------------------

func TestIntegrationEndToEndPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Set up a mock HTTP server that captures ingest requests
	var receivedMu sync.Mutex
	var receivedRecords []parser.MessageRecord

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/ingest" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var req parser.IngestRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		receivedMu.Lock()
		receivedRecords = append(receivedRecords, req.Records...)
		receivedMu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(parser.IngestResponse{
			Accepted: len(req.Records),
		})
	}))
	defer srv.Close()

	// Create a temp directory to act as the watch directory
	watchDir := t.TempDir()
	projectDir := filepath.Join(watchDir, "-test-project")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}

	// Start file watcher
	w, err := watcher.New(watchDir)
	if err != nil {
		t.Fatalf("watcher.New: %v", err)
	}
	if err := w.Start(); err != nil {
		t.Fatalf("watcher.Start: %v", err)
	}
	defer w.Close()

	// Set up state manager
	statePath := filepath.Join(t.TempDir(), "state.json")
	stateMgr, err := state.NewManager(statePath)
	if err != nil {
		t.Fatalf("state.NewManager: %v", err)
	}

	// Set up sender
	s, err := sender.New(srv.URL, "test-key", newIntegrationLogger(),
		sender.WithHTTPClient(srv.Client()),
		sender.WithSleepFunc(func(_ time.Duration) {}),
	)
	if err != nil {
		t.Fatalf("sender.New: %v", err)
	}

	// Set up buffer with small batch and short interval for fast test
	buf := sender.NewBuffer(5, 100*time.Millisecond, func(ctx context.Context, records []parser.MessageRecord) bool {
		return s.Send(ctx, records)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start buffer run loop
	go buf.Run(ctx)

	// Allow watcher to settle
	time.Sleep(100 * time.Millisecond)

	// Write a .jsonl file with 3 assistant records
	jsonlPath := filepath.Join(projectDir, "test-session.jsonl")
	var lines string
	for i := 0; i < 3; i++ {
		lines += makeAssistantJSONL(i) + "\n"
	}
	if err := os.WriteFile(jsonlPath, []byte(lines), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Process events from the watcher: read file, parse, buffer
	deadline := time.After(10 * time.Second)
	eventsProcessed := 0

eventLoop:
	for {
		select {
		case evt := <-w.Events():
			// Read the file from its last known offset
			fs := stateMgr.Get(evt.Path)
			result, err := reader.ReadFromOffset(evt.Path, fs.ByteOffset)
			if err != nil {
				t.Fatalf("ReadFromOffset: %v", err)
			}

			projectPath := filepath.Base(filepath.Dir(evt.Path))
			for _, line := range result.Lines {
				rec := parser.ParseRecord(json.RawMessage(line), projectPath)
				if rec != nil {
					buf.Add(ctx, *rec)
				}
			}

			// Update state with new offset
			stateMgr.Set(evt.Path, state.FileState{
				ByteOffset: result.NewOffset,
				Mtime:      float64(time.Now().Unix()),
			})
			eventsProcessed++
			if eventsProcessed >= 1 {
				break eventLoop
			}

		case <-deadline:
			t.Fatal("timed out waiting for watcher event")
		}
	}

	// Wait for the buffer's timer to flush
	time.Sleep(300 * time.Millisecond)

	// Verify the mock server received correctly-formed records
	receivedMu.Lock()
	count := len(receivedRecords)
	receivedMu.Unlock()

	if count != 3 {
		t.Fatalf("expected 3 records at mock server, got %d", count)
	}

	receivedMu.Lock()
	first := receivedRecords[0]
	receivedMu.Unlock()

	// Verify key fields on the first record
	if first.MessageID != "msg_0000" {
		t.Errorf("MessageID: got %q, want %q", first.MessageID, "msg_0000")
	}
	if first.Model != "claude-sonnet-4-6" {
		t.Errorf("Model: got %q, want %q", first.Model, "claude-sonnet-4-6")
	}
	if first.InputTokens != 100 {
		t.Errorf("InputTokens: got %d, want 100", first.InputTokens)
	}
	if first.EstCost <= 0 {
		t.Error("EstCost should be > 0")
	}
	if len(first.ToolNames) != 2 {
		t.Errorf("ToolNames length: got %d, want 2", len(first.ToolNames))
	}
	if len(first.FilePaths) != 2 {
		t.Errorf("FilePaths length: got %d, want 2", len(first.FilePaths))
	}
}

// ---------------------------------------------------------------------------
// Test 2: Offset persistence -- only new data is read on append
// ---------------------------------------------------------------------------

func TestIntegrationOffsetPersistence(t *testing.T) {
	tmpDir := t.TempDir()
	jsonlPath := filepath.Join(tmpDir, "session.jsonl")
	statePath := filepath.Join(tmpDir, "state.json")

	// Write initial 2 records
	var initial string
	for i := 0; i < 2; i++ {
		initial += makeAssistantJSONL(i) + "\n"
	}
	if err := os.WriteFile(jsonlPath, []byte(initial), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Read from offset 0 -- should get 2 lines
	result1, err := reader.ReadFromOffset(jsonlPath, 0)
	if err != nil {
		t.Fatalf("first ReadFromOffset: %v", err)
	}
	if len(result1.Lines) != 2 {
		t.Fatalf("first read: expected 2 lines, got %d", len(result1.Lines))
	}

	// Persist offset in state manager
	stateMgr, err := state.NewManager(statePath)
	if err != nil {
		t.Fatalf("state.NewManager: %v", err)
	}
	stateMgr.Set(jsonlPath, state.FileState{
		ByteOffset: result1.NewOffset,
		Mtime:      float64(time.Now().Unix()),
	})
	if err := stateMgr.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Append 3 more records
	f, err := os.OpenFile(jsonlPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	for i := 2; i < 5; i++ {
		f.WriteString(makeAssistantJSONL(i) + "\n")
	}
	f.Close()

	// Reload state and read from persisted offset
	stateMgr2, err := state.NewManager(statePath)
	if err != nil {
		t.Fatalf("state.NewManager (reload): %v", err)
	}
	fs := stateMgr2.Get(jsonlPath)

	result2, err := reader.ReadFromOffset(jsonlPath, fs.ByteOffset)
	if err != nil {
		t.Fatalf("second ReadFromOffset: %v", err)
	}

	// Should only get the 3 new lines, not re-read the original 2
	if len(result2.Lines) != 3 {
		t.Fatalf("second read: expected 3 new lines, got %d", len(result2.Lines))
	}

	// Verify the first of the new lines is record index 2
	rec := parser.ParseRecord(json.RawMessage(result2.Lines[0]), "test-project")
	if rec == nil {
		t.Fatal("ParseRecord returned nil for new record")
	}
	if rec.MessageID != "msg_0002" {
		t.Errorf("new record MessageID: got %q, want %q", rec.MessageID, "msg_0002")
	}
}

// ---------------------------------------------------------------------------
// Test 3: Network failure recovery -- retry 503 then succeed, verify records accepted
// ---------------------------------------------------------------------------

func TestIntegrationNetworkFailureRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	var attempts atomic.Int32
	var acceptedRecords atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable) // 503
			return
		}
		var req parser.IngestRequest
		json.NewDecoder(r.Body).Decode(&req)
		acceptedRecords.Add(int32(len(req.Records)))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(parser.IngestResponse{Accepted: len(req.Records)})
	}))
	defer srv.Close()

	// Build a small pipeline: parse records, buffer, send
	tmpDir := t.TempDir()
	jsonlPath := filepath.Join(tmpDir, "session.jsonl")
	statePath := filepath.Join(tmpDir, "state.json")

	// Write 5 records
	var content string
	for i := 0; i < 5; i++ {
		content += makeAssistantJSONL(i) + "\n"
	}
	os.WriteFile(jsonlPath, []byte(content), 0644)

	// Read and parse all records
	result, err := reader.ReadFromOffset(jsonlPath, 0)
	if err != nil {
		t.Fatalf("ReadFromOffset: %v", err)
	}

	var records []parser.MessageRecord
	for _, line := range result.Lines {
		rec := parser.ParseRecord(json.RawMessage(line), "test-project")
		if rec != nil {
			records = append(records, *rec)
		}
	}

	if len(records) != 5 {
		t.Fatalf("expected 5 parsed records, got %d", len(records))
	}

	// Send with retry
	var delays []time.Duration
	var delayMu sync.Mutex

	s, err := sender.New(srv.URL, "test-key", newIntegrationLogger(),
		sender.WithHTTPClient(srv.Client()),
		sender.WithSleepFunc(func(d time.Duration) {
			delayMu.Lock()
			delays = append(delays, d)
			delayMu.Unlock()
		}),
	)
	if err != nil {
		t.Fatalf("sender.New: %v", err)
	}

	ok := s.Send(context.Background(), records)
	if !ok {
		t.Fatal("Send should have succeeded after retries")
	}

	// Verify 3 total attempts (2 failures + 1 success)
	if attempts.Load() != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts.Load())
	}

	// Verify all 5 records were accepted
	if acceptedRecords.Load() != 5 {
		t.Errorf("expected 5 accepted records, got %d", acceptedRecords.Load())
	}

	// Verify backoff delays were recorded
	delayMu.Lock()
	defer delayMu.Unlock()
	if len(delays) != 2 {
		t.Fatalf("expected 2 backoff delays, got %d", len(delays))
	}

	// Now update state to confirm offset persistence after successful send
	stateMgr, err := state.NewManager(statePath)
	if err != nil {
		t.Fatalf("state.NewManager: %v", err)
	}
	stateMgr.Set(jsonlPath, state.FileState{
		ByteOffset: result.NewOffset,
		Mtime:      float64(time.Now().Unix()),
	})
	if err := stateMgr.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify state was persisted correctly
	stateMgr2, err := state.NewManager(statePath)
	if err != nil {
		t.Fatalf("state.NewManager (reload): %v", err)
	}
	fs := stateMgr2.Get(jsonlPath)
	if fs.ByteOffset != result.NewOffset {
		t.Errorf("persisted offset: got %d, want %d", fs.ByteOffset, result.NewOffset)
	}
}

// ---------------------------------------------------------------------------
// Test 4: Schema compatibility golden test -- Go JSON matches Python fields
// ---------------------------------------------------------------------------

func TestIntegrationSchemaCompatibilityGolden(t *testing.T) {
	// Build a fully-populated MessageRecord matching the exact example from
	// the spec's API compatibility section.
	gitName := "Nino Chavez"
	gitEmail := "nino@example.com"
	osUser := "nino.chavez"
	machineID := "ninos-macbook.local"
	version := "2.1.50"
	branch := "main"
	cwd := "/Users/nino.chavez/Workspace/dev/apps/my-project"

	rec := parser.MessageRecord{
		MessageID:                "msg_01WCgAbwjSCoZNPk92q5V3NM",
		SessionID:                "00e5c901-50fc-486f-9fd4-9888ecd96864",
		Timestamp:                "2026-02-23T20:05:33.549Z",
		Model:                    "claude-opus-4-6",
		InputTokens:              3,
		OutputTokens:             11,
		CacheReadInputTokens:     18719,
		CacheCreationInputTokens: 2176,
		EstCost:                  0.076768125,
		ProjectPath:              "-Users-nino-chavez-Workspace-dev-apps-my-project",
		DisplayName:              "~/Workspace/dev/apps/my-project",
		GitName:                  &gitName,
		GitEmail:                 &gitEmail,
		OsUsername:               &osUser,
		MachineID:                &machineID,
		ClaudeVersion:            &version,
		GitBranch:                &branch,
		Cwd:                      &cwd,
		ToolNames:                []string{"Read", "Write", "Bash"},
		FilePaths:                []string{"/src/main.py", "/tests/test_main.py"},
	}

	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Field names expected from server/models.py MessageRecord.
	expectedFields := map[string]bool{
		"message_id":                 true,
		"session_id":                 true,
		"timestamp":                  true,
		"model":                      true,
		"input_tokens":               true,
		"output_tokens":              true,
		"cache_read_input_tokens":    true,
		"cache_creation_input_tokens": true,
		"est_cost":                   true,
		"project_path":               true,
		"display_name":               true,
		"git_name":                   true,
		"git_email":                  true,
		"os_username":                true,
		"machine_id":                 true,
		"claude_version":             true,
		"git_branch":                 true,
		"cwd":                        true,
		"tool_names":                 true,
		"file_paths":                 true,
		"record_type":               true,
	}

	if len(raw) != len(expectedFields) {
		t.Errorf("field count mismatch: Go produces %d fields, spec expects %d", len(raw), len(expectedFields))
	}

	for field := range expectedFields {
		if _, ok := raw[field]; !ok {
			t.Errorf("missing expected field %q in Go JSON output", field)
		}
	}

	// No extra fields
	for field := range raw {
		if !expectedFields[field] {
			t.Errorf("unexpected extra field %q in Go JSON output", field)
		}
	}

	// Verify the record can be wrapped in an IngestRequest envelope
	envelope := parser.IngestRequest{Records: []parser.MessageRecord{rec}}
	envData, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal IngestRequest: %v", err)
	}

	var envRaw map[string]json.RawMessage
	json.Unmarshal(envData, &envRaw)
	if _, ok := envRaw["records"]; !ok {
		t.Error("IngestRequest missing 'records' field")
	}

	// Verify specific field values match the spec example
	var decoded parser.MessageRecord
	json.Unmarshal(data, &decoded)

	if decoded.MessageID != "msg_01WCgAbwjSCoZNPk92q5V3NM" {
		t.Errorf("message_id: got %q", decoded.MessageID)
	}
	if decoded.Model != "claude-opus-4-6" {
		t.Errorf("model: got %q", decoded.Model)
	}
	if decoded.InputTokens != 3 {
		t.Errorf("input_tokens: got %d", decoded.InputTokens)
	}
}

// ---------------------------------------------------------------------------
// Test 5: Pricing parity -- Go cost matches known Python output
// ---------------------------------------------------------------------------

func TestIntegrationPricingParity(t *testing.T) {
	// Reference records with known Python costs computed by hand using the
	// formula: sum(tokens * rate / 1,000,000) for each token category.
	tests := []struct {
		name         string
		model        string
		input        int
		output       int
		cacheRead    int
		cacheCreate  int
		expectedCost float64
	}{
		{
			name:  "opus reference from spec",
			model: "claude-opus-4-6",
			input: 3, output: 11, cacheRead: 18719, cacheCreate: 2176,
			// 3*15/1e6 + 11*75/1e6 + 18719*1.875/1e6 + 2176*18.75/1e6
			expectedCost: 0.076768125,
		},
		{
			name:  "sonnet moderate usage",
			model: "claude-sonnet-4-6",
			input: 5000, output: 2000, cacheRead: 10000, cacheCreate: 1000,
			// 5000*3/1e6 + 2000*15/1e6 + 10000*0.375/1e6 + 1000*3.75/1e6
			expectedCost: 0.052500,
		},
		{
			name:  "haiku high volume",
			model: "claude-haiku-4-5-20251001",
			input: 50000, output: 10000, cacheRead: 100000, cacheCreate: 5000,
			// 50000*0.80/1e6 + 10000*4.0/1e6 + 100000*0.08/1e6 + 5000*1.0/1e6
			expectedCost: 0.093,
		},
		{
			name:  "opus with alias model",
			model: "claude-opus-4-5-20251101",
			input: 1000, output: 500, cacheRead: 0, cacheCreate: 0,
			// 1000*15/1e6 + 500*75/1e6
			expectedCost: 0.0525,
		},
		{
			name:  "unknown model falls back to opus",
			model: "claude-unknown-99",
			input: 1000, output: 500, cacheRead: 0, cacheCreate: 0,
			// Same as opus: 1000*15/1e6 + 500*75/1e6
			expectedCost: 0.0525,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := parser.GetPricing(tc.model)
			got := parser.CalculateCost(p, tc.input, tc.output, tc.cacheRead, tc.cacheCreate)
			diff := math.Abs(got - tc.expectedCost)
			if diff > 0.0001 {
				t.Errorf("cost mismatch for %q: got %.10f, want %.10f (diff %.10f)",
					tc.model, got, tc.expectedCost, diff)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Test 6: State pruning -- stale entries for non-existent files are removed
// ---------------------------------------------------------------------------

func TestIntegrationStatePruning(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	// Create two real files
	realFile1 := filepath.Join(tmpDir, "real1.jsonl")
	realFile2 := filepath.Join(tmpDir, "real2.jsonl")
	os.WriteFile(realFile1, []byte("data\n"), 0644)
	os.WriteFile(realFile2, []byte("data\n"), 0644)

	// Create a state manager with entries for real and stale files
	mgr, err := state.NewManager(statePath)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	mgr.Set(realFile1, state.FileState{ByteOffset: 100, Mtime: 1740825600.0})
	mgr.Set(realFile2, state.FileState{ByteOffset: 200, Mtime: 1740825700.0})
	mgr.Set("/nonexistent/stale1.jsonl", state.FileState{ByteOffset: 300, Mtime: 1740825800.0})
	mgr.Set("/nonexistent/stale2.jsonl", state.FileState{ByteOffset: 400, Mtime: 1740825900.0})
	mgr.Set("/also/missing/stale3.jsonl", state.FileState{ByteOffset: 500, Mtime: 1740826000.0})

	if err := mgr.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Reload state (simulating a restart)
	mgr2, err := state.NewManager(statePath)
	if err != nil {
		t.Fatalf("NewManager (reload): %v", err)
	}

	// Verify all 5 entries loaded
	if mgr2.TrackedFiles() != 5 {
		t.Fatalf("expected 5 tracked files after load, got %d", mgr2.TrackedFiles())
	}

	// Prune stale entries
	pruned := mgr2.Prune()
	if pruned != 3 {
		t.Errorf("expected 3 pruned entries, got %d", pruned)
	}

	if mgr2.TrackedFiles() != 2 {
		t.Errorf("expected 2 remaining tracked files, got %d", mgr2.TrackedFiles())
	}

	// Real files should still have their offsets
	fs1 := mgr2.Get(realFile1)
	if fs1.ByteOffset != 100 {
		t.Errorf("realFile1 offset: got %d, want 100", fs1.ByteOffset)
	}
	fs2 := mgr2.Get(realFile2)
	if fs2.ByteOffset != 200 {
		t.Errorf("realFile2 offset: got %d, want 200", fs2.ByteOffset)
	}

	// Stale entries should be gone
	fs3 := mgr2.Get("/nonexistent/stale1.jsonl")
	if fs3.ByteOffset != 0 {
		t.Errorf("stale entry should be gone, got offset %d", fs3.ByteOffset)
	}
}

// ---------------------------------------------------------------------------
// Test 7: Config layering integration -- env > user > system > defaults
// ---------------------------------------------------------------------------

func TestIntegrationConfigLayering(t *testing.T) {
	tmpDir := t.TempDir()

	systemPath := filepath.Join(tmpDir, "system.toml")
	userPath := filepath.Join(tmpDir, "user.toml")

	// System config: provides api_url, batch_size, log_level
	os.WriteFile(systemPath, []byte(`
api_url = "https://system.example.com"
batch_size = 3000
log_level = "debug"
flush_interval = 30
health_port = 19000
`), 0600)

	// User config: overrides api_url and batch_size from system, adds api_key
	os.WriteFile(userPath, []byte(`
api_url = "https://user.example.com"
batch_size = 4000
api_key = "user-file-key"
`), 0600)

	// Env vars: override api_url from both files
	// Clear both new and legacy env vars to avoid interference
	for _, prefix := range []string{"QUANTIFAI_", "AI_OPS_"} {
		for _, suffix := range []string{"API_URL", "API_KEY", "BATCH_SIZE", "LOG_LEVEL",
			"FLUSH_INTERVAL", "HEALTH_PORT", "SYNC_ENABLED", "AUTO_UPDATE",
			"UPDATE_CHANNEL", "WATCH_DIR", "STATE_FILE", "LOG_FILE"} {
			t.Setenv(prefix+suffix, "")
		}
	}
	t.Setenv("QUANTIFAI_API_URL", "https://env.example.com")

	cfg, err := config.Load(userPath, systemPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	// Env wins for api_url
	if cfg.APIURL != "https://env.example.com" {
		t.Errorf("APIURL: got %q, want env value", cfg.APIURL)
	}

	// User file wins for batch_size (no env set)
	if cfg.BatchSize != 4000 {
		t.Errorf("BatchSize: got %d, want 4000 (user file)", cfg.BatchSize)
	}

	// System file wins for log_level (user file did not set it, no env set)
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel: got %q, want debug (system file)", cfg.LogLevel)
	}

	// System file wins for flush_interval (user file did not set it)
	if cfg.FlushInterval != 30 {
		t.Errorf("FlushInterval: got %d, want 30 (system file)", cfg.FlushInterval)
	}

	// System file wins for health_port (user file did not set it)
	if cfg.HealthPort != 19000 {
		t.Errorf("HealthPort: got %d, want 19000 (system file)", cfg.HealthPort)
	}

	// User file wins for api_key
	if cfg.APIKey != "user-file-key" {
		t.Errorf("APIKey: got %q, want user-file-key", cfg.APIKey)
	}

	// Defaults win for auto_update (not set anywhere)
	if cfg.AutoUpdate {
		t.Error("AutoUpdate: got true, want false (default)")
	}

	// Defaults win for update_channel (not set anywhere)
	if cfg.UpdateChannel != "stable" {
		t.Errorf("UpdateChannel: got %q, want stable (default)", cfg.UpdateChannel)
	}

	// Verify validation passes
	if err := config.Validate(cfg); err != nil {
		t.Errorf("Validate should pass: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Test 8: Large file -- 10,000+ records processed correctly
// ---------------------------------------------------------------------------

func TestIntegrationLargeFile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large file integration test in short mode")
	}

	tmpDir := t.TempDir()
	jsonlPath := filepath.Join(tmpDir, "large-session.jsonl")

	// Write 10,500 records
	const totalRecords = 10500
	f, err := os.Create(jsonlPath)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	for i := 0; i < totalRecords; i++ {
		f.WriteString(makeAssistantJSONL(i) + "\n")
	}
	f.Close()

	// Read all records
	result, err := reader.ReadFromOffset(jsonlPath, 0)
	if err != nil {
		t.Fatalf("ReadFromOffset: %v", err)
	}
	if len(result.Lines) != totalRecords {
		t.Fatalf("expected %d lines, got %d", totalRecords, len(result.Lines))
	}

	// Parse all records
	var parsedRecords []parser.MessageRecord
	for _, line := range result.Lines {
		rec := parser.ParseRecord(json.RawMessage(line), "test-project")
		if rec != nil {
			parsedRecords = append(parsedRecords, *rec)
		}
	}

	if len(parsedRecords) != totalRecords {
		t.Fatalf("expected %d parsed records, got %d", totalRecords, len(parsedRecords))
	}

	// Verify first and last records are correct
	if parsedRecords[0].MessageID != "msg_0000" {
		t.Errorf("first record MessageID: got %q", parsedRecords[0].MessageID)
	}
	lastIdx := totalRecords - 1
	expectedLastID := fmt.Sprintf("msg_%04d", lastIdx)
	if parsedRecords[lastIdx].MessageID != expectedLastID {
		t.Errorf("last record MessageID: got %q, want %q",
			parsedRecords[lastIdx].MessageID, expectedLastID)
	}

	// Verify all records have non-zero cost
	for i, rec := range parsedRecords {
		if rec.EstCost <= 0 {
			t.Errorf("record %d has zero/negative cost: %f", i, rec.EstCost)
			break
		}
	}

	// Batch and send to a mock server, verifying 10,000-record splitting
	var batchSizesMu sync.Mutex
	var batchSizes []int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req parser.IngestRequest
		json.NewDecoder(r.Body).Decode(&req)
		batchSizesMu.Lock()
		batchSizes = append(batchSizes, len(req.Records))
		batchSizesMu.Unlock()
		json.NewEncoder(w).Encode(parser.IngestResponse{Accepted: len(req.Records)})
	}))
	defer srv.Close()

	s, err := sender.New(srv.URL, "test-key", newIntegrationLogger(),
		sender.WithHTTPClient(srv.Client()),
		sender.WithSleepFunc(func(_ time.Duration) {}),
	)
	if err != nil {
		t.Fatalf("sender.New: %v", err)
	}

	buf := sender.NewBuffer(totalRecords, 1*time.Hour, func(ctx context.Context, records []parser.MessageRecord) bool {
		return s.Send(ctx, records)
	})

	ctx := context.Background()
	for _, rec := range parsedRecords {
		buf.Add(ctx, rec)
	}

	// The batch size equals totalRecords so Add triggered a flush. The buffer
	// should have split 10,500 into 10,000 + 500.
	batchSizesMu.Lock()
	defer batchSizesMu.Unlock()

	if len(batchSizes) != 2 {
		t.Fatalf("expected 2 HTTP requests (10000 + 500), got %d: %v", len(batchSizes), batchSizes)
	}
	if batchSizes[0] != 10000 {
		t.Errorf("first batch: got %d, want 10000", batchSizes[0])
	}
	if batchSizes[1] != 500 {
		t.Errorf("second batch: got %d, want 500", batchSizes[1])
	}
}

// ---------------------------------------------------------------------------
// Test 9: Concurrent file writes -- multiple .jsonl files detected by watcher
// ---------------------------------------------------------------------------

func TestIntegrationConcurrentFileWrites(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrent file write integration test in short mode")
	}

	watchDir := t.TempDir()

	w, err := watcher.New(watchDir)
	if err != nil {
		t.Fatalf("watcher.New: %v", err)
	}
	if err := w.Start(); err != nil {
		t.Fatalf("watcher.Start: %v", err)
	}
	defer w.Close()

	// Allow watcher to settle
	time.Sleep(100 * time.Millisecond)

	// Write 5 .jsonl files concurrently
	const numFiles = 5
	var wg sync.WaitGroup
	for i := 0; i < numFiles; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			path := filepath.Join(watchDir, fmt.Sprintf("session-%d.jsonl", idx))
			content := makeAssistantJSONL(idx) + "\n"
			os.WriteFile(path, []byte(content), 0644)
		}(i)
	}
	wg.Wait()

	// Collect events -- we expect at least one event per file (Create or Write)
	seen := make(map[string]bool)
	deadline := time.After(5 * time.Second)

	for len(seen) < numFiles {
		select {
		case evt := <-w.Events():
			seen[evt.Path] = true
		case <-deadline:
			t.Fatalf("timed out: received events for %d/%d files: %v", len(seen), numFiles, seen)
		}
	}

	// Verify each file can be read and parsed
	for path := range seen {
		result, err := reader.ReadFromOffset(path, 0)
		if err != nil {
			t.Errorf("ReadFromOffset(%q): %v", path, err)
			continue
		}
		if len(result.Lines) != 1 {
			t.Errorf("file %q: expected 1 line, got %d", path, len(result.Lines))
			continue
		}
		rec := parser.ParseRecord(json.RawMessage(result.Lines[0]), "test-project")
		if rec == nil {
			t.Errorf("file %q: ParseRecord returned nil", path)
		}
	}
}

// ---------------------------------------------------------------------------
// Test 10: Graceful shutdown -- buffered records are flushed before exit
// ---------------------------------------------------------------------------

func TestIntegrationGracefulShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping graceful shutdown integration test in short mode")
	}

	var flushedRecords atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req parser.IngestRequest
		json.NewDecoder(r.Body).Decode(&req)
		flushedRecords.Add(int32(len(req.Records)))
		json.NewEncoder(w).Encode(parser.IngestResponse{Accepted: len(req.Records)})
	}))
	defer srv.Close()

	s, err := sender.New(srv.URL, "test-key", newIntegrationLogger(),
		sender.WithHTTPClient(srv.Client()),
		sender.WithSleepFunc(func(_ time.Duration) {}),
	)
	if err != nil {
		t.Fatalf("sender.New: %v", err)
	}

	// Large batch size and long interval so neither trigger fires naturally
	buf := sender.NewBuffer(10000, 1*time.Hour, func(ctx context.Context, records []parser.MessageRecord) bool {
		return s.Send(ctx, records)
	})

	ctx, cancel := context.WithCancel(context.Background())

	// Parse some records from a temp file and add to buffer
	tmpDir := t.TempDir()
	jsonlPath := filepath.Join(tmpDir, "session.jsonl")
	var content string
	for i := 0; i < 7; i++ {
		content += makeAssistantJSONL(i) + "\n"
	}
	os.WriteFile(jsonlPath, []byte(content), 0644)

	result, err := reader.ReadFromOffset(jsonlPath, 0)
	if err != nil {
		t.Fatalf("ReadFromOffset: %v", err)
	}

	for _, line := range result.Lines {
		rec := parser.ParseRecord(json.RawMessage(line), "test-project")
		if rec != nil {
			buf.Add(ctx, *rec)
		}
	}

	// Verify nothing has been flushed yet (batch size 10000, interval 1h)
	if flushedRecords.Load() != 0 {
		t.Fatalf("expected 0 flushed records before shutdown, got %d", flushedRecords.Load())
	}

	// Start the Run loop and immediately trigger shutdown
	done := make(chan struct{})
	go func() {
		buf.Run(ctx)
		close(done)
	}()

	// Cancel context to simulate SIGTERM shutdown
	cancel()

	// Wait for Run to complete
	select {
	case <-done:
		// Run completed
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not complete within timeout after shutdown signal")
	}

	// Verify all 7 records were flushed during shutdown
	if flushedRecords.Load() != 7 {
		t.Errorf("expected 7 records flushed on shutdown, got %d", flushedRecords.Load())
	}
}
