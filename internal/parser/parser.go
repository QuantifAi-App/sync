package parser

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
)

// homePrefix is the dash-encoded home directory used to detect and strip the
// user-home portion of encoded project paths.  Built dynamically to match the
// behavior of src/utils.py _HOME_PREFIX.
var homePrefix string

func init() {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	if home != "" {
		// Claude Code encodes paths by replacing '/' and '.' with '-'.
		encoded := strings.ReplaceAll(home, "/", "-")
		encoded = strings.ReplaceAll(encoded, ".", "-")
		if !strings.HasPrefix(encoded, "-") {
			encoded = "-" + encoded
		}
		homePrefix = encoded + "-"
	}
}

// generateMessageID produces a fallback message ID when the record's
// message.id is absent.  Matches the Python format: "msg_gen_" followed by
// 24 hex characters derived from random bytes.
func generateMessageID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		// Extremely unlikely; fall back to a fixed placeholder
		return "msg_gen_000000000000000000000000"
	}
	return "msg_gen_" + hex.EncodeToString(b)
}

// ParseRecord converts a raw JSONL line into a MessageRecord.  Handles both
// type=="assistant" (with usage/tokens) and type=="user" (prompt text only).
// All other record types return nil.
//
// This is the Go port of src/ingestion.py parse_record().
// The caller is responsible for setting identity fields (GitName, GitEmail,
// OsUsername, MachineID) on the returned record before shipping.
func ParseRecord(raw json.RawMessage, projectPath string) *MessageRecord {
	var record map[string]json.RawMessage
	if err := json.Unmarshal(raw, &record); err != nil {
		return nil
	}

	var recType string
	if v, ok := record["type"]; ok {
		if err := json.Unmarshal(v, &recType); err != nil {
			return nil
		}
	}

	switch recType {
	case "assistant":
		return parseAssistantRecord(record, projectPath)
	case "user":
		return parseUserRecord(record, projectPath)
	default:
		return nil
	}
}

// parseAssistantRecord handles type=="assistant" records with usage/token data.
func parseAssistantRecord(record map[string]json.RawMessage, projectPath string) *MessageRecord {
	// message must be a dict
	msgRaw, ok := record["message"]
	if !ok {
		return nil
	}
	var msg map[string]json.RawMessage
	if err := json.Unmarshal(msgRaw, &msg); err != nil {
		return nil
	}

	// usage must be a non-empty dict
	usageRaw, ok := msg["usage"]
	if !ok {
		return nil
	}
	var usage map[string]json.RawMessage
	if err := json.Unmarshal(usageRaw, &usage); err != nil {
		return nil
	}
	if len(usage) == 0 {
		return nil
	}

	// Extract message ID; generate one if missing
	messageID := jsonString(msg, "id")
	if messageID == "" {
		messageID = generateMessageID()
	}

	sessionID := jsonString(record, "sessionId")
	if sessionID == "" {
		sessionID = "unknown"
	}

	timestamp := jsonString(record, "timestamp")
	if timestamp == "" {
		return nil
	}

	model := jsonString(msg, "model")
	if model == "" {
		model = "unknown"
	}

	// Token fields
	inputTokens := jsonInt(usage, "input_tokens")
	outputTokens := jsonInt(usage, "output_tokens")
	cacheRead := jsonInt(usage, "cache_read_input_tokens")
	cacheCreate := jsonInt(usage, "cache_creation_input_tokens")

	// Cost calculation using existing pricing.go
	pricing := GetPricing(model)
	estCost := CalculateCost(pricing, inputTokens, outputTokens, cacheRead, cacheCreate)

	// Content block extraction
	var contentBlocks []json.RawMessage
	if cb, ok := msg["content"]; ok {
		_ = json.Unmarshal(cb, &contentBlocks)
	}
	toolNames := ExtractToolNames(contentBlocks)
	filePaths := ExtractFilePaths(contentBlocks)

	// Decode project path into display name
	displayName := DecodeProjectPath(projectPath)

	// Optional top-level metadata
	cwdVal := jsonStringPtr(record, "cwd")
	versionVal := jsonStringPtr(record, "version")
	gitBranchVal := jsonStringPtr(record, "gitBranch")

	return &MessageRecord{
		MessageID:                messageID,
		SessionID:                sessionID,
		Timestamp:                timestamp,
		Model:                    model,
		InputTokens:              inputTokens,
		OutputTokens:             outputTokens,
		CacheReadInputTokens:     cacheRead,
		CacheCreationInputTokens: cacheCreate,
		EstCost:                  estCost,
		ProjectPath:              projectPath,
		DisplayName:              displayName,
		ClaudeVersion:            versionVal,
		GitBranch:                gitBranchVal,
		Cwd:                      cwdVal,
		ToolNames:                toolNames,
		FilePaths:                filePaths,
		RecordType:               "assistant",
	}
}

// parseUserRecord handles type=="user" records (prompts with no token data).
func parseUserRecord(record map[string]json.RawMessage, projectPath string) *MessageRecord {
	sessionID := jsonString(record, "sessionId")
	if sessionID == "" {
		sessionID = "unknown"
	}

	timestamp := jsonString(record, "timestamp")
	if timestamp == "" {
		return nil
	}

	// Use uuid field as message ID; generate one if missing
	messageID := jsonString(record, "uuid")
	if messageID == "" {
		messageID = generateMessageID()
	}

	// Extract prompt text from message.content
	var contentText *string
	var contentLength *int
	if msgRaw, ok := record["message"]; ok {
		var msg map[string]json.RawMessage
		if err := json.Unmarshal(msgRaw, &msg); err == nil {
			if contentRaw, ok := msg["content"]; ok {
				var text string
				if err := json.Unmarshal(contentRaw, &text); err == nil && text != "" {
					contentText = &text
					l := len(text)
					contentLength = &l
				}
			}
		}
	}

	displayName := DecodeProjectPath(projectPath)
	cwdVal := jsonStringPtr(record, "cwd")
	versionVal := jsonStringPtr(record, "version")
	gitBranchVal := jsonStringPtr(record, "gitBranch")

	return &MessageRecord{
		MessageID:     messageID,
		SessionID:     sessionID,
		Timestamp:     timestamp,
		EstCost:       0,
		ProjectPath:   projectPath,
		DisplayName:   displayName,
		ClaudeVersion: versionVal,
		GitBranch:     gitBranchVal,
		Cwd:           cwdVal,
		RecordType:    "user",
		ContentText:   contentText,
		ContentLength: contentLength,
	}
}

// ExtractToolNames extracts tool names from content blocks.  It iterates
// through content blocks looking for type=="tool_use" and returns the "name"
// field from each.  Port of src/ingestion.py extract_tool_names().
func ExtractToolNames(contentBlocks []json.RawMessage) []string {
	var names []string
	for _, blockRaw := range contentBlocks {
		var block map[string]json.RawMessage
		if err := json.Unmarshal(blockRaw, &block); err != nil {
			continue
		}
		var blockType string
		if v, ok := block["type"]; ok {
			_ = json.Unmarshal(v, &blockType)
		}
		if blockType != "tool_use" {
			continue
		}
		var name string
		if v, ok := block["name"]; ok {
			_ = json.Unmarshal(v, &name)
		}
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

// ExtractFilePaths extracts file paths from tool_use content blocks by
// checking the "input" dict for common path keys (file_path, path, file,
// filename).  Port of src/ingestion.py extract_file_paths().
func ExtractFilePaths(contentBlocks []json.RawMessage) []string {
	pathKeys := []string{"file_path", "path", "file", "filename"}
	var paths []string
	for _, blockRaw := range contentBlocks {
		var block map[string]json.RawMessage
		if err := json.Unmarshal(blockRaw, &block); err != nil {
			continue
		}
		var blockType string
		if v, ok := block["type"]; ok {
			_ = json.Unmarshal(v, &blockType)
		}
		if blockType != "tool_use" {
			continue
		}
		inputRaw, ok := block["input"]
		if !ok {
			continue
		}
		var input map[string]json.RawMessage
		if err := json.Unmarshal(inputRaw, &input); err != nil {
			continue
		}
		for _, key := range pathKeys {
			valRaw, ok := input[key]
			if !ok {
				continue
			}
			var val string
			if err := json.Unmarshal(valRaw, &val); err != nil {
				continue
			}
			if val != "" {
				paths = append(paths, val)
			}
		}
	}
	return paths
}

// DecodeProjectPath converts a dash-encoded project directory name back to a
// human-readable path with a ~/ prefix.  Port of src/utils.py
// decode_project_path().
//
// If the encoded string starts with the current user's home directory prefix,
// that prefix is stripped and remaining dashes are converted to slashes,
// producing "~/rest/of/path".  Otherwise, the leading dash is stripped and
// all dashes become slashes.
func DecodeProjectPath(encoded string) string {
	if homePrefix != "" && strings.HasPrefix(encoded, homePrefix) {
		remainder := encoded[len(homePrefix):]
		return "~/" + strings.ReplaceAll(remainder, "-", "/")
	}

	// Fallback: strip leading dash, convert all dashes to slashes
	stripped := strings.TrimLeft(encoded, "-")
	return strings.ReplaceAll(stripped, "-", "/")
}

// --- helpers for safe JSON field extraction ---

// jsonString extracts a string value from a map of raw JSON messages.
func jsonString(m map[string]json.RawMessage, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return ""
	}
	return s
}

// jsonStringPtr extracts an optional string value, returning nil if the key is
// absent or the value is not a string.
func jsonStringPtr(m map[string]json.RawMessage, key string) *string {
	v, ok := m[key]
	if !ok {
		return nil
	}
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return nil
	}
	if s == "" {
		return nil
	}
	return &s
}

// jsonInt extracts an integer value, defaulting to 0 if absent or invalid.
func jsonInt(m map[string]json.RawMessage, key string) int {
	v, ok := m[key]
	if !ok {
		return 0
	}
	var n float64
	if err := json.Unmarshal(v, &n); err != nil {
		return 0
	}
	return int(n)
}
