package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoverSkills_ProjectDir(t *testing.T) {
	root := t.TempDir()
	createSkillFile(t, filepath.Join(root, "skills", "lint.yml"), "lint")
	createSkillFile(t, filepath.Join(root, ".foreman", "skills", "review.yml"), "review")

	paths := DiscoverSkillPaths(root, "")
	assert.GreaterOrEqual(t, len(paths), 2)
}

func TestDiscoverSkills_Hierarchy(t *testing.T) {
	root := t.TempDir()
	subdir := filepath.Join(root, "packages", "api")
	require.NoError(t, os.MkdirAll(subdir, 0o755))

	createSkillFile(t, filepath.Join(root, "skills", "root.yml"), "root")
	createSkillFile(t, filepath.Join(subdir, ".foreman", "skills", "local.yml"), "local")

	paths := DiscoverSkillPaths(root, subdir)
	assert.GreaterOrEqual(t, len(paths), 2)
}

func TestDiscoverSkills_AdditionalPaths(t *testing.T) {
	root := t.TempDir()
	extra := t.TempDir()
	createSkillFile(t, filepath.Join(extra, "custom.yml"), "custom")

	paths := DiscoverSkillPaths(root, "", extra)
	found := false
	for _, p := range paths {
		if filepath.Base(p) == "custom.yml" {
			found = true
		}
	}
	assert.True(t, found)
}

func TestDiscoverSkills_EmptyDir(t *testing.T) {
	root := t.TempDir()
	paths := DiscoverSkillPaths(root, "")
	assert.Empty(t, paths)
}

func TestDiscoverSkills_Deduplication(t *testing.T) {
	root := t.TempDir()
	createSkillFile(t, filepath.Join(root, "skills", "lint.yml"), "lint")

	// Pass the same extra path twice — should not deduplicate, but the file should only appear once
	extra := filepath.Join(root, "skills")
	paths := DiscoverSkillPaths(root, "", extra)
	count := 0
	for _, p := range paths {
		if filepath.Base(p) == "lint.yml" {
			count++
		}
	}
	assert.Equal(t, 1, count, "lint.yml should appear exactly once")
}

func createSkillFile(t *testing.T, path, id string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	content := fmt.Sprintf("id: %s\ndescription: test\ntrigger: post_lint\nsteps: []\n", id)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}
