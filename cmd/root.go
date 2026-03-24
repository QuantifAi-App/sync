// Package cmd provides CLI subcommands for the quantifai-sync binary.
// The root command starts the agent pipeline: watcher -> reader ->
// parser -> buffer -> sender.  Subcommands (install, uninstall, version,
// healthcheck) are dispatched based on os.Args.
package cmd

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/quantifai/sync/internal/config"
	"github.com/quantifai/sync/internal/editor"
	gitpkg "github.com/quantifai/sync/internal/git"
	"github.com/quantifai/sync/internal/health"
	"github.com/quantifai/sync/internal/logger"
	"github.com/quantifai/sync/internal/parser"
	"github.com/quantifai/sync/internal/reader"
	"github.com/quantifai/sync/internal/sender"
	"github.com/quantifai/sync/internal/state"
	"github.com/quantifai/sync/internal/scanner"
	"github.com/quantifai/sync/internal/updater"
)

// shutdownTimeout is the maximum time allowed for a graceful shutdown
// (flush buffered records, persist state, close watcher).
const shutdownTimeout = 30 * time.Second

// Execute is the main CLI entrypoint.  It parses os.Args to dispatch
// to the appropriate subcommand or runs the agent pipeline by default.
func Execute() int {
	if len(os.Args) < 2 {
		return runAgent()
	}

	switch os.Args[1] {
	case "version":
		return RunVersion()

	case "install":
		fs := flag.NewFlagSet("install", flag.ExitOnError)
		apiKey := fs.String("api-key", "", "API key for non-interactive install")
		fs.Parse(os.Args[2:])
		return RunInstall(*apiKey)

	case "uninstall":
		return RunUninstall()

	case "healthcheck":
		// Load config to get health port
		cfg, _ := config.Load("", "")
		return RunHealthcheck(cfg.HealthPort)

	case "run":
		// Explicit "run" subcommand (used by service managers)
		return runAgent()

	case "tray":
		return RunTray()

	case "git":
		if len(os.Args) < 3 {
			fmt.Fprintf(os.Stderr, "usage: quantifai-sync git [init|remove|list|hook-post-commit]\n")
			return 1
		}
		switch os.Args[2] {
		case "init":
			repoPath := "."
			if len(os.Args) > 3 {
				repoPath = os.Args[3]
			}
			return RunGitInit(repoPath)
		case "remove":
			repoPath := "."
			if len(os.Args) > 3 {
				repoPath = os.Args[3]
			}
			return RunGitRemove(repoPath)
		case "list":
			return RunGitList()
		case "hook-post-commit":
			return RunGitHookPostCommit()
		default:
			fmt.Fprintf(os.Stderr, "unknown git command: %s\n", os.Args[2])
			return 1
		}

	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		fmt.Fprintf(os.Stderr, "usage: quantifai-sync [version|install|uninstall|healthcheck|run|tray|git]\n")
		return 1
	}
}

// runAgent starts the full agent pipeline with graceful shutdown support.
func runAgent() int {
	// Load configuration
	cfg, err := config.Load("", "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		return 1
	}

	if err := config.Validate(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "invalid config: %v\n", err)
		return 1
	}

	if !cfg.SyncEnabled {
		fmt.Fprintf(os.Stderr, "sync_enabled is false; exiting\n")
		return 0
	}

	// Initialize logger
	log, err := logger.New(logger.ParseLevel(cfg.LogLevel), cfg.LogFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		return 1
	}
	defer log.Close()

	log.Info("starting quantifai-sync", map[string]any{
		"version":   Version,
		"watch_dir": cfg.WatchDir,
	})

	// Start health server
	healthState := health.NewHealthState(Version)
	healthSrv := health.NewServer(cfg.HealthPort, healthState)
	// Register editor events endpoint for VS Code extension
	healthSrv.RegisterHandler("/api/v1/editor-events", editor.HandleEditorEvents(log))
	go healthSrv.ListenAndServe()
	defer healthSrv.Shutdown(context.Background())

	// Start background auto-updater
	u := updater.NewUpdater(cfg.AutoUpdate, Version, cfg.UpdateChannel, cfg.UpdateRepo, cfg.UpdateCheckInterval, log)

	// Collect identity once at startup
	identity := parser.CollectIdentity()

	// Initialize state manager
	stateMgr, err := state.NewManager(cfg.StateFile)
	if err != nil {
		log.Error("failed to load state", map[string]any{"error": err.Error()})
		return 1
	}
	pruned := stateMgr.Prune()
	if pruned > 0 {
		log.Info("pruned stale state entries", map[string]any{"count": pruned})
	}

	// Initialize sender
	snd, err := sender.New(cfg.APIURL, cfg.APIKey, log)
	if err != nil {
		log.Error("failed to create sender", map[string]any{"error": err.Error()})
		return 1
	}

	// Set up context with signal-based cancellation for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the updater background loop (no-op when auto_update=false)
	go u.Run(ctx)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	// Initialize buffer with sender flush function.
	// State is persisted only after successful HTTP acknowledgment
	// to guarantee at-least-once delivery.
	flushInterval := time.Duration(cfg.FlushInterval) * time.Second
	buf := sender.NewBuffer(cfg.BatchSize, flushInterval, func(fctx context.Context, records []parser.MessageRecord) bool {
		ok := snd.Send(fctx, records)
		if ok {
			healthState.SetLastSyncTime(time.Now())
			if err := stateMgr.Save(); err != nil {
				log.Warn("failed to save state after send", map[string]any{"error": err.Error()})
			}
		}

		// Flush queued commit events alongside session data
		if cfg.GitEnabled && cfg.APIURL != "" && cfg.APIKey != "" {
			if n := gitpkg.FlushCommitQueue(cfg.APIURL, cfg.APIKey, log); n > 0 {
				log.Debug("flushed commit events", map[string]any{"count": n})
			}
		}

		// Flush queued editor events from VS Code extension
		if cfg.APIURL != "" && cfg.APIKey != "" {
			if n := editor.FlushEditorQueue(cfg.APIURL, cfg.APIKey, log); n > 0 {
				log.Debug("flushed editor events", map[string]any{"count": n})
			}
		}

		return ok
	})

	liteMode := parser.IsLiteKey(cfg.APIKey)
	scanInterval := time.Duration(cfg.FlushInterval) * time.Second

	log.Info("pipeline started", map[string]any{
		"watch_dir":      cfg.WatchDir,
		"batch_size":     cfg.BatchSize,
		"scan_interval":  cfg.FlushInterval,
		"health_port":    cfg.HealthPort,
	})

	healthState.SetStatus(health.StatusOK)

	// Poll-based main loop: scan → read → parse → buffer → flush
	// Every cycle walks the directory, finds files with new data,
	// reads new bytes, parses records, and flushes the batch.
	// First cycle processes all historical files (backfill).
	scanAndProcess := func() {
		files := scanner.Scan(cfg.WatchDir, stateMgr)
		if len(files) == 0 {
			return
		}

		for _, f := range files {
			processFile(ctx, f.Path, f.ByteOffset, stateMgr, identity, buf, log, cfg.IntentTagEnabled, liteMode)
		}

		// Flush after processing all files in this cycle
		buf.Flush(ctx)
		healthState.SetRecordsBuffered(buf.Len())

		total, pending := scanner.Count(cfg.WatchDir, stateMgr)
		healthState.SetFilesTracked(total)
		if pending > 0 {
			log.Debug("scan cycle complete", map[string]any{
				"files_scanned": len(files),
				"total_files":   total,
				"pending":       pending,
			})
		}
	}

	// Run first scan immediately (handles backfill)
	scanAndProcess()

	ticker := time.NewTicker(scanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			scanAndProcess()

		case sig := <-sigCh:
			log.Info("received signal, initiating graceful shutdown", map[string]any{
				"signal": sig.String(),
			})
			cancel()
			goto shutdown
		}
	}

shutdown:
	return gracefulShutdown(buf, stateMgr, log)
}

// processFile reads new lines from a JSONL file starting at the given
// byte offset, parses them into MessageRecord structs, and adds them to
// the send buffer.
func processFile(
	ctx context.Context,
	filePath string,
	byteOffset int64,
	stateMgr *state.Manager,
	identity *parser.Identity,
	buf *sender.Buffer,
	log *logger.Logger,
	intentTagEnabled bool,
	liteMode bool,
) {
	result, err := reader.ReadFromOffset(filePath, byteOffset)
	if err != nil {
		log.Warn("failed to read file", map[string]any{
			"path":  filePath,
			"error": err.Error(),
		})
		return
	}

	if len(result.Lines) == 0 {
		return
	}

	// Derive the project path from the file's parent directory name
	projectPath := projectPathFromFile(filePath)

	parsed := 0
	var sessionIntentTag *string // first user prompt's intent tag for this batch
	for _, line := range result.Lines {
		rec := parser.ParseRecord(json.RawMessage(line), projectPath)
		if rec == nil {
			continue
		}

		// Enrich with identity fields
		rec.GitName = identity.GitName
		rec.GitEmail = identity.GitEmail
		rec.OsUsername = identity.OsUsername
		rec.MachineID = identity.MachineID

		// Extract intent tag from first user prompt (opt-in)
		if intentTagEnabled && rec.RecordType == "user" && rec.ContentText != nil && sessionIntentTag == nil {
			sessionIntentTag = parser.ExtractIntentTag(*rec.ContentText)
		}

		// Stamp intent tag on all records in the batch
		if sessionIntentTag != nil {
			rec.IntentTag = sessionIntentTag
		}

		// Lite mode: strip PII before transmission (key prefix "ql_")
		if liteMode {
			parser.ScrubForLite(rec)
		}

		buf.Add(ctx, *rec)
		parsed++
	}

	// Update in-memory state with new byte offset.
	// Disk persistence is deferred to the buffer's flush callback
	// (after successful HTTP acknowledgment) to ensure at-least-once delivery.
	stateMgr.Set(filePath, state.FileState{
		ByteOffset: result.NewOffset,
	})

	if parsed > 0 {
		log.Debug("processed file event", map[string]any{
			"path":        filePath,
			"lines":       len(result.Lines),
			"records":     parsed,
			"new_offset":  result.NewOffset,
		})
	}
}

// projectPathFromFile extracts the project directory name (the
// dash-encoded path) from a JSONL file's absolute path.  Claude Code
// stores sessions at ~/.claude/projects/<encoded-path>/<session>.jsonl,
// so the project path is the parent directory name.
func projectPathFromFile(filePath string) string {
	// Walk up from file to get the parent directory name
	dir := filePath
	for i := len(dir) - 1; i >= 0; i-- {
		if dir[i] == '/' || dir[i] == '\\' {
			dir = dir[:i]
			break
		}
	}
	// Now extract the last component of dir
	for i := len(dir) - 1; i >= 0; i-- {
		if dir[i] == '/' || dir[i] == '\\' {
			return dir[i+1:]
		}
	}
	return dir
}

// gracefulShutdown flushes remaining records and persists state.
// Returns exit code 0 on success, 1 on timeout.
func gracefulShutdown(buf *sender.Buffer, stateMgr *state.Manager, log *logger.Logger) int {
	done := make(chan struct{})
	go func() {
		defer close(done)

		// Flush any remaining buffered records
		log.Info("flushing buffered records", map[string]any{"buffered": buf.Len()})
		buf.Flush(context.Background())

		// Persist state
		if err := stateMgr.Save(); err != nil {
			log.Error("failed to save state during shutdown", map[string]any{"error": err.Error()})
		}

		log.Info("shutdown complete", nil)
	}()

	select {
	case <-done:
		return 0
	case <-time.After(shutdownTimeout):
		log.Error("shutdown timed out", map[string]any{"timeout_seconds": shutdownTimeout.Seconds()})
		return 1
	}
}
