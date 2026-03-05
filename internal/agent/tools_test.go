package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReadTool(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.go"), []byte("package main"), 0644)

	input, _ := json.Marshal(map[string]string{"path": "test.go"})
	result, err := builtinTools["Read"](dir, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "package main" {
		t.Fatalf("unexpected result: %s", result)
	}
}

func TestReadTool_PathTraversal(t *testing.T) {
	dir := t.TempDir()
	input, _ := json.Marshal(map[string]string{"path": "../../../etc/passwd"})
	_, err := builtinTools["Read"](dir, input)
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
}

func TestReadTool_NotFound(t *testing.T) {
	dir := t.TempDir()
	input, _ := json.Marshal(map[string]string{"path": "nonexistent.go"})
	_, err := builtinTools["Read"](dir, input)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestGlobTool(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(dir, "main_test.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# readme"), 0644)

	input, _ := json.Marshal(map[string]string{"pattern": "*.go"})
	result, err := builtinTools["Glob"](dir, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Fatal("expected glob results")
	}
	// Should find both .go files but not .md
	if !contains(result, "main.go") || !contains(result, "main_test.go") {
		t.Fatalf("expected both .go files, got: %s", result)
	}
	if contains(result, "readme.md") {
		t.Fatalf("should not match .md files, got: %s", result)
	}
}

func TestGrepTool(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {\n\t// TODO: implement\n}\n"), 0644)

	input, _ := json.Marshal(map[string]string{"pattern": "TODO", "path": "."})
	result, err := builtinTools["Grep"](dir, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Fatal("expected grep results")
	}
	if !contains(result, "TODO") {
		t.Fatalf("expected TODO in results, got: %s", result)
	}
}

func TestGrepTool_NoMatch(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)

	input, _ := json.Marshal(map[string]string{"pattern": "NOTFOUND", "path": "."})
	result, err := builtinTools["Grep"](dir, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Fatalf("expected empty result, got: %s", result)
	}
}

func TestGrepTool_PathTraversal(t *testing.T) {
	dir := t.TempDir()
	input, _ := json.Marshal(map[string]string{"pattern": "test", "path": "../../.."})
	_, err := builtinTools["Grep"](dir, input)
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
}

func TestBuiltinToolDefs(t *testing.T) {
	defs := BuiltinToolDefs([]string{"Read", "Glob", "Grep"})
	if len(defs) != 3 {
		t.Fatalf("expected 3 defs, got %d", len(defs))
	}

	// Unknown tool should be silently skipped
	defs = BuiltinToolDefs([]string{"Read", "Unknown"})
	if len(defs) != 1 {
		t.Fatalf("expected 1 def (unknown skipped), got %d", len(defs))
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
