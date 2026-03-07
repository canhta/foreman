// internal/pipeline/feedback.go
package pipeline

import (
	"fmt"
	"strings"
)

const maxFeedbackLen = 2000

// maxSummaryLen is the truncation limit for the collapsed "Prior attempt summary"
// entry produced by ResetKeepingSummary. A summary combines all prior entries and
// can legitimately be several times larger than a single entry, so it uses a
// proportionally larger cap to avoid silently losing valid context.
const maxSummaryLen = maxFeedbackLen * 3

// FeedbackAccumulator collects feedback from various pipeline stages
// for retry prompts.
type FeedbackAccumulator struct {
	entries []feedbackEntry
}

type feedbackEntry struct {
	category string
	content  string
}

// NewFeedbackAccumulator creates an empty feedback accumulator.
func NewFeedbackAccumulator() *FeedbackAccumulator {
	return &FeedbackAccumulator{}
}

// HasFeedback returns true if any feedback has been recorded.
func (f *FeedbackAccumulator) HasFeedback() bool {
	return len(f.entries) > 0
}

// Attempt returns the current number of feedback entries. This is the raw entry
// count: multiple pipeline stages can each contribute an entry within one retry
// cycle (e.g., a lint error and a test failure), and after ResetKeepingSummary()
// the count resets to 1 (the collapsed "Prior attempt summary" entry). It does
// not represent the number of retry cycles performed.
func (f *FeedbackAccumulator) Attempt() int {
	return len(f.entries)
}

// AddLintError adds lint failure output.
func (f *FeedbackAccumulator) AddLintError(output string) {
	f.entries = append(f.entries, feedbackEntry{
		category: "Lint errors",
		content:  truncate(output, maxFeedbackLen),
	})
}

// AddTestError adds test failure output.
func (f *FeedbackAccumulator) AddTestError(output string) {
	f.entries = append(f.entries, feedbackEntry{
		category: "Test failures",
		content:  truncate(output, maxFeedbackLen),
	})
}

// AddSpecFeedback adds spec reviewer feedback.
func (f *FeedbackAccumulator) AddSpecFeedback(feedback string) {
	f.entries = append(f.entries, feedbackEntry{
		category: "Spec review issues",
		content:  truncate(feedback, maxFeedbackLen),
	})
}

// AddQualityFeedback adds quality reviewer feedback.
func (f *FeedbackAccumulator) AddQualityFeedback(feedback string) {
	f.entries = append(f.entries, feedbackEntry{
		category: "Quality review issues",
		content:  truncate(feedback, maxFeedbackLen),
	})
}

// AddTDDFeedback adds TDD verification feedback.
func (f *FeedbackAccumulator) AddTDDFeedback(feedback string) {
	f.entries = append(f.entries, feedbackEntry{
		category: "TDD verification failed",
		content:  truncate(feedback, maxFeedbackLen),
	})
}

// Reset clears all accumulated feedback for reuse across retry rounds.
func (f *FeedbackAccumulator) Reset() {
	f.entries = f.entries[:0]
}

// ResetKeepingSummary collapses all current entries into a single "Prior attempt
// summary" entry, then clears the rest. This preserves context from prior retry
// attempts without growing the feedback list unboundedly. If there is no current
// feedback, the accumulator is left empty.
func (f *FeedbackAccumulator) ResetKeepingSummary() {
	if len(f.entries) == 0 {
		return
	}
	// Render the current state into a single summary string.
	summary := f.Render()
	f.entries = f.entries[:0]
	f.entries = append(f.entries, feedbackEntry{
		category: "Prior attempt summary",
		content:  truncate(summary, maxSummaryLen),
	})
}

// Render produces the combined feedback string for inclusion in retry prompts.
func (f *FeedbackAccumulator) Render() string {
	if len(f.entries) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, e := range f.entries {
		fmt.Fprintf(&sb, "## %s\n%s\n\n", e.category, e.content)
	}
	return strings.TrimSpace(sb.String())
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "\n... (truncated)"
}
