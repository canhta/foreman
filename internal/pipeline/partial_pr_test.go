package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestShouldCreatePartialPR_WithCompletedTasks(t *testing.T) {
	assert.True(t, ShouldCreatePartialPR(5, 3, true))
}

func TestShouldCreatePartialPR_NoCompletedTasks(t *testing.T) {
	assert.False(t, ShouldCreatePartialPR(5, 0, true))
}

func TestShouldCreatePartialPR_Disabled(t *testing.T) {
	assert.False(t, ShouldCreatePartialPR(5, 3, false))
}

func TestShouldCreatePartialPR_AllComplete(t *testing.T) {
	// Not partial if all tasks are done
	assert.False(t, ShouldCreatePartialPR(5, 5, true))
}

func TestFormatPartialPRComment(t *testing.T) {
	comment := FormatPartialPRComment(42, 3, 5, "Add JWT validation", "Tests failed after 2 retries", []string{"Update routes", "Add middleware"})
	assert.Contains(t, comment, "PR #42")
	assert.Contains(t, comment, "3/5")
	assert.Contains(t, comment, "JWT validation")
	assert.Contains(t, comment, "Tests failed")
	assert.Contains(t, comment, "Update routes")
	assert.Contains(t, comment, "human developer")
}

func TestFormatRemainingTasks(t *testing.T) {
	tasks := []string{"Task A", "Task B", "Task C"}
	formatted := FormatRemainingTasks(tasks)
	assert.Contains(t, formatted, "- Task A")
	assert.Contains(t, formatted, "- Task B")
	assert.Contains(t, formatted, "- Task C")
}
