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

	// Cap the slice to the last `threshold` entries to bound memory growth.
	if len(d.history) > d.threshold {
		d.history = d.history[len(d.history)-d.threshold:]
	}

	// Only trigger once we have a full window.
	if len(d.history) < d.threshold {
		return false
	}

	for _, h := range d.history {
		if h != key {
			return false
		}
	}
	return true
}

func hash(tool, input string) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s:%s", tool, input)))
	return fmt.Sprintf("%x", h[:8])
}
