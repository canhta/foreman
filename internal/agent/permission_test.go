package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPermission_ExactAllow(t *testing.T) {
	rules := Ruleset{
		{Permission: "read", Pattern: "*", Action: ActionAllow},
	}
	assert.Equal(t, ActionAllow, Evaluate("read", "main.go", rules))
}

func TestPermission_DenyPattern(t *testing.T) {
	rules := Ruleset{
		{Permission: "read", Pattern: "*", Action: ActionAllow},
		{Permission: "read", Pattern: ".env*", Action: ActionDeny},
	}
	assert.Equal(t, ActionAllow, Evaluate("read", "main.go", rules))
	assert.Equal(t, ActionDeny, Evaluate("read", ".env", rules))
	assert.Equal(t, ActionDeny, Evaluate("read", ".env.local", rules))
}

func TestPermission_WildcardPermission(t *testing.T) {
	rules := Ruleset{
		{Permission: "*", Pattern: "*", Action: ActionAllow},
		{Permission: "bash", Pattern: "*", Action: ActionDeny},
	}
	assert.Equal(t, ActionAllow, Evaluate("read", "main.go", rules))
	assert.Equal(t, ActionDeny, Evaluate("bash", "rm -rf /", rules))
}

func TestPermission_EditToolMapping(t *testing.T) {
	rules := Ruleset{
		{Permission: "edit", Pattern: "*.go", Action: ActionAllow},
		{Permission: "edit", Pattern: "*.yml", Action: ActionDeny},
	}
	assert.Equal(t, ActionAllow, Evaluate("Write", "main.go", rules))
	assert.Equal(t, ActionAllow, Evaluate("Edit", "main.go", rules))
	assert.Equal(t, ActionAllow, Evaluate("MultiEdit", "main.go", rules))
	assert.Equal(t, ActionDeny, Evaluate("Write", "config.yml", rules))
}

func TestPermission_DefaultDeny(t *testing.T) {
	rules := Ruleset{}
	assert.Equal(t, ActionDeny, Evaluate("bash", "anything", rules))
}

func TestPermission_LastRuleWins(t *testing.T) {
	rules := Ruleset{
		{Permission: "read", Pattern: "*", Action: ActionDeny},
		{Permission: "read", Pattern: "*", Action: ActionAllow},
	}
	assert.Equal(t, ActionAllow, Evaluate("read", "main.go", rules))
}

func TestPermission_MergeRulesets(t *testing.T) {
	defaults := Ruleset{
		{Permission: "*", Pattern: "*", Action: ActionAllow},
	}
	agentRules := Ruleset{
		{Permission: "bash", Pattern: "*", Action: ActionDeny},
	}
	merged := Merge(defaults, agentRules)
	assert.Equal(t, ActionAllow, Evaluate("read", "main.go", merged))
	assert.Equal(t, ActionDeny, Evaluate("bash", "rm -rf", merged))
}
