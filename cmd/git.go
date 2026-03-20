package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/quantifai/sync/internal/config"
	"github.com/quantifai/sync/internal/git"
)

// RunGitInit installs the quantifai post-commit hook in the given
// repository path.  It resolves the path to absolute, installs the
// hook, and prints the result.
func RunGitInit(repoPath string) int {
	absPath, err := filepath.Abs(repoPath)
	if err != nil {
		fmt.Printf("error: could not resolve path %q: %v\n", repoPath, err)
		return 1
	}

	if err := git.InstallHook(absPath); err != nil {
		fmt.Printf("error: %v\n", err)
		return 1
	}

	if git.IsHookInstalled(absPath) {
		fmt.Printf("quantifai hook installed in %s\n", absPath)
	} else {
		fmt.Printf("hook was already installed in %s\n", absPath)
	}
	return 0
}

// RunGitRemove removes the quantifai post-commit hook from the given
// repository path and restores any pre-existing hook.
func RunGitRemove(repoPath string) int {
	absPath, err := filepath.Abs(repoPath)
	if err != nil {
		fmt.Printf("error: could not resolve path %q: %v\n", repoPath, err)
		return 1
	}

	if err := git.RemoveHook(absPath); err != nil {
		fmt.Printf("error: %v\n", err)
		return 1
	}

	fmt.Printf("quantifai hook removed from %s\n", absPath)
	return 0
}

// RunGitList prints the hook installation status of all configured
// repositories.  If no repos are configured, it prints a hint.
func RunGitList() int {
	cfg, err := config.Load("", "")
	if err != nil {
		fmt.Printf("error loading config: %v\n", err)
		return 1
	}

	if len(cfg.GitRepos) == 0 {
		fmt.Println("no repositories configured in git_repos")
		fmt.Println("tip: use 'quantifai-sync git init <path>' to set up a repository")
		return 0
	}

	statuses, err := git.ListHookRepos(cfg.GitRepos)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return 1
	}

	for _, s := range statuses {
		status := "not installed"
		if s.HookInstalled {
			status = "installed"
		}
		fmt.Printf("  %s  [%s]\n", s.Path, status)
	}

	return 0
}

// RunGitHookPostCommit is invoked by the git post-commit hook script.
// It captures commit metadata, detects AI tool usage via trailers and
// process scanning, and queues the event for later upload.  The entire
// function must complete within ~100ms to avoid slowing git commits.
func RunGitHookPostCommit() int {
	// Load config for git settings
	cfg, _ := config.Load("", "")
	if !cfg.GitEnabled {
		return 0
	}

	// Capture commit metadata from git
	event, err := git.CaptureCommit()
	if err != nil {
		// Silently fail -- we must never block a user's commit
		return 0
	}

	// Detect AI tools via running processes (if enabled)
	if cfg.GitProcessScan {
		processes := git.DetectAIProcesses()
		if len(processes) > 0 && event.AIToolDetected == "" {
			event.AIToolDetected = processes[0]
		}
	}

	// Set AI confidence based on available signals
	event.AIConfidence = computeConfidence(event)

	// Queue for later batch upload (no network I/O)
	if err := git.QueueCommitEvent(event); err != nil {
		// Silently fail -- never block the commit
		return 0
	}

	return 0
}

// computeConfidence determines the confidence level of AI involvement
// based on the available signals: trailers, process detection, and
// tool identification.
func computeConfidence(event *git.CommitEvent) string {
	hasTrailer := len(event.AITrailers) > 0
	hasTool := event.AIToolDetected != ""

	switch {
	case hasTrailer && hasTool:
		return "high"
	case hasTrailer:
		return "high"
	case hasTool:
		return "medium"
	default:
		return "none"
	}
}

// mergeToolSignals is a helper used by tests -- the real logic is
// inline in RunGitHookPostCommit above.  Keeping it exported allows
// test coverage of the confidence computation.
func mergeToolSignals(trailerTool string, processList []string) string {
	if trailerTool != "" {
		return trailerTool
	}
	if len(processList) > 0 {
		return strings.Join(processList, ",")
	}
	return ""
}
