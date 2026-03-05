package cmd

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContextCmd_Exists(t *testing.T) {
	cmd := newContextCmd()
	assert.Equal(t, "context", cmd.Use)
	assert.NotEmpty(t, cmd.Short)

	// Should have "generate" subcommand
	found := false
	for _, sub := range cmd.Commands() {
		if sub.Name() == "generate" {
			found = true
			break
		}
	}
	assert.True(t, found, "context should have generate subcommand")
}

func TestContextGenerateCmd_HasFlags(t *testing.T) {
	cmd := newContextCmd()
	var genCmd *cobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Name() == "generate" {
			genCmd = sub
			break
		}
	}
	require.NotNil(t, genCmd)

	assert.NotNil(t, genCmd.Flags().Lookup("offline"))
	assert.NotNil(t, genCmd.Flags().Lookup("dry-run"))
	assert.NotNil(t, genCmd.Flags().Lookup("force"))
	assert.NotNil(t, genCmd.Flags().Lookup("output"))
}

func TestContextUpdateCmd_Exists(t *testing.T) {
	cmd := newContextCmd()
	found := false
	for _, sub := range cmd.Commands() {
		if sub.Name() == "update" {
			found = true
			break
		}
	}
	assert.True(t, found, "context should have update subcommand")
}
