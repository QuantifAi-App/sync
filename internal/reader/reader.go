// Package reader provides incremental byte-offset file reading for
// JSONL files.  It opens a file, seeks to a persisted byte offset, and
// returns each complete line as raw bytes along with the updated offset.
package reader

import (
	"bufio"
	"io"
	"os"
)

// ReadResult holds the lines read from a file and the new byte offset
// after reading.
type ReadResult struct {
	Lines     [][]byte // complete lines (no trailing newline)
	NewOffset int64    // byte offset after the last complete line
}

// ReadFromOffset opens the file at path, seeks to the given byte offset,
// and reads all complete lines from that point forward.  Partial lines
// (data without a trailing newline at the end of the file) are NOT
// returned -- they will be picked up on the next read when the line is
// completed.
//
// Empty lines are skipped.  The returned NewOffset is the byte position
// immediately after the last complete line that was read, which is the
// correct seek position for the next incremental read.
func ReadFromOffset(path string, offset int64) (*ReadResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Seek to the persisted byte offset
	if offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return nil, err
		}
	}

	result := &ReadResult{
		NewOffset: offset,
	}

	scanner := bufio.NewScanner(f)
	// Allow lines up to 10 MB (JSONL records with large content blocks)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()

		// Track bytes consumed: line length + 1 for the newline delimiter
		result.NewOffset += int64(len(line)) + 1

		// Skip empty lines
		if len(line) == 0 {
			continue
		}

		// Make a copy since scanner reuses the buffer
		lineCopy := make([]byte, len(line))
		copy(lineCopy, line)
		result.Lines = append(result.Lines, lineCopy)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Check if the file ends without a newline (partial line scenario).
	// bufio.Scanner already handles this correctly: it returns the last
	// line even without a newline.  However, we need to detect this case
	// and exclude that line because it may be incomplete (still being written).
	//
	// To detect: compare our calculated offset with the actual file size.
	// If they differ, the last line lacked a trailing newline.
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}

	if result.NewOffset > fi.Size() {
		// Scanner returned a line without a trailing newline.
		// Remove the last line (it is partial/incomplete) and adjust offset.
		if len(result.Lines) > 0 {
			lastLine := result.Lines[len(result.Lines)-1]
			result.Lines = result.Lines[:len(result.Lines)-1]
			// Subtract the line length + 1 (the newline we assumed)
			result.NewOffset -= int64(len(lastLine)) + 1
		}
	}

	return result, nil
}
