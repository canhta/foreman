// internal/context/repo_analyzer_test.go
package context

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnalyzeRepo_GoProject(t *testing.T) {
	workDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "go.mod"), []byte("module example.com/foo\ngo 1.23"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "Makefile"), []byte("test:\n\tgo test ./..."), 0o644))

	info, err := AnalyzeRepo(workDir)
	require.NoError(t, err)
	assert.Equal(t, "go", info.Language)
	assert.Contains(t, info.BuildCmd, "go")
	assert.Contains(t, info.TestCmd, "go test")
}

func TestAnalyzeRepo_NodeProject(t *testing.T) {
	workDir := t.TempDir()
	packageJSON := `{"name": "test", "scripts": {"test": "jest", "lint": "eslint .", "build": "tsc"}}`
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "package.json"), []byte(packageJSON), 0o644))

	info, err := AnalyzeRepo(workDir)
	require.NoError(t, err)
	assert.Equal(t, "typescript", info.Language)
	assert.Contains(t, info.TestCmd, "npm test")
	assert.Contains(t, info.LintCmd, "npm run lint")
}

func TestAnalyzeRepo_PythonProject(t *testing.T) {
	workDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "requirements.txt"), []byte("flask\npytest"), 0o644))

	info, err := AnalyzeRepo(workDir)
	require.NoError(t, err)
	assert.Equal(t, "python", info.Language)
	assert.Contains(t, info.TestCmd, "pytest")
}

func TestAnalyzeRepo_ForemanContext(t *testing.T) {
	workDir := t.TempDir()
	contextMD := `# Foreman Context

## Commands
- Test: ` + "`npm run test:unit`" + `
- Lint: ` + "`npm run lint`" + `
- Build: ` + "`npm run build`" + `
`
	require.NoError(t, os.WriteFile(filepath.Join(workDir, ".foreman-context.md"), []byte(contextMD), 0o644))

	info, err := AnalyzeRepo(workDir)
	require.NoError(t, err)
	assert.Equal(t, "npm run test:unit", info.TestCmd)
	assert.Equal(t, "npm run lint", info.LintCmd)
	assert.Equal(t, "npm run build", info.BuildCmd)
	// No language-indicating files in temp dir, so language should be "unknown"
	assert.Equal(t, "unknown", info.Language)
}

func TestAnalyzeRepo_EmptyDir(t *testing.T) {
	workDir := t.TempDir()
	info, err := AnalyzeRepo(workDir)
	require.NoError(t, err)
	assert.Equal(t, "unknown", info.Language)
}

func TestAnalyzeRepo_FileTree(t *testing.T) {
	workDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(workDir, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "src/main.go"), []byte("package main"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "go.mod"), []byte("module test"), 0o644))

	info, err := AnalyzeRepo(workDir)
	require.NoError(t, err)
	assert.Contains(t, info.FileTree, "src/")
	assert.Contains(t, info.FileTree, "go.mod")
}
