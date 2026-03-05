package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRunCmd_Exists(t *testing.T) {
	cmd := newRunCmd()
	assert.Equal(t, "run", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
}

func TestRunCmd_HasDryRunFlag(t *testing.T) {
	cmd := newRunCmd()
	flag := cmd.Flags().Lookup("dry-run")
	assert.NotNil(t, flag)
}

func TestRunCmd_RequiresArgs(t *testing.T) {
	cmd := newRunCmd()
	// cobra.ExactArgs(1) means 0 args should error
	err := cmd.Args(cmd, []string{})
	assert.Error(t, err)
}

func TestRunCmd_AcceptsOneArg(t *testing.T) {
	cmd := newRunCmd()
	err := cmd.Args(cmd, []string{"PROJ-123"})
	assert.NoError(t, err)
}
