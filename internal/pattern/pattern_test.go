package pattern

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckPrompt_Disabled(t *testing.T) {
	m := New("http://localhost", "key", false)
	if matches := m.CheckPrompt("fix Vercel build failure"); matches != nil {
		t.Errorf("expected nil when disabled, got %v", matches)
	}
}

func TestCheckPrompt_ShortPrompt(t *testing.T) {
	m := New("http://localhost", "key", true)
	if matches := m.CheckPrompt("hi"); matches != nil {
		t.Errorf("expected nil for short prompt, got %v", matches)
	}
}

func TestCheckPrompt_ServerMatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/knowledge/match" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected auth: %s", r.Header.Get("Authorization"))
		}

		var req matchRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Prompt == "" {
			t.Error("empty prompt in request")
		}

		json.NewEncoder(w).Encode(matchResponse{
			Patterns: []MatchResult{
				{PatternID: "p1", Intent: "Fix Vercel build", Similarity: 0.85},
			},
			Count: 1,
		})
	}))
	defer server.Close()

	m := New(server.URL, "test-key", true)
	matches := m.CheckPrompt("fix Vercel build failure for SvelteKit project")

	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0].PatternID != "p1" {
		t.Errorf("expected pattern p1, got %s", matches[0].PatternID)
	}
}

func TestCheckPrompt_Cooldown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(matchResponse{
			Patterns: []MatchResult{
				{PatternID: "p1", Intent: "Fix build", Similarity: 0.9},
			},
			Count: 1,
		})
	}))
	defer server.Close()

	m := New(server.URL, "key", true)

	// First call: should match
	matches := m.CheckPrompt("fix Vercel build failure for SvelteKit")
	if len(matches) != 1 {
		t.Fatalf("first call: expected 1 match, got %d", len(matches))
	}

	// Mark as surfaced
	m.MarkSurfaced("p1")

	// Second call: should be filtered by cooldown
	matches = m.CheckPrompt("fix Vercel build failure again")
	if len(matches) != 0 {
		t.Errorf("second call: expected 0 matches (cooldown), got %d", len(matches))
	}
}

func TestCheckPrompt_ServerDown(t *testing.T) {
	m := New("http://localhost:1", "key", true) // nothing listening
	matches := m.CheckPrompt("this should fail silently without error output")
	if matches != nil {
		t.Errorf("expected nil on server failure, got %v", matches)
	}
}

func TestFormatCLIOutput_Empty(t *testing.T) {
	out := FormatCLIOutput(nil)
	if out != "" {
		t.Errorf("expected empty string for nil matches, got %q", out)
	}
}

func TestFormatCLIOutput_SingleMatch(t *testing.T) {
	out := FormatCLIOutput([]MatchResult{
		{PatternID: "p1", Intent: "Fix Vercel build failure", Similarity: 0.85},
	})
	if out == "" {
		t.Error("expected non-empty output for match")
	}
	if !contains(out, "Fix Vercel build") {
		t.Errorf("output should contain intent, got %q", out)
	}
}

func TestFormatCLIOutput_MultipleMatches(t *testing.T) {
	out := FormatCLIOutput([]MatchResult{
		{PatternID: "p1", Intent: "Fix build", Similarity: 0.9},
		{PatternID: "p2", Intent: "Fix deploy", Similarity: 0.8},
	})
	if !contains(out, "+1 more") {
		t.Errorf("expected '+1 more' for multiple matches, got %q", out)
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && stringContains(s, substr)
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
