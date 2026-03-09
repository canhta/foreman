package tools

import (
	"bufio"
	"fmt"
	"strings"
)

// ValidatePatchHunks validates a unified diff patch against the original file content.
// It checks that context lines and deleted lines in each hunk match the original.
// Returns a list of mismatch descriptions. An empty slice means the patch is valid.
// New files (from /dev/null) have no context to validate — returns empty.
func ValidatePatchHunks(original, patch string) []string {
	// Detect new file creation (--- /dev/null)
	scanner := bufio.NewScanner(strings.NewReader(patch))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "--- /dev/null") {
			return nil
		}
		// Stop after the first file header line
		if strings.HasPrefix(line, "--- ") {
			break
		}
	}

	hunks, err := parseUnifiedDiff(patch)
	if err != nil {
		return []string{fmt.Sprintf("patch parse error: %s", err.Error())}
	}

	if original == "" {
		return nil
	}

	originalLines := strings.Split(original, "\n")
	var errs []string

	for _, h := range hunks {
		// origStart is 1-based; convert to 0-based index
		pos := h.origStart - 1
		origIdx := pos

		for _, pl := range h.lines {
			if pl.op == '+' {
				// Added lines are not in the original — skip
				continue
			}
			// Context (' ') and deleted ('-') lines must match the original
			if origIdx < 0 || origIdx >= len(originalLines) {
				errs = append(errs, fmt.Sprintf(
					"hunk @@ -%d,%d +%d,%d @@: original file has only %d lines, expected context at line %d",
					h.origStart, h.origCount, h.newStart, h.newCount,
					len(originalLines), origIdx+1,
				))
				break
			}
			if originalLines[origIdx] != pl.text {
				errs = append(errs, fmt.Sprintf(
					"hunk @@ -%d,%d +%d,%d @@: context mismatch at line %d: expected %q, got %q",
					h.origStart, h.origCount, h.newStart, h.newCount,
					origIdx+1, pl.text, originalLines[origIdx],
				))
			}
			origIdx++
		}
	}

	return errs
}
