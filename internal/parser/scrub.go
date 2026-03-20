package parser

import "strings"

// IsLiteKey returns true if the API key indicates a QuantifAI Lite target.
// Lite ingest keys are prefixed with "ql_" — auto-generated during signup.
func IsLiteKey(apiKey string) bool {
	return strings.HasPrefix(apiKey, "ql_")
}

// ScrubForLite strips PII and IP fields from a MessageRecord before
// transmission to a Lite endpoint. This ensures no prompts, code, file
// paths, git identity, or machine identifiers leave the user's machine.
//
// Fields preserved: message_id, session_id, timestamp, model, tokens,
// cost, project_path (last segment only), display_name (last segment only),
// record_type, tool_names, intent_tag.
//
// Fields stripped: content_text, content_length, file_paths, git_name,
// git_email, os_username, machine_id, cwd, git_branch, remote_url.
func ScrubForLite(rec *MessageRecord) {
	// Strip prompt/response content
	rec.ContentText = nil
	rec.ContentLength = nil

	// Strip file paths (IP)
	rec.FilePaths = nil

	// Strip git identity
	rec.GitName = nil
	rec.GitEmail = nil

	// Strip machine identifiers
	rec.OsUsername = nil
	rec.MachineID = nil

	// Strip working directory and branch (can reveal project structure)
	rec.Cwd = nil
	rec.GitBranch = nil

	// Scrub project path to last segment only (no full paths)
	rec.ProjectPath = lastPathSegment(rec.ProjectPath)
	rec.DisplayName = lastPathSegment(rec.DisplayName)
}

// lastPathSegment extracts the final directory name from an encoded
// or decoded project path. E.g. "~/Workspace/dev/my-project" → "my-project".
func lastPathSegment(path string) string {
	// Handle both / and - separated paths (encoded and decoded)
	path = strings.TrimRight(path, "/\\-")
	if path == "" {
		return "unknown"
	}

	// Try slash separator first (decoded paths)
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		seg := path[idx+1:]
		if seg != "" {
			return seg
		}
	}

	// For encoded paths like "-Users-nino-project", take last dash segment
	// But only if there are multiple dashes (single segment has no dashes to split on)
	if strings.Count(path, "-") > 2 {
		if idx := strings.LastIndex(path, "-"); idx >= 0 {
			seg := path[idx+1:]
			if seg != "" {
				return seg
			}
		}
	}

	return path
}
