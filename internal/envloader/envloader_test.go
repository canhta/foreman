package envloader_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/canhta/foreman/internal/envloader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_SetsEnvVars(t *testing.T) {
	f := filepath.Join(t.TempDir(), ".env")
	require.NoError(t, os.WriteFile(f, []byte("FOO=bar\nBAZ=qux\n"), 0o600))

	t.Setenv("FOO", "")
	t.Setenv("BAZ", "")

	require.NoError(t, envloader.Load(map[string]string{".env": f}))
	assert.Equal(t, "bar", os.Getenv("FOO"))
	assert.Equal(t, "qux", os.Getenv("BAZ"))
}

func TestLoad_IgnoresCommentsAndBlanks(t *testing.T) {
	f := filepath.Join(t.TempDir(), ".env")
	content := "# comment\n\nKEY=value\n"
	require.NoError(t, os.WriteFile(f, []byte(content), 0o600))

	t.Setenv("KEY", "")
	require.NoError(t, envloader.Load(map[string]string{".env": f}))
	assert.Equal(t, "value", os.Getenv("KEY"))
}

func TestLoad_StripsQuotes(t *testing.T) {
	f := filepath.Join(t.TempDir(), ".env")
	require.NoError(t, os.WriteFile(f, []byte(`QUOTED="hello world"`+"\n"), 0o600))

	t.Setenv("QUOTED", "")
	require.NoError(t, envloader.Load(map[string]string{".env": f}))
	assert.Equal(t, "hello world", os.Getenv("QUOTED"))
}

func TestLoad_MissingFileIsSkipped(t *testing.T) {
	err := envloader.Load(map[string]string{".env": "/does/not/exist/.env"})
	assert.NoError(t, err)
}

func TestCopyInto_CopiesFilesToWorktree(t *testing.T) {
	src := filepath.Join(t.TempDir(), "project.env")
	require.NoError(t, os.WriteFile(src, []byte("KEY=val\n"), 0o600))

	worktreeDir := t.TempDir()
	require.NoError(t, envloader.CopyInto(map[string]string{".env": src}, worktreeDir))

	dest := filepath.Join(worktreeDir, ".env")
	data, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Equal(t, "KEY=val\n", string(data))
}

func TestCopyInto_CreatesSubdirs(t *testing.T) {
	src := filepath.Join(t.TempDir(), "api.env")
	require.NoError(t, os.WriteFile(src, []byte("DB=postgres\n"), 0o600))

	worktreeDir := t.TempDir()
	require.NoError(t, envloader.CopyInto(map[string]string{"packages/api/.env": src}, worktreeDir))

	dest := filepath.Join(worktreeDir, "packages", "api", ".env")
	_, err := os.Stat(dest)
	assert.NoError(t, err)
}

func TestCopyInto_MissingSourceIsSkipped(t *testing.T) {
	err := envloader.CopyInto(map[string]string{".env": "/no/such/file"}, t.TempDir())
	assert.NoError(t, err)
}
