// internal/pipeline/dep_detector_test.go
package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetectDepChange_GoMod(t *testing.T) {
	modified := []string{"go.mod", "internal/handler.go"}
	result := DetectDepChange(modified)
	assert.True(t, result.Changed)
	assert.Equal(t, "go.mod", result.File)
	assert.Equal(t, "go", result.Command)
	assert.Equal(t, []string{"mod", "download"}, result.Args)
}

func TestDetectDepChange_PackageJSON(t *testing.T) {
	modified := []string{"src/app.ts", "package.json"}
	result := DetectDepChange(modified)
	assert.True(t, result.Changed)
	assert.Equal(t, "package.json", result.File)
	assert.Equal(t, "npm", result.Command)
}

func TestDetectDepChange_YarnLock(t *testing.T) {
	modified := []string{"yarn.lock"}
	result := DetectDepChange(modified)
	assert.True(t, result.Changed)
	assert.Equal(t, "yarn", result.Command)
}

func TestDetectDepChange_CargoToml(t *testing.T) {
	modified := []string{"Cargo.toml"}
	result := DetectDepChange(modified)
	assert.True(t, result.Changed)
	assert.Equal(t, "cargo", result.Command)
	assert.Equal(t, []string{"fetch"}, result.Args)
}

func TestDetectDepChange_RequirementsTxt(t *testing.T) {
	modified := []string{"requirements.txt"}
	result := DetectDepChange(modified)
	assert.True(t, result.Changed)
	assert.Equal(t, "pip", result.Command)
}

func TestDetectDepChange_NoDepFiles(t *testing.T) {
	modified := []string{"internal/handler.go", "internal/handler_test.go"}
	result := DetectDepChange(modified)
	assert.False(t, result.Changed)
}

func TestDetectDepChange_NestedPackageJSON(t *testing.T) {
	modified := []string{"packages/api/package.json"}
	result := DetectDepChange(modified)
	assert.True(t, result.Changed)
	assert.Equal(t, "package.json", result.File)
}
