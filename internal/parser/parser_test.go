package parser

import (
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"
)

// completeAssistantRecord is a realistic JSONL record with all fields populated
// that both parse_record() and identity fields would produce.  Used as the
// foundation for multiple test cases.
func completeAssistantRecord() string {
	return `{
		"type": "assistant",
		"sessionId": "00e5c901-50fc-486f-9fd4-9888ecd96864",
		"timestamp": "2026-02-23T20:05:33.549Z",
		"cwd": "/Users/nino.chavez/Workspace/dev/apps/my-project",
		"version": "2.1.50",
		"gitBranch": "main",
		"message": {
			"id": "msg_01WCgAbwjSCoZNPk92q5V3NM",
			"model": "claude-opus-4-6",
			"usage": {
				"input_tokens": 3,
				"output_tokens": 11,
				"cache_read_input_tokens": 18719,
				"cache_creation_input_tokens": 2176
			},
			"content": [
				{"type": "text", "text": "Hello"},
				{
					"type": "tool_use",
					"name": "Read",
					"input": {"file_path": "/src/main.py"}
				},
				{
					"type": "tool_use",
					"name": "Write",
					"input": {"file_path": "/tests/test_main.py", "content": "..."}
				},
				{
					"type": "tool_use",
					"name": "Bash",
					"input": {"command": "ls -la"}
				}
			]
		}
	}`
}

// TestParseRecordComplete verifies that parse_record() with a complete
// assistant record produces all 21 fields correctly (identity fields are set
// by the caller, so this test checks the 17 non-identity fields plus the
// slice fields).
func TestParseRecordComplete(t *testing.T) {
	raw := json.RawMessage(completeAssistantRecord())
	projectPath := "-Users-nino-chavez-Workspace-dev-apps-my-project"

	rec := ParseRecord(raw, projectPath)
	if rec == nil {
		t.Fatal("ParseRecord returned nil for a valid assistant record")
	}

	// Required string fields
	if rec.MessageID != "msg_01WCgAbwjSCoZNPk92q5V3NM" {
		t.Errorf("MessageID: got %q, want %q", rec.MessageID, "msg_01WCgAbwjSCoZNPk92q5V3NM")
	}
	if rec.SessionID != "00e5c901-50fc-486f-9fd4-9888ecd96864" {
		t.Errorf("SessionID: got %q, want %q", rec.SessionID, "00e5c901-50fc-486f-9fd4-9888ecd96864")
	}
	if rec.Timestamp != "2026-02-23T20:05:33.549Z" {
		t.Errorf("Timestamp: got %q, want %q", rec.Timestamp, "2026-02-23T20:05:33.549Z")
	}
	if rec.Model != "claude-opus-4-6" {
		t.Errorf("Model: got %q, want %q", rec.Model, "claude-opus-4-6")
	}
	if rec.ProjectPath != projectPath {
		t.Errorf("ProjectPath: got %q, want %q", rec.ProjectPath, projectPath)
	}

	// Token fields
	if rec.InputTokens != 3 {
		t.Errorf("InputTokens: got %d, want 3", rec.InputTokens)
	}
	if rec.OutputTokens != 11 {
		t.Errorf("OutputTokens: got %d, want 11", rec.OutputTokens)
	}
	if rec.CacheReadInputTokens != 18719 {
		t.Errorf("CacheReadInputTokens: got %d, want 18719", rec.CacheReadInputTokens)
	}
	if rec.CacheCreationInputTokens != 2176 {
		t.Errorf("CacheCreationInputTokens: got %d, want 2176", rec.CacheCreationInputTokens)
	}

	// Cost should be non-zero for this record
	if rec.EstCost <= 0 {
		t.Errorf("EstCost: got %f, want > 0", rec.EstCost)
	}

	// Optional pointer fields
	if rec.Cwd == nil || *rec.Cwd != "/Users/nino.chavez/Workspace/dev/apps/my-project" {
		t.Errorf("Cwd: got %v, want /Users/nino.chavez/Workspace/dev/apps/my-project", rec.Cwd)
	}
	if rec.ClaudeVersion == nil || *rec.ClaudeVersion != "2.1.50" {
		t.Errorf("ClaudeVersion: got %v, want 2.1.50", rec.ClaudeVersion)
	}
	if rec.GitBranch == nil || *rec.GitBranch != "main" {
		t.Errorf("GitBranch: got %v, want main", rec.GitBranch)
	}

	// Tool names extracted from content blocks
	expectedTools := []string{"Read", "Write", "Bash"}
	if len(rec.ToolNames) != len(expectedTools) {
		t.Fatalf("ToolNames length: got %d, want %d", len(rec.ToolNames), len(expectedTools))
	}
	for i, name := range expectedTools {
		if rec.ToolNames[i] != name {
			t.Errorf("ToolNames[%d]: got %q, want %q", i, rec.ToolNames[i], name)
		}
	}

	// File paths extracted from tool_use input params
	expectedPaths := []string{"/src/main.py", "/tests/test_main.py"}
	if len(rec.FilePaths) != len(expectedPaths) {
		t.Fatalf("FilePaths length: got %d, want %d", len(rec.FilePaths), len(expectedPaths))
	}
	for i, p := range expectedPaths {
		if rec.FilePaths[i] != p {
			t.Errorf("FilePaths[%d]: got %q, want %q", i, rec.FilePaths[i], p)
		}
	}

	// RecordType should be "assistant"
	if rec.RecordType != "assistant" {
		t.Errorf("RecordType: got %q, want %q", rec.RecordType, "assistant")
	}

	// Identity fields should be nil (caller sets them)
	if rec.GitName != nil {
		t.Errorf("GitName: got %v, want nil (caller sets this)", rec.GitName)
	}
	if rec.GitEmail != nil {
		t.Errorf("GitEmail: got %v, want nil (caller sets this)", rec.GitEmail)
	}
	if rec.OsUsername != nil {
		t.Errorf("OsUsername: got %v, want nil (caller sets this)", rec.OsUsername)
	}
	if rec.MachineID != nil {
		t.Errorf("MachineID: got %v, want nil (caller sets this)", rec.MachineID)
	}
}

// TestParseRecordSkipsIrrelevantTypes verifies that non-assistant/non-user
// record types are correctly skipped and return nil.
func TestParseRecordSkipsIrrelevantTypes(t *testing.T) {
	types := []string{"system", "progress", "queue-operation", "file-history-snapshot"}
	for _, typ := range types {
		t.Run(typ, func(t *testing.T) {
			raw := json.RawMessage(`{
				"type": "` + typ + `",
				"sessionId": "abc123",
				"timestamp": "2026-02-23T20:05:33.549Z",
				"message": {
					"usage": {"input_tokens": 100}
				}
			}`)
			rec := ParseRecord(raw, "test-project")
			if rec != nil {
				t.Errorf("expected nil for type=%q, got %+v", typ, rec)
			}
		})
	}
}

// TestParseRecordSkipsMissingUsage verifies that assistant records without a
// valid usage dict are skipped.
func TestParseRecordSkipsMissingUsage(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{
			name: "no message field",
			raw:  `{"type": "assistant", "timestamp": "2026-02-23T20:05:33.549Z"}`,
		},
		{
			name: "message without usage",
			raw: `{
				"type": "assistant",
				"timestamp": "2026-02-23T20:05:33.549Z",
				"message": {"model": "claude-opus-4-6"}
			}`,
		},
		{
			name: "empty usage object",
			raw: `{
				"type": "assistant",
				"timestamp": "2026-02-23T20:05:33.549Z",
				"message": {"model": "claude-opus-4-6", "usage": {}}
			}`,
		},
		{
			name: "usage is not a dict",
			raw: `{
				"type": "assistant",
				"timestamp": "2026-02-23T20:05:33.549Z",
				"message": {"model": "claude-opus-4-6", "usage": "invalid"}
			}`,
		},
		{
			name: "missing timestamp",
			raw: `{
				"type": "assistant",
				"message": {"model": "claude-opus-4-6", "usage": {"input_tokens": 10}}
			}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := ParseRecord(json.RawMessage(tc.raw), "test-project")
			if rec != nil {
				t.Errorf("expected nil for %q, got %+v", tc.name, rec)
			}
		})
	}
}

// TestParseRecordGeneratesMessageID verifies that when message.id is missing,
// a UUID-based message ID is generated with the "msg_gen_" prefix and 24 hex
// characters.
func TestParseRecordGeneratesMessageID(t *testing.T) {
	raw := json.RawMessage(`{
		"type": "assistant",
		"sessionId": "session-1",
		"timestamp": "2026-02-23T20:05:33.549Z",
		"message": {
			"model": "claude-opus-4-6",
			"usage": {"input_tokens": 10, "output_tokens": 5}
		}
	}`)

	rec := ParseRecord(raw, "test-project")
	if rec == nil {
		t.Fatal("ParseRecord returned nil for a valid record without message.id")
	}

	if !strings.HasPrefix(rec.MessageID, "msg_gen_") {
		t.Errorf("generated MessageID should start with 'msg_gen_', got %q", rec.MessageID)
	}

	// "msg_gen_" is 8 chars + 24 hex chars = 32 total
	suffix := rec.MessageID[len("msg_gen_"):]
	if len(suffix) != 24 {
		t.Errorf("generated ID suffix length: got %d, want 24", len(suffix))
	}

	// Verify uniqueness: two calls should produce different IDs
	rec2 := ParseRecord(raw, "test-project")
	if rec2 == nil {
		t.Fatal("second ParseRecord returned nil")
	}
	if rec.MessageID == rec2.MessageID {
		t.Error("two generated message IDs should be unique")
	}
}

// TestExtractToolNames verifies that tool names are correctly extracted from
// tool_use content blocks, skipping non-tool_use blocks.
func TestExtractToolNames(t *testing.T) {
	blocks := []json.RawMessage{
		json.RawMessage(`{"type": "text", "text": "Hello world"}`),
		json.RawMessage(`{"type": "tool_use", "name": "Read", "input": {}}`),
		json.RawMessage(`{"type": "tool_use", "name": "Write", "input": {}}`),
		json.RawMessage(`{"type": "tool_result", "content": "ok"}`),
		json.RawMessage(`{"type": "tool_use", "name": "Bash", "input": {}}`),
		// Missing name field
		json.RawMessage(`{"type": "tool_use", "input": {}}`),
		// Empty name
		json.RawMessage(`{"type": "tool_use", "name": "", "input": {}}`),
		// Invalid JSON
		json.RawMessage(`not json`),
	}

	names := ExtractToolNames(blocks)
	expected := []string{"Read", "Write", "Bash"}

	if len(names) != len(expected) {
		t.Fatalf("got %d tool names, want %d: %v", len(names), len(expected), names)
	}
	for i, name := range expected {
		if names[i] != name {
			t.Errorf("names[%d]: got %q, want %q", i, names[i], name)
		}
	}
}

// TestExtractFilePaths verifies that file paths are extracted from tool_use
// input params using the keys: file_path, path, file, filename.
func TestExtractFilePaths(t *testing.T) {
	blocks := []json.RawMessage{
		// file_path key
		json.RawMessage(`{"type": "tool_use", "name": "Read", "input": {"file_path": "/src/main.py"}}`),
		// path key
		json.RawMessage(`{"type": "tool_use", "name": "Write", "input": {"path": "/tests/test.py", "content": "..."}}`),
		// file key
		json.RawMessage(`{"type": "tool_use", "name": "Edit", "input": {"file": "/config.toml"}}`),
		// filename key
		json.RawMessage(`{"type": "tool_use", "name": "Upload", "input": {"filename": "/data.csv"}}`),
		// Non-tool_use block -- should be ignored
		json.RawMessage(`{"type": "text", "text": "some text"}`),
		// tool_use without input -- should be ignored
		json.RawMessage(`{"type": "tool_use", "name": "Bash"}`),
		// tool_use with non-dict input -- should be ignored
		json.RawMessage(`{"type": "tool_use", "name": "Bash", "input": "string"}`),
		// tool_use with no path keys in input -- should be ignored
		json.RawMessage(`{"type": "tool_use", "name": "Bash", "input": {"command": "ls"}}`),
		// tool_use with empty path value -- should be ignored
		json.RawMessage(`{"type": "tool_use", "name": "Read", "input": {"file_path": ""}}`),
	}

	paths := ExtractFilePaths(blocks)
	expected := []string{"/src/main.py", "/tests/test.py", "/config.toml", "/data.csv"}

	if len(paths) != len(expected) {
		t.Fatalf("got %d file paths, want %d: %v", len(paths), len(expected), paths)
	}
	for i, p := range expected {
		if paths[i] != p {
			t.Errorf("paths[%d]: got %q, want %q", i, paths[i], p)
		}
	}
}

// TestDecodeProjectPath verifies that encoded project paths are decoded into
// human-readable display names with ~/ prefix.  Tests both the home-prefix
// match path and the fallback path.
func TestDecodeProjectPath(t *testing.T) {
	// Build the expected home-based encoded path for this machine.
	// This mirrors the Python _HOME_PREFIX logic from src/utils.py.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("cannot determine home dir: %v", err)
	}

	encodedHome := strings.ReplaceAll(home, "/", "-")
	encodedHome = strings.ReplaceAll(encodedHome, ".", "-")
	if !strings.HasPrefix(encodedHome, "-") {
		encodedHome = "-" + encodedHome
	}

	tests := []struct {
		name     string
		encoded  string
		expected string
	}{
		{
			name:     "home prefix match",
			encoded:  encodedHome + "-Workspace-dev-apps-my-project",
			expected: "~/Workspace/dev/apps/my/project",
		},
		{
			name:     "fallback strips leading dash",
			encoded:  "-other-path-to-project",
			expected: "other/path/to/project",
		},
		{
			name:     "no leading dash",
			encoded:  "some-project",
			expected: "some/project",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DecodeProjectPath(tc.encoded)
			if got != tc.expected {
				t.Errorf("DecodeProjectPath(%q): got %q, want %q", tc.encoded, got, tc.expected)
			}
		})
	}
}

// TestParseRecordCostMatchesPython verifies that the Go cost calculation for
// a known record matches the Python parse_record() output within the
// floating-point tolerance specified by the spec (< 0.0001 USD).
func TestParseRecordCostMatchesPython(t *testing.T) {
	raw := json.RawMessage(completeAssistantRecord())
	projectPath := "-Users-nino-chavez-Workspace-dev-apps-my-project"

	rec := ParseRecord(raw, projectPath)
	if rec == nil {
		t.Fatal("ParseRecord returned nil for a valid record")
	}

	// Python reference calculation:
	// 3 * 15.0 / 1e6 = 0.000045
	// 11 * 75.0 / 1e6 = 0.000825
	// 18719 * 1.875 / 1e6 = 0.035098125
	// 2176 * 18.75 / 1e6 = 0.040800
	// Total = 0.076768125
	expectedCost := 0.076768125

	diff := math.Abs(rec.EstCost - expectedCost)
	if diff > 0.0001 {
		t.Errorf("est_cost: got %.10f, want %.10f (diff %.10f > 0.0001 tolerance)",
			rec.EstCost, expectedCost, diff)
	}
}

// TestExtractToolNamesNilInput verifies that nil/empty content blocks return
// nil (not an empty slice or error).
func TestExtractToolNamesNilInput(t *testing.T) {
	names := ExtractToolNames(nil)
	if names != nil {
		t.Errorf("expected nil for nil input, got %v", names)
	}

	names = ExtractToolNames([]json.RawMessage{})
	if names != nil {
		t.Errorf("expected nil for empty input, got %v", names)
	}
}

// TestExtractFilePathsNilInput verifies that nil/empty content blocks return
// nil.
func TestExtractFilePathsNilInput(t *testing.T) {
	paths := ExtractFilePaths(nil)
	if paths != nil {
		t.Errorf("expected nil for nil input, got %v", paths)
	}

	paths = ExtractFilePaths([]json.RawMessage{})
	if paths != nil {
		t.Errorf("expected nil for empty input, got %v", paths)
	}
}

// TestParseUserRecord verifies that type=="user" records are parsed correctly
// with prompt text, zero tokens, and record_type="user".
func TestParseUserRecord(t *testing.T) {
	raw := json.RawMessage(`{
		"type": "user",
		"sessionId": "5904d910-ac75-446c-8467-99220c5ebc60",
		"timestamp": "2026-03-10T02:15:00.000Z",
		"uuid": "f315dd2c-4506-4da5-96d1-1ccd9ca7ac66",
		"cwd": "/Users/nino/Workspace/dev/wip/ai-ops-analytics",
		"version": "2.1.72",
		"gitBranch": "main",
		"message": {
			"role": "user",
			"content": "how is it that sessions can have 0 prompts?"
		}
	}`)

	rec := ParseRecord(raw, "test-project")
	if rec == nil {
		t.Fatal("ParseRecord returned nil for a valid user record")
	}

	if rec.RecordType != "user" {
		t.Errorf("RecordType: got %q, want %q", rec.RecordType, "user")
	}
	if rec.MessageID != "f315dd2c-4506-4da5-96d1-1ccd9ca7ac66" {
		t.Errorf("MessageID: got %q, want uuid value", rec.MessageID)
	}
	if rec.SessionID != "5904d910-ac75-446c-8467-99220c5ebc60" {
		t.Errorf("SessionID: got %q", rec.SessionID)
	}
	if rec.InputTokens != 0 || rec.OutputTokens != 0 || rec.EstCost != 0 {
		t.Errorf("User records should have zero tokens/cost, got in=%d out=%d cost=%f",
			rec.InputTokens, rec.OutputTokens, rec.EstCost)
	}
	if rec.ContentText == nil {
		t.Fatal("ContentText should not be nil")
	}
	if *rec.ContentText != "how is it that sessions can have 0 prompts?" {
		t.Errorf("ContentText: got %q", *rec.ContentText)
	}
	if rec.ContentLength == nil || *rec.ContentLength != 43 {
		t.Errorf("ContentLength: got %d, want 43", func() int { if rec.ContentLength != nil { return *rec.ContentLength }; return -1 }())
	}
	if rec.GitBranch == nil || *rec.GitBranch != "main" {
		t.Errorf("GitBranch: got %v, want main", rec.GitBranch)
	}
}

// TestParseUserRecordNoContent verifies user records without message content
// still parse (with nil content fields).
func TestParseUserRecordNoContent(t *testing.T) {
	raw := json.RawMessage(`{
		"type": "user",
		"sessionId": "abc-123",
		"timestamp": "2026-03-10T00:00:00.000Z",
		"uuid": "def-456"
	}`)

	rec := ParseRecord(raw, "test-project")
	if rec == nil {
		t.Fatal("ParseRecord returned nil for user record without content")
	}
	if rec.RecordType != "user" {
		t.Errorf("RecordType: got %q, want user", rec.RecordType)
	}
	if rec.ContentText != nil {
		t.Errorf("ContentText should be nil for empty content, got %q", *rec.ContentText)
	}
}
