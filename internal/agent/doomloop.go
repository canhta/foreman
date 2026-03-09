package agent

import (
	"crypto/sha256"
	"fmt"
)

// DoomLoopDetector tracks repeated identical tool calls to prevent infinite loops.
type DoomLoopDetector struct {
	history   []string
	threshold int
}

// NewDoomLoopDetector creates a detector that triggers after `threshold` consecutive
// identical tool calls.
func NewDoomLoopDetector(threshold int) *DoomLoopDetector {
	return &DoomLoopDetector{threshold: threshold}
}

// Check returns true if the same tool+input has been called `threshold` times
// consecutively.
func (d *DoomLoopDetector) Check(toolName, input string) bool {
	key := hash(toolName, input)
	d.history = append(d.history, key)

	// Only check the last `threshold` entries
	if len(d.history) < d.threshold {
		return false
	}

	last := d.history[len(d.history)-d.threshold:]
	for _, h := range last {
		if h != key {
			return false
		}
	}
	return true
}

// Reset clears the history.
func (d *DoomLoopDetector) Reset() {
	d.history = nil
}

func hash(tool, input string) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s:%s", tool, input)))
	return fmt.Sprintf("%x", h[:8])
}
