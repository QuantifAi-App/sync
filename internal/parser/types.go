package parser

// MessageRecord represents a single message record shipped to the central API.
// Fields match server/models.py MessageRecord.  JSON tags use the same
// snake_case names the Pydantic model expects so the Go shipper produces
// wire-compatible payloads with zero server-side changes.
type MessageRecord struct {
	MessageID                string   `json:"message_id"`
	SessionID                string   `json:"session_id"`
	Timestamp                string   `json:"timestamp"`
	Model                    string   `json:"model,omitempty"`
	InputTokens              int      `json:"input_tokens"`
	OutputTokens             int      `json:"output_tokens"`
	CacheReadInputTokens     int      `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int      `json:"cache_creation_input_tokens"`
	EstCost                  float64  `json:"est_cost"`
	ProjectPath              string   `json:"project_path"`
	DisplayName              string   `json:"display_name"`
	GitName                  *string  `json:"git_name"`
	GitEmail                 *string  `json:"git_email"`
	OsUsername               *string  `json:"os_username"`
	MachineID                *string  `json:"machine_id"`
	ClaudeVersion            *string  `json:"claude_version"`
	GitBranch                *string  `json:"git_branch"`
	Cwd                      *string  `json:"cwd"`
	ToolNames                []string `json:"tool_names"`
	FilePaths                []string `json:"file_paths"`
	RecordType               string   `json:"record_type"`
	ContentText              *string  `json:"content_text,omitempty"`
	ContentLength            *int     `json:"content_length,omitempty"`
	IntentTag                *string  `json:"intent_tag,omitempty"`
}

// IngestRequest is the batch envelope sent to POST /api/v1/ingest.
// The server enforces a maximum of 10,000 records per request.
type IngestRequest struct {
	Records []MessageRecord `json:"records"`
}

// IngestResponse is the server acknowledgment after a successful ingest.
type IngestResponse struct {
	Accepted int `json:"accepted"`
	Errors   int `json:"errors"`
}
