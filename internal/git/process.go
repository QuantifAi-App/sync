package git

import (
	"os/exec"
	"strings"
)

// processSignature maps a process name substring to a normalized
// AI tool name.
type processSignature struct {
	Name string // substring to match in process list
	Tool string // normalized tool name
}

// knownProcesses lists process name patterns for AI coding tools.
// Order matters: more specific patterns should come first.
var knownProcesses = []processSignature{
	{"Cursor Helper", "cursor"},
	{"Cursor", "cursor"},
	{"copilot-agent", "copilot"},
	{"GitHub Copilot", "copilot"},
	{"claude", "claude"},
	{"windsurf", "windsurf"},
	{"aider", "aider"},
}

// DetectAIProcesses scans running processes for known AI tool
// signatures.  It returns a deduplicated list of detected tool names.
// The scan must complete quickly (<50ms) since it runs in the
// post-commit hook's 100ms budget.
func DetectAIProcesses() []string {
	// Use ps with minimal output format to keep it fast
	cmd := exec.Command("ps", "axo", "comm")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	psOutput := string(out)
	seen := make(map[string]bool)
	var tools []string

	for _, proc := range knownProcesses {
		if seen[proc.Tool] {
			continue
		}
		if strings.Contains(psOutput, proc.Name) {
			seen[proc.Tool] = true
			tools = append(tools, proc.Tool)
		}
	}

	return tools
}
