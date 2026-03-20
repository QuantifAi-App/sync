// Package state provides JSON-file-based byte offset persistence for
// the incremental file reader.  The state file records the last
// successfully consumed byte offset for each watched JSONL file so the
// shipper can resume without re-processing data after a restart.
//
// Writes are atomic: data is written to a .tmp file and then renamed
// into place, preventing corruption if the process is killed mid-write.
// The state file has 0600 permissions (owner read/write only).
package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// FileState records the progress for a single watched file.
type FileState struct {
	ByteOffset int64   `json:"byte_offset"`
	Mtime      float64 `json:"mtime"` // Unix timestamp with sub-second precision
}

// StateFile is the top-level JSON structure persisted to disk.
type StateFile struct {
	Version   int                  `json:"version"`
	UpdatedAt string               `json:"updated_at"`
	Files     map[string]FileState `json:"files"`
}

// Manager provides thread-safe access to the byte offset state.
type Manager struct {
	mu   sync.RWMutex
	path string
	data StateFile
}

// NewManager creates a state Manager that reads from and writes to the
// given path.  If the file exists it is loaded; otherwise an empty state
// is initialized.
func NewManager(path string) (*Manager, error) {
	m := &Manager{
		path: path,
		data: StateFile{
			Version: 1,
			Files:   make(map[string]FileState),
		},
	}

	// Load existing state if the file exists
	if _, err := os.Stat(path); err == nil {
		if loadErr := m.load(); loadErr != nil {
			return nil, fmt.Errorf("state: load %s: %w", path, loadErr)
		}
	}

	return m, nil
}

// Get returns the persisted state for a file path, or a zero FileState
// if the file has not been tracked.
func (m *Manager) Get(filePath string) FileState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.data.Files[filePath]
}

// Set updates the state for a file path.  The change is held in memory
// until Save() is called.
func (m *Manager) Set(filePath string, fs FileState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data.Files[filePath] = fs
}

// Save persists the current state to disk using atomic write
// (write to .tmp then os.Rename).  The file is created with 0600
// permissions.
func (m *Manager) Save() error {
	m.mu.RLock()
	snapshot := m.data
	snapshot.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	m.mu.RUnlock()

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("state: marshal: %w", err)
	}
	data = append(data, '\n')

	// Ensure the parent directory exists
	dir := filepath.Dir(m.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("state: mkdir %s: %w", dir, err)
	}

	// Atomic write: write to .tmp then rename
	tmpPath := m.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("state: write tmp: %w", err)
	}

	if err := os.Rename(tmpPath, m.path); err != nil {
		os.Remove(tmpPath) // cleanup on failure
		return fmt.Errorf("state: rename: %w", err)
	}

	return nil
}

// Prune removes entries for files that no longer exist on disk.
// This is called on startup to clean up stale state.
func (m *Manager) Prune() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	pruned := 0
	for path := range m.data.Files {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			delete(m.data.Files, path)
			pruned++
		}
	}
	return pruned
}

// TrackedFiles returns the number of files currently in the state.
func (m *Manager) TrackedFiles() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.data.Files)
}

// load reads and parses the state file from disk.
func (m *Manager) load() error {
	data, err := os.ReadFile(m.path)
	if err != nil {
		return err
	}

	var sf StateFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return err
	}

	if sf.Files == nil {
		sf.Files = make(map[string]FileState)
	}

	m.data = sf
	return nil
}
