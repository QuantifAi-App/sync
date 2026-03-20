package state

import (
	"os"
	"path/filepath"
	"testing"
)

// TestStateAtomicWrite verifies that Save() uses the write-tmp-rename
// pattern: the final state file exists, and the .tmp file is cleaned up.
func TestStateAtomicWrite(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "shipper-state.json")

	m, err := NewManager(statePath)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	m.Set("/path/to/file.jsonl", FileState{ByteOffset: 1024, Mtime: 1740825600.123})

	if err := m.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// The final file should exist
	if _, err := os.Stat(statePath); err != nil {
		t.Errorf("state file does not exist: %v", err)
	}

	// The .tmp file should NOT exist (it was renamed)
	tmpPath := statePath + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Errorf(".tmp file should not exist after rename, got err: %v", err)
	}
}

// TestStateFilePermissions verifies that the state file is created with
// 0600 permissions (owner read/write only).
func TestStateFilePermissions(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "shipper-state.json")

	m, err := NewManager(statePath)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	m.Set("/path/to/file.jsonl", FileState{ByteOffset: 512, Mtime: 1740825600.0})

	if err := m.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(statePath)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("permissions: got %04o, want 0600", perm)
	}
}

// TestStatePruneStaleEntries verifies that Prune removes entries for
// files that no longer exist on disk while keeping entries for files
// that do exist.
func TestStatePruneStaleEntries(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "shipper-state.json")

	// Create one real file that exists
	realFile := filepath.Join(tmpDir, "real.jsonl")
	os.WriteFile(realFile, []byte("data\n"), 0644)

	m, err := NewManager(statePath)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Add entries for a real file and a stale file
	m.Set(realFile, FileState{ByteOffset: 100, Mtime: 1740825600.0})
	m.Set("/nonexistent/stale.jsonl", FileState{ByteOffset: 200, Mtime: 1740825600.0})

	pruned := m.Prune()
	if pruned != 1 {
		t.Errorf("pruned: got %d, want 1", pruned)
	}

	if m.TrackedFiles() != 1 {
		t.Errorf("tracked files after prune: got %d, want 1", m.TrackedFiles())
	}

	// The real file entry should still be present
	fs := m.Get(realFile)
	if fs.ByteOffset != 100 {
		t.Errorf("real file byte_offset: got %d, want 100", fs.ByteOffset)
	}

	// The stale entry should be gone
	fs = m.Get("/nonexistent/stale.jsonl")
	if fs.ByteOffset != 0 {
		t.Errorf("stale file should return zero FileState, got byte_offset %d", fs.ByteOffset)
	}
}

// TestStateRoundTrip verifies that state survives a save-load cycle:
// data written by one Manager instance can be read back by another.
func TestStateRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "shipper-state.json")

	// Write state
	m1, err := NewManager(statePath)
	if err != nil {
		t.Fatalf("NewManager (write): %v", err)
	}

	m1.Set("/path/to/a.jsonl", FileState{ByteOffset: 4096, Mtime: 1740825600.5})
	m1.Set("/path/to/b.jsonl", FileState{ByteOffset: 8192, Mtime: 1740825700.0})

	if err := m1.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Read state in a new manager
	m2, err := NewManager(statePath)
	if err != nil {
		t.Fatalf("NewManager (read): %v", err)
	}

	if m2.TrackedFiles() != 2 {
		t.Errorf("tracked files: got %d, want 2", m2.TrackedFiles())
	}

	fs := m2.Get("/path/to/a.jsonl")
	if fs.ByteOffset != 4096 {
		t.Errorf("a.jsonl byte_offset: got %d, want 4096", fs.ByteOffset)
	}
	if fs.Mtime != 1740825600.5 {
		t.Errorf("a.jsonl mtime: got %f, want 1740825600.5", fs.Mtime)
	}
}
