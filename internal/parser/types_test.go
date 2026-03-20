package parser

import (
	"encoding/json"
	"testing"
)

// TestMessageRecordGoldenJSON verifies that a fully populated MessageRecord
// serializes to JSON with field names that match server/models.py exactly.
// This is the schema compatibility golden test: any drift between the Go
// struct tags and the Pydantic model will cause this test to fail.
func TestMessageRecordGoldenJSON(t *testing.T) {
	gitName := "Nino Chavez"
	gitEmail := "nino@example.com"
	osUser := "nino.chavez"
	machineID := "ninos-macbook.local"
	version := "2.1.50"
	branch := "main"
	cwd := "/Users/nino.chavez/Workspace/dev/apps/my-project"

	contentText := "Hello, can you help me?"
	contentLen := len(contentText)

	rec := MessageRecord{
		MessageID:                "msg_01WCgAbwjSCoZNPk92q5V3NM",
		SessionID:                "00e5c901-50fc-486f-9fd4-9888ecd96864",
		Timestamp:                "2026-02-23T20:05:33.549Z",
		Model:                    "claude-opus-4-6",
		InputTokens:              3,
		OutputTokens:             11,
		CacheReadInputTokens:     18719,
		CacheCreationInputTokens: 2176,
		EstCost:                  0.0468,
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
		RecordType:               "assistant",
		ContentText:              &contentText,
		ContentLength:            &contentLen,
	}

	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	// All field names from server/models.py must be present in the JSON output.
	expectedFields := []string{
		"message_id", "session_id", "timestamp", "model",
		"input_tokens", "output_tokens",
		"cache_read_input_tokens", "cache_creation_input_tokens",
		"est_cost", "project_path", "display_name",
		"git_name", "git_email", "os_username", "machine_id",
		"claude_version", "git_branch", "cwd",
		"tool_names", "file_paths",
		"record_type", "content_text", "content_length",
	}

	// Unmarshal into a generic map to check field names
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}

	if len(raw) != len(expectedFields) {
		t.Errorf("expected %d fields, got %d", len(expectedFields), len(raw))
	}

	for _, field := range expectedFields {
		if _, ok := raw[field]; !ok {
			t.Errorf("missing JSON field %q", field)
		}
	}
}
