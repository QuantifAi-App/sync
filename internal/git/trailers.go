package git

import (
	"strings"
)

// aiPattern maps a substring found in a trailer value to its
// normalized AI tool name.
type aiPattern struct {
	Pattern string // substring to match (case-insensitive)
	Tool    string // normalized tool name
}

// knownAIPatterns is the list of recognized AI co-author signatures.
var knownAIPatterns = []aiPattern{
	{"anthropic", "claude"},
	{"github.com/copilot", "copilot"},
	{"openai", "openai"},
	{"cursor", "cursor"},
	{"google", "gemini"},
	{"codeium", "windsurf"},
	{"aider", "aider"},
}

// trailerPrefixes are the git trailer keys that indicate AI assistance.
var trailerPrefixes = []string{
	"co-authored-by:",
	"generated-by:",
	"ai-assisted:",
}

// ParseAITrailers scans a commit message for AI co-author trailers and
// returns the matching trailer lines and the detected tool name.  If
// multiple tools are detected, the first match wins.
func ParseAITrailers(commitMessage string) (trailers []string, tool string) {
	lines := strings.Split(commitMessage, "\n")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)

		for _, prefix := range trailerPrefixes {
			if !strings.HasPrefix(lower, prefix) {
				continue
			}

			trailers = append(trailers, trimmed)

			// Try to identify the AI tool from the trailer value
			if tool == "" {
				value := strings.ToLower(trimmed)
				for _, p := range knownAIPatterns {
					if strings.Contains(value, p.Pattern) {
						tool = p.Tool
						break
					}
				}
			}
			break // matched a prefix, no need to check others for this line
		}
	}

	return trailers, tool
}
