package command

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistryAddAndGet(t *testing.T) {
	r := NewRegistry()

	r.Register(Command{
		Name:        "review",
		Description: "Review changes",
		Template:    "Review the following diff:\n$ARGUMENTS",
		Subtask:     true,
	})

	cmd, err := r.Get("review")
	require.NoError(t, err)
	assert.Equal(t, "review", cmd.Name)
	assert.True(t, cmd.Subtask)
}

func TestRegistryRender(t *testing.T) {
	r := NewRegistry()
	r.Register(Command{
		Name:     "review",
		Template: "Review changes for $1 in branch $2",
	})

	result, err := r.Render("review", "auth module", "feature/auth")
	require.NoError(t, err)
	assert.Contains(t, result, "auth module")
	assert.Contains(t, result, "feature/auth")
}

func TestRegistryRenderArguments(t *testing.T) {
	r := NewRegistry()
	r.Register(Command{
		Name:     "explain",
		Template: "Explain the following:\n$ARGUMENTS",
	})

	result, err := r.Render("explain", "how does the pipeline work?")
	require.NoError(t, err)
	assert.Contains(t, result, "how does the pipeline work?")
}

func TestRegistryList(t *testing.T) {
	r := NewRegistry()
	r.Register(Command{Name: "a", Description: "aaa", Template: "a"})
	r.Register(Command{Name: "b", Description: "bbb", Template: "b"})

	cmds := r.List()
	assert.Len(t, cmds, 2)
}
