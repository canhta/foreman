// internal/pipeline/feedback.go
package pipeline

import (
	"fmt"
	"strings"
)

const maxFeedbackLen = 2000

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

// Attempt returns the number of feedback entries (correlates to retry count).
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

// Render produces the combined feedback string for inclusion in retry prompts.
func (f *FeedbackAccumulator) Render() string {
	if len(f.entries) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, e := range f.entries {
		sb.WriteString(fmt.Sprintf("## %s\n%s\n\n", e.category, e.content))
	}
	return strings.TrimSpace(sb.String())
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "\n... (truncated)"
}
