package cmd

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMigrateFromPostgresCmd_Defaults(t *testing.T) {
	cmd := newMigrateFromPostgresCmd()
	require.NotNil(t, cmd)

	home, err := os.UserHomeDir()
	require.NoError(t, err)

	flag := cmd.Flags().Lookup("output-dir")
	require.NotNil(t, flag)
	assert.Equal(t, filepath.Join(home, ".foreman", "projects", "migrated"), flag.DefValue)
	assert.Equal(t, "migrate-from-postgres", cmd.Use)
}

func TestNewMigrateFromPostgresCmd_RunE(t *testing.T) {
	tmpDir := t.TempDir()
	chdirForTest(t, tmpDir)
	require.NoError(t, os.WriteFile("postgres_dump.sql", []byte("-- test dump"), 0o644))

	outputDir := filepath.Join(tmpDir, "runecmd")
	cmd := newMigrateFromPostgresCmd()
	require.NoError(t, cmd.Flags().Set("output-dir", outputDir))

	_, err := captureStdoutAndRun(t, func() error {
		return cmd.RunE(cmd, nil)
	})
	require.NoError(t, err)

	info, statErr := os.Stat(outputDir)
	require.NoError(t, statErr)
	assert.True(t, info.IsDir())
}

func TestRunMigrateFromPostgres_MissingDumpReturnsError(t *testing.T) {
	tmpDir := t.TempDir()
	chdirForTest(t, tmpDir)

	outputDir := filepath.Join(tmpDir, "projects", "migrated")
	stdout, err := captureStdoutAndRun(t, func() error {
		return runMigrateFromPostgres(outputDir)
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "postgres_dump.sql not found")
	assert.Contains(t, stdout, `ERROR: dump file "postgres_dump.sql" not found`)
}

func TestRunMigrateFromPostgres_CreatesOutputDirectoryWhenDumpExists(t *testing.T) {
	tmpDir := t.TempDir()
	chdirForTest(t, tmpDir)

	require.NoError(t, os.WriteFile("postgres_dump.sql", []byte("-- test dump"), 0o644))

	outputDir := filepath.Join(tmpDir, "nested", "project")
	_, err := captureStdoutAndRun(t, func() error {
		return runMigrateFromPostgres(outputDir)
	})

	require.NoError(t, err)

	info, statErr := os.Stat(outputDir)
	require.NoError(t, statErr)
	assert.True(t, info.IsDir())
}

func TestRunMigrateFromPostgres_PrintsGuidanceTextForNextSteps(t *testing.T) {
	tmpDir := t.TempDir()
	chdirForTest(t, tmpDir)

	require.NoError(t, os.WriteFile("postgres_dump.sql", []byte("-- test dump"), 0o644))

	outputDir := filepath.Join(tmpDir, "project")
	stdout, err := captureStdoutAndRun(t, func() error {
		return runMigrateFromPostgres(outputDir)
	})

	require.NoError(t, err)
	assert.Contains(t, stdout, "Next steps:")
	assert.Contains(t, stdout, "foreman project create --dir "+outputDir)
	assert.Contains(t, stdout, "Migration guidance complete. No data has been modified.")
}

func TestRunMigrateFromPostgres_OutputIncludesForemanDBPath(t *testing.T) {
	tmpDir := t.TempDir()
	chdirForTest(t, tmpDir)

	require.NoError(t, os.WriteFile("postgres_dump.sql", []byte("-- test dump"), 0o644))

	outputDir := filepath.Join(tmpDir, "project")
	dbPath := filepath.Join(outputDir, "foreman.db")

	stdout, err := captureStdoutAndRun(t, func() error {
		return runMigrateFromPostgres(outputDir)
	})

	require.NoError(t, err)
	assert.Contains(t, stdout, "Target SQLite database   : "+dbPath)
	assert.Contains(t, stdout, "pgloader postgres_dump.sql sqlite://"+dbPath)
	assert.Contains(t, stdout, "sqlite3 "+dbPath+" < postgres_dump.sql")
}

func TestRunMigrateFromPostgres_OutputDirCreationFails(t *testing.T) {
	tmpDir := t.TempDir()
	chdirForTest(t, tmpDir)

	require.NoError(t, os.WriteFile("postgres_dump.sql", []byte("-- test dump"), 0o644))

	filePath := filepath.Join(tmpDir, "not-a-directory")
	require.NoError(t, os.WriteFile(filePath, []byte("x"), 0o644))

	_, err := captureStdoutAndRun(t, func() error {
		return runMigrateFromPostgres(filepath.Join(filePath, "child"))
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "create output directory")
}

func chdirForTest(t *testing.T, dir string) {
	t.Helper()

	oldWD, err := os.Getwd()
	require.NoError(t, err)

	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})
}

func captureStdoutAndRun(t *testing.T, fn func() error) (string, error) {
	t.Helper()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)

	os.Stdout = w
	runErr := fn()

	require.NoError(t, w.Close())
	os.Stdout = oldStdout

	out, readErr := io.ReadAll(r)
	require.NoError(t, readErr)
	require.NoError(t, r.Close())

	return string(out), runErr
}
