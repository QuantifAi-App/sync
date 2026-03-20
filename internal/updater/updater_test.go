package updater

import (
	"testing"

	"github.com/quantifai/sync/internal/logger"
)

// newDiscardLogger creates a logger that discards output (debug level
// so all messages pass the filter, but written to stderr which is
// effectively discarded during tests).
func newDiscardLogger() *logger.Logger {
	l, _ := logger.New(logger.LevelDebug, "")
	return l
}

// ---------------------------------------------------------------------------
// Test: Auto-update is disabled by default (no-op when auto_update=false)
// ---------------------------------------------------------------------------

func TestAutoUpdateDisabledByDefault(t *testing.T) {
	// When auto_update is false, NewUpdater should return a NoopUpdater
	u := NewUpdater(false, "1.0.0", "stable", "quantifai-app/sync", "24h", newDiscardLogger())

	// Verify the returned type is NoopUpdater
	noop, ok := u.(*NoopUpdater)
	if !ok {
		t.Fatalf("expected *NoopUpdater when auto_update=false, got %T", u)
	}

	// Verify CheckAndApply is a no-op (returns false, nil)
	applied, err := noop.CheckAndApply(nil)
	if applied {
		t.Error("NoopUpdater.CheckAndApply returned applied=true, want false")
	}
	if err != nil {
		t.Errorf("NoopUpdater.CheckAndApply returned error: %v", err)
	}

	// Verify Run returns immediately (does not block)
	done := make(chan struct{})
	go func() {
		noop.Run(nil)
		close(done)
	}()

	select {
	case <-done:
		// Run returned immediately as expected
	default:
		// Give it a very brief moment since goroutine scheduling
		// is not instantaneous
		<-done
	}
}

func TestAutoUpdateEnabledReturnsGithubUpdater(t *testing.T) {
	u := NewUpdater(true, "1.0.0", "stable", "quantifai-app/sync", "24h", newDiscardLogger())
	if _, ok := u.(*GithubUpdater); !ok {
		t.Fatalf("expected *GithubUpdater when auto_update=true, got %T", u)
	}
}

func TestNewUpdaterDefaultRepo(t *testing.T) {
	u := NewUpdater(true, "1.0.0", "stable", "", "24h", newDiscardLogger())
	gu, ok := u.(*GithubUpdater)
	if !ok {
		t.Fatalf("expected *GithubUpdater, got %T", u)
	}
	if gu.repo != "quantifai-app/sync" {
		t.Errorf("repo: got %q, want %q", gu.repo, "quantifai-app/sync")
	}
}

func TestNewUpdaterCustomInterval(t *testing.T) {
	u := NewUpdater(true, "1.0.0", "stable", "quantifai-app/sync", "12h", newDiscardLogger())
	gu := u.(*GithubUpdater)
	if gu.interval.Hours() != 12 {
		t.Errorf("interval: got %v, want 12h", gu.interval)
	}
}

func TestNewUpdaterInvalidIntervalUsesDefault(t *testing.T) {
	u := NewUpdater(true, "1.0.0", "stable", "quantifai-app/sync", "not-a-duration", newDiscardLogger())
	gu := u.(*GithubUpdater)
	if gu.interval != defaultCheckInterval {
		t.Errorf("interval: got %v, want %v", gu.interval, defaultCheckInterval)
	}
}
