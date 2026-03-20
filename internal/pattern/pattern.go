// Package pattern provides Tier 3 knowledge pattern matching.
// On session start, it checks the first user prompt against the server's
// match endpoint and surfaces recipes in the CLI if a high-confidence
// match is found.
//
// Design constraints:
// - Non-blocking: API failure is silent (no error output)
// - Max 200ms timeout (don't slow down the developer)
// - Cooldown: same pattern not surfaced to same user within 7 days
// - Opt-in via config: knowledge.tier3_enabled = true
package pattern

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Matcher checks incoming session prompts against the knowledge patterns API.
type Matcher struct {
	apiURL  string
	apiKey  string
	client  *http.Client
	enabled bool

	// cooldown tracks recently surfaced patterns to avoid repetition
	mu       sync.Mutex
	cooldown map[string]time.Time // pattern_id -> last surfaced time
}

// MatchResult represents a matched pattern from the server.
type MatchResult struct {
	PatternID  string  `json:"pattern_id"`
	Intent     string  `json:"intent"`
	Similarity float64 `json:"similarity"`
}

type matchResponse struct {
	Patterns []MatchResult `json:"patterns"`
	Count    int           `json:"count"`
}

type matchRequest struct {
	Prompt        string  `json:"prompt"`
	MinConfidence float64 `json:"min_confidence"`
}

// New creates a Matcher. If enabled is false, all calls are no-ops.
func New(apiURL, apiKey string, enabled bool) *Matcher {
	return &Matcher{
		apiURL:  strings.TrimRight(apiURL, "/"),
		apiKey:  apiKey,
		enabled: enabled,
		client: &http.Client{
			Timeout: 200 * time.Millisecond,
		},
		cooldown: make(map[string]time.Time),
	}
}

// CheckPrompt checks a prompt against the knowledge patterns API.
// Returns matched patterns, or nil if disabled/failed/no matches.
// This method NEVER returns an error — failures are silent.
func (m *Matcher) CheckPrompt(prompt string) []MatchResult {
	if !m.enabled || prompt == "" || m.apiURL == "" || m.apiKey == "" {
		return nil
	}

	// Skip very short or noise prompts
	if len(prompt) < 20 {
		return nil
	}

	body, err := json.Marshal(matchRequest{
		Prompt:        prompt,
		MinConfidence: 0.3, // server-side threshold is higher; send broad
	})
	if err != nil {
		return nil
	}

	req, err := http.NewRequest("POST", m.apiURL+"/api/v1/knowledge/match", bytes.NewReader(body))
	if err != nil {
		return nil
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+m.apiKey)

	resp, err := m.client.Do(req)
	if err != nil {
		return nil // timeout or network failure — silent
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		io.Copy(io.Discard, resp.Body)
		return nil
	}

	var result matchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil
	}

	if result.Count == 0 {
		return nil
	}

	// Filter by cooldown
	m.mu.Lock()
	defer m.mu.Unlock()

	var filtered []MatchResult
	now := time.Now()
	for _, match := range result.Patterns {
		if last, ok := m.cooldown[match.PatternID]; ok {
			if now.Sub(last) < 7*24*time.Hour {
				continue // cooldown active
			}
		}
		filtered = append(filtered, match)
	}

	return filtered
}

// MarkSurfaced records that a pattern was shown to the user,
// starting the cooldown period.
func (m *Matcher) MarkSurfaced(patternID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cooldown[patternID] = time.Now()
}

// FormatCLIOutput formats matched patterns for terminal display.
// Returns empty string if no matches — caller should not print anything.
func FormatCLIOutput(matches []MatchResult) string {
	if len(matches) == 0 {
		return ""
	}

	best := matches[0]
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("\n  \033[36m[recipe]\033[0m %s", truncate(best.Intent, 80)))

	if len(matches) > 1 {
		sb.WriteString(fmt.Sprintf(" \033[90m(+%d more)\033[0m", len(matches)-1))
	}

	sb.WriteString("\n")

	return sb.String()
}

// ReportFeedback sends feedback to the server about a surfaced pattern.
// Non-blocking — fire and forget.
func (m *Matcher) ReportFeedback(patternID, response string) {
	if !m.enabled || m.apiURL == "" || m.apiKey == "" {
		return
	}

	go func() {
		body, _ := json.Marshal(map[string]string{"response": response})
		req, err := http.NewRequest(
			"POST",
			m.apiURL+"/api/v1/knowledge/patterns/"+patternID+"/feedback",
			bytes.NewReader(body),
		)
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+m.apiKey)

		resp, err := m.client.Do(req)
		if err != nil {
			return
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()
}

// LoadCooldown loads cooldown state from disk (persists across restarts).
func (m *Matcher) LoadCooldown() {
	path := cooldownPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}

	var entries map[string]string
	if err := json.Unmarshal(data, &entries); err != nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for id, ts := range entries {
		t, err := time.Parse(time.RFC3339, ts)
		if err != nil {
			continue
		}
		// Only load entries still within cooldown
		if time.Since(t) < 7*24*time.Hour {
			m.cooldown[id] = t
		}
	}
}

// SaveCooldown persists cooldown state to disk.
func (m *Matcher) SaveCooldown() {
	m.mu.Lock()
	entries := make(map[string]string, len(m.cooldown))
	for id, t := range m.cooldown {
		if time.Since(t) < 7*24*time.Hour {
			entries[id] = t.Format(time.RFC3339)
		}
	}
	m.mu.Unlock()

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return
	}

	path := cooldownPath()
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, data, 0o644)
}

func cooldownPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "quantifai", "pattern-cooldown.json")
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
