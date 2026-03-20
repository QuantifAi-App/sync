package parser

import "testing"

func TestExtractIntentTag(t *testing.T) {
	tests := []struct {
		name   string
		prompt string
		want   *string
		prefix string // if non-empty, check tag starts with this
	}{
		{
			name:   "clear action verb",
			prompt: "Fix the TypeScript error in the auth middleware where cookies aren't awaited",
			prefix: "fix/",
		},
		{
			name:   "add feature",
			prompt: "Add a new endpoint for user profile updates with validation",
			prefix: "add/",
		},
		{
			name:   "noise prefix - request interrupted",
			prompt: "[Request interrupted by user]",
			want:   nil,
		},
		{
			name:   "noise prefix - continuation",
			prompt: "This session is being continued from a previous one",
			want:   nil,
		},
		{
			name:   "noise prefix - system reminder",
			prompt: "<system-reminder>something here</system-reminder>",
			want:   nil,
		},
		{
			name:   "too short",
			prompt: "fix bug",
			want:   nil,
		},
		{
			name:   "strips file paths",
			prompt: "Refactor the database connection pool in /Users/nino/Workspace/dev/project/server/db.py to use asyncpg",
			prefix: "refactor/",
		},
		{
			name:   "no action verb extracts topic words",
			prompt: "The authentication middleware is throwing errors when cookies are expired",
			prefix: "authentication/",
		},
		{
			name:   "max length enforced",
			prompt: "Implement a comprehensive real-time monitoring dashboard with WebSocket connections, automatic reconnection logic, exponential backoff retry strategies, and graceful degradation to polling when WebSockets are unavailable in the production environment",
			prefix: "implement/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractIntentTag(tt.prompt)

			if tt.want == nil && tt.prefix == "" {
				// Expect nil
				if got != nil {
					t.Errorf("expected nil, got %q", *got)
				}
				return
			}

			if got == nil {
				t.Fatal("expected non-nil tag, got nil")
			}

			if len(*got) > 100 {
				t.Errorf("tag exceeds 100 chars: %d", len(*got))
			}

			if tt.prefix != "" {
				if len(*got) < len(tt.prefix) || (*got)[:len(tt.prefix)] != tt.prefix {
					t.Errorf("expected tag to start with %q, got %q", tt.prefix, *got)
				}
			}

			// Ensure no file paths leaked through
			if contains(*got, "/Users/") || contains(*got, "/home/") {
				t.Errorf("tag contains file path: %q", *got)
			}
		})
	}
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
