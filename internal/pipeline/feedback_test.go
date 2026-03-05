// internal/pipeline/feedback_test.go
package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
