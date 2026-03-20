package reader

import (
	"os"
	"path/filepath"
	"testing"
)

// TestReaderFromOffsetZero verifies that reading from offset 0 returns
// all complete lines in the file.
func TestReaderFromOffsetZero(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.jsonl")

	content := `{"type":"assistant","id":"1"}
{"type":"assistant","id":"2"}
{"type":"user","id":"3"}
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	result, err := ReadFromOffset(path, 0)
	if err != nil {
		t.Fatalf("ReadFromOffset: %v", err)
	}

	if len(result.Lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(result.Lines))
	}

	if string(result.Lines[0]) != `{"type":"assistant","id":"1"}` {
		t.Errorf("line 0: got %q", string(result.Lines[0]))
	}
	if string(result.Lines[2]) != `{"type":"user","id":"3"}` {
		t.Errorf("line 2: got %q", string(result.Lines[2]))
	}

	// NewOffset should equal the file size
	if result.NewOffset != int64(len(content)) {
		t.Errorf("NewOffset: got %d, want %d", result.NewOffset, len(content))
	}
}

// TestReaderSeeksToOffset verifies that reading from a non-zero offset
// returns only the lines appended after that offset.
func TestReaderSeeksToOffset(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.jsonl")

	line1 := `{"type":"assistant","id":"1"}` + "\n"
	line2 := `{"type":"assistant","id":"2"}` + "\n"
	content := line1 + line2

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Read from offset equal to the length of the first line
	offset := int64(len(line1))
	result, err := ReadFromOffset(path, offset)
	if err != nil {
		t.Fatalf("ReadFromOffset: %v", err)
	}

	if len(result.Lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(result.Lines))
	}

	if string(result.Lines[0]) != `{"type":"assistant","id":"2"}` {
		t.Errorf("line: got %q", string(result.Lines[0]))
	}

	if result.NewOffset != int64(len(content)) {
		t.Errorf("NewOffset: got %d, want %d", result.NewOffset, len(content))
	}
}

// TestReaderHandlesPartialLine verifies that a file ending without a
// trailing newline (partial/incomplete line) does not include that line
// in the results.  The partial line will be picked up on the next read
// once it is completed with a newline.
func TestReaderHandlesPartialLine(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.jsonl")

	completeLine := `{"type":"assistant","id":"1"}` + "\n"
	partialLine := `{"type":"assistant","id":"2"` // no trailing newline
	content := completeLine + partialLine

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	result, err := ReadFromOffset(path, 0)
	if err != nil {
		t.Fatalf("ReadFromOffset: %v", err)
	}

	// Only the complete line should be returned
	if len(result.Lines) != 1 {
		t.Fatalf("expected 1 complete line, got %d", len(result.Lines))
	}

	if string(result.Lines[0]) != `{"type":"assistant","id":"1"}` {
		t.Errorf("line: got %q", string(result.Lines[0]))
	}

	// NewOffset should be at the end of the complete line, not the partial one
	if result.NewOffset != int64(len(completeLine)) {
		t.Errorf("NewOffset: got %d, want %d (should stop before partial line)", result.NewOffset, len(completeLine))
	}
}
