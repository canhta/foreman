package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSkillInjector_Inject_CreatesFiles(t *testing.T) {
	workDir := t.TempDir()
	si := NewSkillInjector(SkillInjectorConfig{
		TestCommand: "go test ./...",
		Language:    "Go",
	})

	err := si.Inject(workDir)
	require.NoError(t, err)

	// settings.json should exist
	data, err := os.ReadFile(filepath.Join(workDir, ".claude", "settings.json"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "permissions")

	// TDD skill should exist under foreman namespace
	data, err = os.ReadFile(filepath.Join(workDir, ".claude", "foreman", "skills", "tdd.md"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "go test ./...")
	assert.NotContains(t, string(data), "{{.TestCommand}}")
}

func TestSkillInjector_Inject_MergesExistingSettings(t *testing.T) {
	workDir := t.TempDir()
	claudeDir := filepath.Join(workDir, ".claude")
	require.NoError(t, os.MkdirAll(claudeDir, 0o755))

	existing := map[string]interface{}{
		"model": "sonnet",
		"hooks": map[string]interface{}{
			"PreToolUse": []interface{}{"existing-hook"},
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(claudeDir, "settings.json"), data, 0o644))

	si := NewSkillInjector(SkillInjectorConfig{TestCommand: "npm test", Language: "TypeScript"})
	require.NoError(t, si.Inject(workDir))

	merged, err := os.ReadFile(filepath.Join(claudeDir, "settings.json"))
	require.NoError(t, err)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(merged, &result))

	// Existing fields preserved
	assert.Equal(t, "sonnet", result["model"])
	// Foreman hooks merged in
	hooks := result["hooks"].(map[string]interface{})
	assert.Contains(t, hooks, "PreToolUse")
	assert.Contains(t, hooks, "UserPromptSubmit")
}

func TestSkillInjector_Inject_NoExistingClaudeDir(t *testing.T) {
	workDir := t.TempDir()
	si := NewSkillInjector(SkillInjectorConfig{TestCommand: "pytest", Language: "Python"})

	err := si.Inject(workDir)
	require.NoError(t, err)

	assert.DirExists(t, filepath.Join(workDir, ".claude"))
	assert.DirExists(t, filepath.Join(workDir, ".claude", "foreman"))
}

func TestSkillInjector_Cleanup_RemovesForemanFiles(t *testing.T) {
	workDir := t.TempDir()
	si := NewSkillInjector(SkillInjectorConfig{TestCommand: "go test", Language: "Go"})

	require.NoError(t, si.Inject(workDir))
	assert.DirExists(t, filepath.Join(workDir, ".claude", "foreman"))

	si.Cleanup(workDir)
	assert.NoDirExists(t, filepath.Join(workDir, ".claude", "foreman"))
	// settings.json should remain (may have user config)
	assert.FileExists(t, filepath.Join(workDir, ".claude", "settings.json"))
}
