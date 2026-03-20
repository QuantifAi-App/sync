// Package watcher provides fsnotify-based recursive directory watching
// for JSONL files.  It monitors ~/.claude/projects/ (or a configurable
// directory) and emits file-change events on a channel for the main
// pipeline to consume.
package watcher

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// Event represents a JSONL file that was created or modified.
type Event struct {
	Path string // absolute path to the .jsonl file
	Op   Op     // operation type
}

// Op describes the kind of file event.
type Op int

const (
	OpCreate Op = iota
	OpWrite
)

// Watcher recursively monitors a directory tree for .jsonl file changes
// using fsnotify (native OS file-change events, not polling).
type Watcher struct {
	fsw    *fsnotify.Watcher
	events chan Event
	errors chan error
	done   chan struct{}
	wg     sync.WaitGroup
	root   string
}

// New creates a Watcher that monitors the given root directory and all
// its subdirectories.  Call Start() to begin watching, and Events()/Errors()
// to consume notifications.
func New(root string) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	w := &Watcher{
		fsw:    fsw,
		events: make(chan Event, 256),
		errors: make(chan error, 16),
		done:   make(chan struct{}),
		root:   root,
	}

	return w, nil
}

// Events returns the channel that receives filtered JSONL file events.
func (w *Watcher) Events() <-chan Event {
	return w.events
}

// Errors returns the channel that receives watcher errors.
func (w *Watcher) Errors() <-chan error {
	return w.errors
}

// Start begins watching the root directory recursively.  It adds all
// existing subdirectories to the watch list and starts the event loop.
func (w *Watcher) Start() error {
	// Walk the directory tree and add all directories to the watcher
	if err := w.addRecursive(w.root); err != nil {
		return err
	}

	w.wg.Add(1)
	go w.loop()

	return nil
}

// Close stops the watcher and cleans up resources.
func (w *Watcher) Close() error {
	close(w.done)
	err := w.fsw.Close()
	w.wg.Wait()
	close(w.events)
	close(w.errors)
	return err
}

// loop is the main event processing goroutine.
func (w *Watcher) loop() {
	defer w.wg.Done()

	for {
		select {
		case <-w.done:
			return

		case event, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			w.handleEvent(event)

		case err, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			// Log and continue rather than crashing
			select {
			case w.errors <- err:
			default:
				// Drop if error channel is full
			}
		}
	}
}

// handleEvent processes a single fsnotify event, filtering for .jsonl
// files and dynamically adding new subdirectories to the watch list.
func (w *Watcher) handleEvent(event fsnotify.Event) {
	path := event.Name

	// If a new directory was created, add it to the watch list
	if event.Has(fsnotify.Create) {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			_ = w.addRecursive(path)
			return
		}
	}

	// Only emit events for .jsonl files
	if !isJSONLFile(path) {
		return
	}

	if event.Has(fsnotify.Create) {
		select {
		case w.events <- Event{Path: path, Op: OpCreate}:
		case <-w.done:
		}
	}

	if event.Has(fsnotify.Write) {
		select {
		case w.events <- Event{Path: path, Op: OpWrite}:
		case <-w.done:
		}
	}
}

// addRecursive walks a directory tree and adds each directory to the
// fsnotify watcher.
func (w *Watcher) addRecursive(root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip inaccessible paths
		}
		if info.IsDir() {
			if watchErr := w.fsw.Add(path); watchErr != nil {
				// Non-fatal: log and continue
				select {
				case w.errors <- watchErr:
				default:
				}
			}
		}
		return nil
	})
}

// isJSONLFile returns true if the path has a .jsonl extension.
func isJSONLFile(path string) bool {
	return strings.HasSuffix(strings.ToLower(path), ".jsonl")
}
