package parser

import (
	"regexp"
	"strings"
)

// Noise prefixes — prompts starting with these are system/continuation noise,
// not real user intent. Matches the enterprise pattern_detector.py filter.
var noisePrefixes = []string{
	"[Request interrupted",
	"This session is being continued",
	"Caveat: The messages below",
	"<local-command-caveat>",
	"<system-reminder>",
	"Tool loaded",
	"[Image:",
	"Base directory:",
}

// Action verbs that indicate clear intent.
var actionVerbs = regexp.MustCompile(
	`(?i)^(fix|add|implement|create|build|update|refactor|remove|delete|move|rename|` +
		`migrate|upgrade|debug|test|write|setup|configure|deploy|optimize|improve|convert|` +
		`install|integrate|extract|split|merge|rewrite|replace|enable|disable|connect|handle)`)

// pathPattern matches file-path-like strings to strip from intent tags.
var pathPattern = regexp.MustCompile(`(?:/[\w._-]+){2,}`)

// ExtractIntentTag generates a short intent tag from a user prompt.
// Returns nil if the prompt is noise, too short, or has no clear intent.
//
// The tag format is: "verb/topic/detail" (max 100 chars).
// No file paths, code, or PII — safe for server-side storage and pattern matching.
func ExtractIntentTag(prompt string) *string {
	if prompt == "" || len(prompt) < 20 {
		return nil
	}

	// Check noise prefixes
	for _, prefix := range noisePrefixes {
		if strings.HasPrefix(prompt, prefix) {
			return nil
		}
	}

	// Take first line only (multi-line prompts: first line is usually the intent)
	firstLine := strings.SplitN(prompt, "\n", 2)[0]
	firstLine = strings.TrimSpace(firstLine)

	if len(firstLine) < 10 {
		return nil
	}

	// Strip file paths
	cleaned := pathPattern.ReplaceAllString(firstLine, "")
	cleaned = strings.TrimSpace(cleaned)

	// Strip backtick-wrapped code snippets
	cleaned = regexp.MustCompile("`[^`]+`").ReplaceAllString(cleaned, "")
	cleaned = strings.TrimSpace(cleaned)

	if len(cleaned) < 10 {
		return nil
	}

	// Build the tag: try to extract verb + key nouns
	tag := buildTag(cleaned)
	if tag == "" {
		return nil
	}

	// Cap at 100 chars
	if len(tag) > 100 {
		tag = tag[:100]
	}

	return &tag
}

// buildTag creates a slash-delimited tag from cleaned prompt text.
func buildTag(text string) string {
	lower := strings.ToLower(text)

	// Try to match an action verb
	matches := actionVerbs.FindString(lower)
	if matches == "" {
		// No clear action verb — use first 3 significant words
		words := significantWords(lower)
		if len(words) < 2 {
			return ""
		}
		if len(words) > 4 {
			words = words[:4]
		}
		return strings.Join(words, "/")
	}

	// Have a verb — extract topic words after it
	verb := matches
	rest := strings.TrimPrefix(lower, verb)
	rest = strings.TrimLeft(rest, " ")

	words := significantWords(rest)
	if len(words) == 0 {
		return verb
	}
	if len(words) > 3 {
		words = words[:3]
	}

	return verb + "/" + strings.Join(words, "/")
}

// significantWords extracts non-stopword words from text.
func significantWords(text string) []string {
	stops := map[string]bool{
		"the": true, "a": true, "an": true, "in": true, "on": true, "at": true,
		"to": true, "for": true, "of": true, "with": true, "from": true, "by": true,
		"is": true, "it": true, "that": true, "this": true, "and": true, "or": true,
		"but": true, "not": true, "so": true, "be": true, "as": true, "if": true,
		"do": true, "does": true, "did": true, "was": true, "were": true, "are": true,
		"has": true, "have": true, "had": true, "will": true, "would": true, "could": true,
		"should": true, "can": true, "may": true, "might": true, "my": true, "i": true,
		"me": true, "we": true, "our": true, "you": true, "your": true, "its": true,
		"when": true, "where": true, "how": true, "what": true, "which": true, "who": true,
	}

	wordRegex := regexp.MustCompile(`[a-z][a-z0-9_-]*`)
	allWords := wordRegex.FindAllString(text, -1)

	var result []string
	for _, w := range allWords {
		if len(w) >= 3 && !stops[w] {
			result = append(result, w)
		}
	}
	return result
}
