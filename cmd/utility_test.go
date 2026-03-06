package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCostCmd_Exists(t *testing.T) {
	cmd := newCostCmd()
	assert.Equal(t, "cost [today|week|month|per-ticket]", cmd.Use)
}

func TestCostCmd_AcceptsSubcommand(t *testing.T) {
	cmd := newCostCmd()
	err := cmd.Args(cmd, []string{"today"})
	assert.NoError(t, err)
}

func TestCostCmd_RejectsZeroArgs(t *testing.T) {
	cmd := newCostCmd()
	err := cmd.Args(cmd, []string{})
	assert.Error(t, err)
}

func TestPsCmd_Exists(t *testing.T) {
	cmd := newPsCmd()
	assert.Equal(t, "ps", cmd.Use)
	flag := cmd.Flags().Lookup("all")
	assert.NotNil(t, flag)
}

func TestDoctorCmd_Exists(t *testing.T) {
	cmd := newDoctorCmd()
	assert.Equal(t, "doctor", cmd.Use)
}
