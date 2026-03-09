package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDiffTracker_RecordChange(t *testing.T) {
	dt := NewDiffTracker()
	dt.RecordChange("main.go", ChangeModified, 10, 5)
	dt.RecordChange("util.go", ChangeCreated, 20, 0)
	dt.RecordChange("main.go", ChangeModified, 3, 2) // second edit to same file
	summary := dt.Summary()
	assert.Equal(t, 2, summary.FilesChanged)
	assert.Equal(t, 33, summary.TotalAdditions)
	assert.Equal(t, 7, summary.TotalDeletions)
}

func TestDiffTracker_FileList(t *testing.T) {
	dt := NewDiffTracker()
	dt.RecordChange("a.go", ChangeModified, 1, 0)
	dt.RecordChange("b.go", ChangeCreated, 5, 0)
	dt.RecordChange("c.go", ChangeDeleted, 0, 10)
	files := dt.ChangedFiles()
	assert.Len(t, files, 3)
}
