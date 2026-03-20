package parser

import "testing"

func TestIsLiteKey(t *testing.T) {
	tests := []struct {
		key  string
		want bool
	}{
		{"ql_abc123def456", true},
		{"ql_", true},
		{"qla_something", false},
		{"enterprise-key-here", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsLiteKey(tt.key); got != tt.want {
			t.Errorf("IsLiteKey(%q) = %v, want %v", tt.key, got, tt.want)
		}
	}
}

func TestScrubForLite(t *testing.T) {
	prompt := "Fix the auth middleware"
	length := 22
	cwd := "/Users/nino/project"
	branch := "main"
	gitName := "Nino"
	gitEmail := "nino@example.com"
	osUser := "nino"
	machineID := "macbook-pro.local"

	rec := &MessageRecord{
		MessageID:   "msg-123",
		SessionID:   "sess-456",
		Timestamp:   "2026-03-19T00:00:00Z",
		Model:       "claude-sonnet-4.6",
		InputTokens: 1000,
		OutputTokens: 500,
		EstCost:     0.05,
		ProjectPath: "-Users-nino-Workspace-dev-my-project",
		DisplayName: "~/Workspace/dev/my-project",
		RecordType:  "user",
		ContentText: &prompt,
		ContentLength: &length,
		FilePaths:   []string{"/Users/nino/project/src/auth.ts"},
		GitName:     &gitName,
		GitEmail:    &gitEmail,
		OsUsername:  &osUser,
		MachineID:   &machineID,
		Cwd:         &cwd,
		GitBranch:   &branch,
		ToolNames:   []string{"Read", "Edit"},
	}

	ScrubForLite(rec)

	// Preserved fields
	if rec.MessageID != "msg-123" {
		t.Error("MessageID should be preserved")
	}
	if rec.SessionID != "sess-456" {
		t.Error("SessionID should be preserved")
	}
	if rec.Model != "claude-sonnet-4.6" {
		t.Error("Model should be preserved")
	}
	if rec.InputTokens != 1000 || rec.OutputTokens != 500 {
		t.Error("Token counts should be preserved")
	}
	if rec.EstCost != 0.05 {
		t.Error("EstCost should be preserved")
	}
	if len(rec.ToolNames) != 2 {
		t.Error("ToolNames should be preserved")
	}

	// Stripped fields
	if rec.ContentText != nil {
		t.Error("ContentText should be nil")
	}
	if rec.ContentLength != nil {
		t.Error("ContentLength should be nil")
	}
	if rec.FilePaths != nil {
		t.Error("FilePaths should be nil")
	}
	if rec.GitName != nil {
		t.Error("GitName should be nil")
	}
	if rec.GitEmail != nil {
		t.Error("GitEmail should be nil")
	}
	if rec.OsUsername != nil {
		t.Error("OsUsername should be nil")
	}
	if rec.MachineID != nil {
		t.Error("MachineID should be nil")
	}
	if rec.Cwd != nil {
		t.Error("Cwd should be nil")
	}
	if rec.GitBranch != nil {
		t.Error("GitBranch should be nil")
	}

	// Project path scrubbed to last segment
	// Encoded path "-Users-nino-Workspace-dev-my-project" → last dash segment = "project"
	// (dash-encoded paths can't distinguish hyphens in names from separators)
	if rec.ProjectPath != "project" {
		t.Errorf("ProjectPath should be 'project', got %q", rec.ProjectPath)
	}
	if rec.DisplayName != "my-project" {
		t.Errorf("DisplayName should be 'my-project', got %q", rec.DisplayName)
	}
}

func TestLastPathSegment(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"~/Workspace/dev/my-project", "my-project"},
		{"-Users-nino-Workspace-dev-my-project", "project"},
		{"my-project", "my-project"},
		{"", "unknown"},
		{"/", "unknown"},
	}
	for _, tt := range tests {
		if got := lastPathSegment(tt.path); got != tt.want {
			t.Errorf("lastPathSegment(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}
