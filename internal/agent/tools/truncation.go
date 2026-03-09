package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	DefaultMaxLines = 2000
	DefaultMaxBytes = 50 * 1024 // 50KB
	MaxLineLength   = 2000
)

// TruncateOutput truncates tool output that exceeds line or byte limits.
// Returns the truncated output and whether truncation occurred.
func TruncateOutput(output string, maxLines, maxBytes int) (string, bool) {
	lines := strings.Split(output, "\n")
	truncated := false

	// Truncate individual long lines
	for i, line := range lines {
		if len(line) > MaxLineLength {
			lines[i] = line[:MaxLineLength] + "..."
			truncated = true
		}
	}

	// Truncate by line count
	if len(lines) > maxLines {
		omitted := len(lines) - maxLines
		lines = lines[:maxLines]
		lines = append(lines, fmt.Sprintf("\n... (%d lines omitted. Use Read with offset to see the rest.)", omitted))
		truncated = true
	}

	result := strings.Join(lines, "\n")

	// Truncate by byte count
	if len(result) > maxBytes {
		result = result[:maxBytes]
		result += "\n... (output truncated at byte limit. Full output saved to disk.)"
		truncated = true
	}

	return result, truncated
}

// SaveTruncatedOutput saves the full output to disk for later retrieval.
func SaveTruncatedOutput(dataDir, toolCallID, output string) (string, error) {
	dir := filepath.Join(dataDir, "tool-output")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("tool_%s.txt", toolCallID))
	if err := os.WriteFile(path, []byte(output), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// TruncateHint returns a hint message for the agent when output was truncated.
func TruncateHint(savedPath string) string {
	return fmt.Sprintf("Output was truncated. Full output saved to: %s\nUse Read with offset/limit or Grep to find specific content.", savedPath)
}
