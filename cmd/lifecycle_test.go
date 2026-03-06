package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStartCmd_Exists(t *testing.T) {
	cmd := newStartCmd()
	assert.Equal(t, "start", cmd.Use)
}

func TestStopCmd_Exists(t *testing.T) {
	cmd := newStopCmd()
	assert.Equal(t, "stop", cmd.Use)
}

func TestStatusCmd_Exists(t *testing.T) {
	cmd := newStatusCmd()
	assert.Equal(t, "status", cmd.Use)
}
