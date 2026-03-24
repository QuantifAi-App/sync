// Package scanner provides poll-based directory scanning for JSONL files.
// Unlike the fsnotify watcher, scanner does a full walk every cycle and
// compares file sizes against stored byte offsets to find files with new
// data.  This handles both initial backfill (empty state = read everything)
// and ongoing sync (only read new bytes).
package scanner

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/quantifai/sync/internal/state"
)

// FileToProcess represents a JSONL file that has new data to read.
type FileToProcess struct {
	Path       string
	ByteOffset int64 // resume reading from this position
}

// Scan walks the root directory recursively and returns all .jsonl files
// that have new data (file size > stored byte offset).  Files not in the
// state manager are treated as new (offset 0 = read from start).
//
// This is called every flush interval.  Walking ~2500 files on SSD takes
// <10ms.  Only files with new data are returned for reading.
func Scan(root string, stateMgr *state.Manager) []FileToProcess {
	var result []FileToProcess

	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip inaccessible paths
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(path), ".jsonl") {
			return nil
		}
		if info.Size() == 0 {
			return nil
		}

		stored := stateMgr.Get(path)
		if info.Size() > stored.ByteOffset {
			result = append(result, FileToProcess{
				Path:       path,
				ByteOffset: stored.ByteOffset,
			})
		}

		return nil
	})

	return result
}

// Count returns total JSONL files and how many have unread data.
func Count(root string, stateMgr *state.Manager) (total int, pending int) {
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(path), ".jsonl") || info.Size() == 0 {
			return nil
		}
		total++
		stored := stateMgr.Get(path)
		if info.Size() > stored.ByteOffset {
			pending++
		}
		return nil
	})
	return
}
