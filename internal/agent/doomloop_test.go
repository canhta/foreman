package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDoomLoopDetector(t *testing.T) {
	d := NewDoomLoopDetector(3)

	// First call — no loop
	assert.False(t, d.Check("Read", `{"path": "main.go"}`))

	// Same call again — no loop yet (need 3)
	assert.False(t, d.Check("Read", `{"path": "main.go"}`))

	// Third identical call — DOOM LOOP
	assert.True(t, d.Check("Read", `{"path": "main.go"}`))

	// Different call resets
	assert.False(t, d.Check("Write", `{"path": "main.go"}`))
}

func TestDoomLoopDetector_DifferentInputs(t *testing.T) {
	d := NewDoomLoopDetector(3)

	assert.False(t, d.Check("Read", `{"path": "a.go"}`))
	assert.False(t, d.Check("Read", `{"path": "b.go"}`))
	assert.False(t, d.Check("Read", `{"path": "c.go"}`))
	// All different inputs — no loop
}
