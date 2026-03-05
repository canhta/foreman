package integration

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFixturesExist(t *testing.T) {
	fixtures := []string{
		"../fixtures/sample_repo/main.go",
		"../fixtures/sample_repo/go.mod",
		"../fixtures/sample_tickets/LOCAL-1.json",
	}
	for _, f := range fixtures {
		path := filepath.Join(".", f)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("fixture missing: %s", f)
		}
	}
}
