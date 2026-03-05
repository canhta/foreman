package context

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestObservationLog_AppendAndRead(t *testing.T) {
	dir := t.TempDir()

	log := NewObservationLog(dir)

	obs := Observation{
		Type:    "test_failure",
		Details: map[string]string{"test": "TestFoo", "error": "expected 1 got 2"},
		File:    "pkg/foo.go",
		Time:    time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
	}

	err := log.Append(obs)
	require.NoError(t, err)

	observations, newCursor, err := log.ReadFrom(0)
	require.NoError(t, err)
	require.Len(t, observations, 1)
	assert.Equal(t, "test_failure", observations[0].Type)
	assert.Equal(t, "TestFoo", observations[0].Details["test"])
	assert.Equal(t, "pkg/foo.go", observations[0].File)
	assert.True(t, newCursor > 0, "cursor should advance")
}

func TestObservationLog_ReadFromCursor(t *testing.T) {
	dir := t.TempDir()

	log := NewObservationLog(dir)

	// Append two observations
	require.NoError(t, log.Append(Observation{
		Type: "first",
		Time: time.Now(),
	}))

	_, cursor1, err := log.ReadFrom(0)
	require.NoError(t, err)

	require.NoError(t, log.Append(Observation{
		Type: "second",
		Time: time.Now(),
	}))

	// Read from cursor should only return the second
	observations, _, err := log.ReadFrom(cursor1)
	require.NoError(t, err)
	require.Len(t, observations, 1)
	assert.Equal(t, "second", observations[0].Type)
}

func TestObservationLog_ReadFromEmpty(t *testing.T) {
	dir := t.TempDir()

	log := NewObservationLog(dir)

	observations, cursor, err := log.ReadFrom(0)
	require.NoError(t, err)
	assert.Empty(t, observations)
	assert.Equal(t, int64(0), cursor)
}

func TestObservationLog_CreatesDirectory(t *testing.T) {
	dir := t.TempDir()

	log := NewObservationLog(dir)
	err := log.Append(Observation{Type: "test", Time: time.Now()})
	require.NoError(t, err)

	// Verify .foreman directory was created
	assert.DirExists(t, filepath.Join(dir, ".foreman"))
}
