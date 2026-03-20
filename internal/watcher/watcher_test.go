package watcher

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestWatcherDetectsNewJSONLFile verifies that the watcher emits a
// Create event when a new .jsonl file is created in the watched directory.
func TestWatcherDetectsNewJSONLFile(t *testing.T) {
	tmpDir := t.TempDir()

	w, err := New(tmpDir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := w.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer w.Close()

	// Allow the watcher to settle
	time.Sleep(100 * time.Millisecond)

	// Create a new .jsonl file
	newFile := filepath.Join(tmpDir, "test-session.jsonl")
	if err := os.WriteFile(newFile, []byte(`{"type":"assistant"}`+"\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Wait for the event
	select {
	case evt := <-w.Events():
		if evt.Path != newFile {
			t.Errorf("event path: got %q, want %q", evt.Path, newFile)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for create event")
	}
}

// TestWatcherDetectsWriteEvents verifies that appending to an existing
// .jsonl file emits a Write event.
func TestWatcherDetectsWriteEvents(t *testing.T) {
	tmpDir := t.TempDir()

	// Pre-create the file before starting the watcher
	existingFile := filepath.Join(tmpDir, "existing.jsonl")
	if err := os.WriteFile(existingFile, []byte(`{"type":"user"}`+"\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	w, err := New(tmpDir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := w.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer w.Close()

	// Allow the watcher to settle
	time.Sleep(100 * time.Millisecond)

	// Append to the file
	f, err := os.OpenFile(existingFile, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	f.WriteString(`{"type":"assistant"}` + "\n")
	f.Close()

	// Wait for the write event
	select {
	case evt := <-w.Events():
		if evt.Path != existingFile {
			t.Errorf("event path: got %q, want %q", evt.Path, existingFile)
		}
		if evt.Op != OpWrite {
			t.Errorf("event op: got %v, want OpWrite", evt.Op)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for write event")
	}
}

// TestWatcherIgnoresNonJSONLFiles verifies that the watcher does not
// emit events for files without the .jsonl extension.
func TestWatcherIgnoresNonJSONLFiles(t *testing.T) {
	tmpDir := t.TempDir()

	w, err := New(tmpDir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := w.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer w.Close()

	// Allow the watcher to settle
	time.Sleep(100 * time.Millisecond)

	// Create non-jsonl files
	os.WriteFile(filepath.Join(tmpDir, "notes.txt"), []byte("hello"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "data.json"), []byte("{}"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "log.csv"), []byte("a,b"), 0644)

	// Wait briefly -- no events should arrive
	select {
	case evt := <-w.Events():
		t.Errorf("unexpected event for non-jsonl file: %+v", evt)
	case <-time.After(500 * time.Millisecond):
		// Good: no events received
	}
}
