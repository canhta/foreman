package mcp_test

import (
	"testing"

	"github.com/canhta/foreman/internal/agent/mcp"
	"github.com/stretchr/testify/assert"
)

func TestMCPServerConfig_EffectiveDefaults(t *testing.T) {
	cfg := mcp.MCPServerConfig{Name: "test"}
	assert.Equal(t, "none", cfg.EffectiveRestartPolicy())
	assert.Equal(t, 3, cfg.EffectiveMaxRestarts())
	assert.Equal(t, 2, cfg.EffectiveRestartDelaySecs())
}

func TestMCPServerConfig_EffectiveOverrides(t *testing.T) {
	maxR := 5
	delayR := 10
	cfg := mcp.MCPServerConfig{
		Name:             "test",
		RestartPolicy:    "always",
		MaxRestarts:      &maxR,
		RestartDelaySecs: &delayR,
	}
	assert.Equal(t, "always", cfg.EffectiveRestartPolicy())
	assert.Equal(t, 5, cfg.EffectiveMaxRestarts())
	assert.Equal(t, 10, cfg.EffectiveRestartDelaySecs())
}

func TestMCPServerConfig_EnvField(t *testing.T) {
	cfg := mcp.MCPServerConfig{
		Name: "test",
		Env:  map[string]string{"API_KEY": "secret"},
	}
	assert.Equal(t, "secret", cfg.Env["API_KEY"])
}
