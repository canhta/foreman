package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectDecompositionConflicts_NoConflicts(t *testing.T) {
	children := []ChildTicketSpec{
		{Title: "Child A", FilesToModify: []string{"internal/auth/login.go", "internal/auth/model.go"}},
		{Title: "Child B", FilesToModify: []string{"internal/api/handler.go"}},
		{Title: "Child C", FilesToModify: []string{"internal/db/schema.go", "migrations/001_init.sql"}},
	}

	conflicts := DetectDecompositionConflicts(children)

	assert.Empty(t, conflicts, "expected no conflicts when all children modify different files")
}

func TestDetectDecompositionConflicts_SingleConflict(t *testing.T) {
	children := []ChildTicketSpec{
		{Title: "Child A", FilesToModify: []string{"internal/auth/login.go", "internal/auth/model.go"}},
		{Title: "Child B", FilesToModify: []string{"internal/auth/login.go", "internal/api/handler.go"}},
	}

	conflicts := DetectDecompositionConflicts(children)

	require.Len(t, conflicts, 1, "expected exactly one conflict")
	assert.Equal(t, "internal/auth/login.go", conflicts[0].File)
	assert.ElementsMatch(t, []string{"Child A", "Child B"}, conflicts[0].Children)
}

func TestDetectDecompositionConflicts_MultipleConflicts(t *testing.T) {
	// A+B share file1, B+C share file2
	children := []ChildTicketSpec{
		{Title: "Child A", FilesToModify: []string{"shared/file1.go", "only_a.go"}},
		{Title: "Child B", FilesToModify: []string{"shared/file1.go", "shared/file2.go"}},
		{Title: "Child C", FilesToModify: []string{"shared/file2.go", "only_c.go"}},
	}

	conflicts := DetectDecompositionConflicts(children)

	require.Len(t, conflicts, 2, "expected two conflicts")

	conflictFiles := make(map[string][]string, len(conflicts))
	for _, c := range conflicts {
		conflictFiles[c.File] = c.Children
	}

	assert.Contains(t, conflictFiles, "shared/file1.go")
	assert.ElementsMatch(t, []string{"Child A", "Child B"}, conflictFiles["shared/file1.go"])

	assert.Contains(t, conflictFiles, "shared/file2.go")
	assert.ElementsMatch(t, []string{"Child B", "Child C"}, conflictFiles["shared/file2.go"])
}

func TestDetectDecompositionConflicts_EmptyChildren(t *testing.T) {
	conflicts := DetectDecompositionConflicts(nil)

	assert.Empty(t, conflicts, "expected no conflicts for empty input")
}

func TestDetectDecompositionConflicts_SingleChild(t *testing.T) {
	children := []ChildTicketSpec{
		{Title: "Child A", FilesToModify: []string{"internal/auth/login.go", "internal/auth/model.go"}},
	}

	conflicts := DetectDecompositionConflicts(children)

	assert.Empty(t, conflicts, "single child cannot have file conflicts")
}
