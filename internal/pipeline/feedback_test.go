// internal/pipeline/feedback_test.go
package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFeedbackAccumulator_Empty(t *testing.T) {
	fb := NewFeedbackAccumulator()
	assert.False(t, fb.HasFeedback())
	assert.Equal(t, 0, fb.Attempt())
}

func TestFeedbackAccumulator_AddLintError(t *testing.T) {
	fb := NewFeedbackAccumulator()
	fb.AddLintError("main.go:10: unused variable 'x'")
	assert.True(t, fb.HasFeedback())
	assert.Equal(t, 1, fb.Attempt())
	rendered := fb.Render()
	assert.Contains(t, rendered, "Lint errors")
	assert.Contains(t, rendered, "unused variable")
}

func TestFeedbackAccumulator_AddTestError(t *testing.T) {
	fb := NewFeedbackAccumulator()
	fb.AddTestError("--- FAIL: TestFoo (0.00s)\n    foo_test.go:5: expected 1 got 2")
	rendered := fb.Render()
	assert.Contains(t, rendered, "Test failures")
}

func TestFeedbackAccumulator_AddSpecFeedback(t *testing.T) {
	fb := NewFeedbackAccumulator()
	fb.AddSpecFeedback("Missing error handling for nil input")
	rendered := fb.Render()
	assert.Contains(t, rendered, "Spec review")
	assert.Contains(t, rendered, "nil input")
}

func TestFeedbackAccumulator_AddQualityFeedback(t *testing.T) {
	fb := NewFeedbackAccumulator()
	fb.AddQualityFeedback("[CRITICAL] SQL injection in query builder")
	rendered := fb.Render()
	assert.Contains(t, rendered, "Quality review")
	assert.Contains(t, rendered, "SQL injection")
}

func TestFeedbackAccumulator_AddTDDFeedback(t *testing.T) {
	fb := NewFeedbackAccumulator()
	fb.AddTDDFeedback("Tests passed without implementation — tests are not verifying new behavior")
	rendered := fb.Render()
	assert.Contains(t, rendered, "TDD verification")
}

func TestFeedbackAccumulator_MultipleFeedback(t *testing.T) {
	fb := NewFeedbackAccumulator()
	fb.AddLintError("error1")
	fb.AddTestError("error2")
	fb.AddSpecFeedback("spec issue")
	assert.Equal(t, 3, fb.Attempt())
	rendered := fb.Render()
	assert.Contains(t, rendered, "Lint errors")
	assert.Contains(t, rendered, "Test failures")
	assert.Contains(t, rendered, "Spec review")
}

func TestFeedbackAccumulator_Reset(t *testing.T) {
	fb := NewFeedbackAccumulator()
	fb.AddLintError("some error")
	fb.AddTestError("test failure")
	assert.True(t, fb.HasFeedback())
	assert.Equal(t, 2, fb.Attempt())
	fb.Reset()
	assert.False(t, fb.HasFeedback())
	assert.Equal(t, 0, fb.Attempt())
	assert.Empty(t, fb.Render())
}

func TestFeedbackAccumulator_Truncation(t *testing.T) {
	fb := NewFeedbackAccumulator()
	longError := make([]byte, 5000)
	for i := range longError {
		longError[i] = 'a'
	}
	fb.AddTestError(string(longError))
	rendered := fb.Render()
	assert.Less(t, len(rendered), 4000) // Should be truncated
}

func TestFeedbackAccumulator_ResetKeepingSummary_CollapsesPriorFeedback(t *testing.T) {
	fb := NewFeedbackAccumulator()

	// Simulate feedback from attempt 1.
	fb.AddTestError("test failed: panic in handler")
	fb.AddQualityFeedback("[CRITICAL] SQL injection vulnerability")
	require.True(t, fb.HasFeedback())
	require.Equal(t, 2, fb.Attempt())

	// Collapse to summary.
	fb.ResetKeepingSummary()

	// Should have exactly one entry now.
	assert.Equal(t, 1, fb.Attempt(), "ResetKeepingSummary should collapse to a single entry")
	assert.True(t, fb.HasFeedback(), "should still have feedback after collapse")

	// The single entry should mention prior attempt / summary.
	rendered := fb.Render()
	assert.Contains(t, rendered, "Prior attempt summary", "collapsed entry should be labelled as prior summary")
	// Original content should be preserved in the summary.
	assert.Contains(t, rendered, "test failed", "prior test error content should survive in summary")
	assert.Contains(t, rendered, "SQL injection", "prior quality feedback should survive in summary")
}

func TestFeedbackAccumulator_ResetKeepingSummary_ThenAddNewFeedback(t *testing.T) {
	fb := NewFeedbackAccumulator()

	// Attempt 1 feedback.
	fb.AddLintError("unused import")
	fb.ResetKeepingSummary()

	// Attempt 2 feedback.
	fb.AddTestError("new test failure")

	rendered := fb.Render()
	// Both the collapsed summary and the new error should appear.
	assert.Contains(t, rendered, "Prior attempt summary")
	assert.Contains(t, rendered, "new test failure")
}

func TestFeedbackAccumulator_ResetKeepingSummary_WhenEmpty_RemainsEmpty(t *testing.T) {
	fb := NewFeedbackAccumulator()
	fb.ResetKeepingSummary()
	assert.False(t, fb.HasFeedback(), "empty accumulator should stay empty after ResetKeepingSummary")
	assert.Equal(t, 0, fb.Attempt())
}

func TestFeedbackAccumulator_ResetKeepingSummary_UsesLargerLimit(t *testing.T) {
	// Fill three entries each near maxFeedbackLen characters. The combined
	// rendered summary will be well above maxFeedbackLen, so using the old per-entry
	// cap would silently discard the second and third entries. The summary cap
	// (maxSummaryLen = maxFeedbackLen*3) must preserve all content.
	fb := NewFeedbackAccumulator()
	padding := func(n int) string {
		b := make([]byte, n)
		for i := range b {
			b[i] = 'x'
		}
		return string(b)
	}
	// Each entry content is padded to just under maxFeedbackLen so it passes
	// the per-entry truncation, but the combined summary exceeds maxFeedbackLen.
	fb.AddLintError("lint-marker " + padding(maxFeedbackLen-20))
	fb.AddTestError("test-marker " + padding(maxFeedbackLen-20))
	fb.AddQualityFeedback("quality-marker " + padding(maxFeedbackLen-20))

	fb.ResetKeepingSummary()

	rendered := fb.Render()
	// All three category headers must survive in the summary.
	assert.Contains(t, rendered, "Lint errors", "lint entry must survive summary collapse")
	assert.Contains(t, rendered, "Test failures", "test entry must survive summary collapse")
	assert.Contains(t, rendered, "Quality review issues", "quality entry must survive summary collapse")
	// Unique markers from each entry must still be present.
	assert.Contains(t, rendered, "lint-marker", "lint content must survive summary collapse")
	assert.Contains(t, rendered, "test-marker", "test content must survive summary collapse")
	assert.Contains(t, rendered, "quality-marker", "quality content must survive summary collapse")
}
