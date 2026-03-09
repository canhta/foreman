// internal/pipeline/testhelper_registry_test.go
//
// mustLoadTestRegistry loads the real prompts registry from the project root.
// All pipeline tests that exercise reviewer/implementer prompt paths call this
// helper so they use the new registry instead of the removed .md.j2 fallback.
package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/canhta/foreman/internal/prompts"
)

// mustLoadTestRegistry finds the project root (go.mod) and returns a Registry
// loaded from the prompts/ directory. The test is fatally failed on any error.
func mustLoadTestRegistry(t *testing.T) *prompts.Registry {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("mustLoadTestRegistry: getwd: %v", err)
	}
	// Walk up to the directory containing go.mod.
	root := dir
	for {
		if _, statErr := os.Stat(filepath.Join(root, "go.mod")); statErr == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatalf("mustLoadTestRegistry: could not find project root (go.mod) from %s", dir)
		}
		root = parent
	}
	reg, err := prompts.Load(filepath.Join(root, "prompts"))
	if err != nil {
		t.Fatalf("mustLoadTestRegistry: prompts.Load: %v", err)
	}
	return reg
}
