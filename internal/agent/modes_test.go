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

func TestModeReadOnlyFlag(t *testing.T) {
	assert.True(t, PlanMode().ReadOnly, "PlanMode should be ReadOnly")
	assert.True(t, ExploreMode().ReadOnly, "ExploreMode should be ReadOnly")
	assert.False(t, BuildMode().ReadOnly, "BuildMode should not be ReadOnly")
}

func TestModeMaxTurns(t *testing.T) {
	assert.Greater(t, PlanMode().MaxTurns, 0)
	assert.Greater(t, ExploreMode().MaxTurns, 0)
	assert.Greater(t, BuildMode().MaxTurns, 0)
}

func TestLookupMode_Known(t *testing.T) {
	for _, name := range []string{"plan", "explore", "build"} {
		mode, ok := LookupMode(name)
		assert.True(t, ok, "LookupMode(%q) should be found", name)
		assert.Equal(t, name, mode.Name)
	}
}

func TestLookupMode_Unknown(t *testing.T) {
	_, ok := LookupMode("nonexistent")
	assert.False(t, ok)
}
