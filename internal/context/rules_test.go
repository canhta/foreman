// internal/context/rules_test.go
package context

import (
	"testing"
)

func TestLoadRules_Default(t *testing.T) {
	rules := LoadDirectoryRules("")
	if rules == nil {
		t.Fatal("expected non-nil rules")
	}
	if rules.TestCommand != "make test" {
		t.Errorf("expected default test command 'make test', got %q", rules.TestCommand)
	}
	if rules.LintCommand != "make lint" {
		t.Errorf("expected default lint command 'make lint', got %q", rules.LintCommand)
	}
}

func TestLoadRules_Go(t *testing.T) {
	rules := LoadDirectoryRules("go")
	if rules.TestCommand != "go test ./..." {
		t.Errorf("expected go test command, got %s", rules.TestCommand)
	}
	if rules.LintCommand != "go vet ./..." {
		t.Errorf("expected go vet command, got %s", rules.LintCommand)
	}
}

func TestLoadRules_Node(t *testing.T) {
	rules := LoadDirectoryRules("node")
	if rules.TestCommand != "npm test" {
		t.Errorf("expected npm test, got %s", rules.TestCommand)
	}
}

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		name     string
		files    []string
		expected string
	}{
		{"go", []string{"go.mod", "main.go"}, "go"},
		{"node", []string{"package.json", "index.js"}, "node"},
		{"rust", []string{"Cargo.toml", "src/main.rs"}, "rust"},
		{"python", []string{"requirements.txt", "main.py"}, "python"},
		{"unknown", []string{"unknown.xyz"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectLanguage(tt.files)
			if got != tt.expected {
				t.Errorf("DetectLanguage(%v) = %q, want %q", tt.files, got, tt.expected)
			}
		})
	}
}
