package git

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// hookScript is the post-commit hook installed by quantifai-sync.
// It chains to any pre-existing hook and then invokes the
// hook-post-commit subcommand to capture commit metadata.
const hookScript = `#!/bin/sh
# Managed by quantifai-sync — do not edit
# Chain to pre-existing hook if present
if [ -x "$(dirname "$0")/post-commit.pre-quantifai" ]; then
    "$(dirname "$0")/post-commit.pre-quantifai"
fi
quantifai-sync git hook-post-commit
`

// hookMarker identifies our hook in an already-installed script.
const hookMarker = "Managed by quantifai-sync"

// markerFile is placed in the repo root to indicate quantifai tracking.
const markerFile = ".quantifai"

// RepoStatus holds the hook installation state for a single repository.
type RepoStatus struct {
	Path          string `json:"path"`
	HookInstalled bool   `json:"hook_installed"`
	MarkerPresent bool   `json:"marker_present"`
}

// InstallHook installs the quantifai post-commit hook into the
// repository at repoPath.  It is idempotent: calling it on a repo
// that already has the hook is a no-op.  Any existing post-commit
// hook is preserved by renaming it to post-commit.pre-quantifai.
func InstallHook(repoPath string) error {
	repoPath, err := filepath.Abs(repoPath)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	gitDir := filepath.Join(repoPath, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		return fmt.Errorf("%s is not a git repository (no .git directory)", repoPath)
	}

	hooksDir := filepath.Join(gitDir, "hooks")
	hookPath := filepath.Join(hooksDir, "post-commit")
	backupPath := filepath.Join(hooksDir, "post-commit.pre-quantifai")

	// Idempotency check: already installed
	if IsHookInstalled(repoPath) {
		return nil
	}

	// Ensure hooks directory exists
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		return fmt.Errorf("create hooks dir: %w", err)
	}

	// Backup existing hook if present
	if info, err := os.Stat(hookPath); err == nil && info.Mode().IsRegular() {
		if err := os.Rename(hookPath, backupPath); err != nil {
			return fmt.Errorf("backup existing hook: %w", err)
		}
	}

	// Write our hook script
	if err := os.WriteFile(hookPath, []byte(hookScript), 0755); err != nil {
		return fmt.Errorf("write hook: %w", err)
	}

	// Create .quantifai marker file in repo root
	markerPath := filepath.Join(repoPath, markerFile)
	if err := os.WriteFile(markerPath, []byte("# quantifai-sync tracking enabled\n"), 0644); err != nil {
		return fmt.Errorf("write marker: %w", err)
	}

	return nil
}

// RemoveHook removes the quantifai post-commit hook and restores any
// original hook that was backed up during installation.
func RemoveHook(repoPath string) error {
	repoPath, err := filepath.Abs(repoPath)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	hooksDir := filepath.Join(repoPath, ".git", "hooks")
	hookPath := filepath.Join(hooksDir, "post-commit")
	backupPath := filepath.Join(hooksDir, "post-commit.pre-quantifai")

	// Only remove if it's our hook
	if data, err := os.ReadFile(hookPath); err == nil {
		if strings.Contains(string(data), hookMarker) {
			if err := os.Remove(hookPath); err != nil {
				return fmt.Errorf("remove hook: %w", err)
			}
		}
	}

	// Restore original hook if backup exists
	if _, err := os.Stat(backupPath); err == nil {
		if err := os.Rename(backupPath, hookPath); err != nil {
			return fmt.Errorf("restore original hook: %w", err)
		}
	}

	// Remove marker file
	markerPath := filepath.Join(repoPath, markerFile)
	os.Remove(markerPath) // best effort

	return nil
}

// IsHookInstalled returns true if the quantifai post-commit hook is
// installed in the given repository.
func IsHookInstalled(repoPath string) bool {
	repoPath, _ = filepath.Abs(repoPath)
	hookPath := filepath.Join(repoPath, ".git", "hooks", "post-commit")

	data, err := os.ReadFile(hookPath)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), hookMarker)
}

// ListHookRepos checks the installation status of each configured
// repository path and returns a RepoStatus for each.
func ListHookRepos(configRepos []string) ([]RepoStatus, error) {
	statuses := make([]RepoStatus, 0, len(configRepos))

	for _, repo := range configRepos {
		absPath, err := filepath.Abs(repo)
		if err != nil {
			absPath = repo
		}

		status := RepoStatus{
			Path:          absPath,
			HookInstalled: IsHookInstalled(absPath),
		}

		markerPath := filepath.Join(absPath, markerFile)
		if _, err := os.Stat(markerPath); err == nil {
			status.MarkerPresent = true
		}

		statuses = append(statuses, status)
	}

	return statuses, nil
}
