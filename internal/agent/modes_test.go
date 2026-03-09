package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPlanModePermissions(t *testing.T) {
	mode := PlanMode()
	assert.Equal(t, ActionDeny, Evaluate("Edit", "main.go", mode.Permissions))
	assert.Equal(t, ActionDeny, Evaluate("Write", "main.go", mode.Permissions))
	assert.Equal(t, ActionDeny, Evaluate("bash", "make build", mode.Permissions))
	assert.Equal(t, ActionAllow, Evaluate("Read", "main.go", mode.Permissions))
	assert.Equal(t, ActionAllow, Evaluate("Glob", "**/*.go", mode.Permissions))
	assert.Equal(t, ActionAllow, Evaluate("Grep", "pattern", mode.Permissions))
}

func TestExploreModePermissions(t *testing.T) {
	mode := ExploreMode()
	assert.Equal(t, ActionAllow, Evaluate("Read", "main.go", mode.Permissions))
	assert.Equal(t, ActionAllow, Evaluate("Glob", "**/*.go", mode.Permissions))
	assert.Equal(t, ActionAllow, Evaluate("Grep", "pattern", mode.Permissions))
	assert.Equal(t, ActionDeny, Evaluate("Edit", "main.go", mode.Permissions))
	assert.Equal(t, ActionDeny, Evaluate("Write", "main.go", mode.Permissions))
}

func TestBuildModePermissions(t *testing.T) {
	mode := BuildMode()
	assert.Equal(t, ActionAllow, Evaluate("Edit", "main.go", mode.Permissions))
	assert.Equal(t, ActionAllow, Evaluate("bash", "go test", mode.Permissions))
}
