package tools_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/canhta/foreman/internal/agent/tools"
)

func newFSRegistry(t *testing.T) (*tools.Registry, string) {
	t.Helper()
	dir := t.TempDir()
	reg := tools.NewRegistry(nil, nil, tools.ToolHooks{})
	return reg, dir
}

func execTool(t *testing.T, reg *tools.Registry, dir, name string, input any) string {
	t.Helper()
	b, _ := json.Marshal(input)
	out, err := reg.Execute(context.Background(), dir, name, b)
	if err != nil {
		t.Fatalf("%s: unexpected error: %v", name, err)
	}
	return out
}

func TestRead_Basic(t *testing.T) {
	reg, dir := newFSRegistry(t)
	os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello world"), 0644)
	out := execTool(t, reg, dir, "Read", map[string]string{"path": "hello.txt"})
	if !strings.Contains(out, "hello world") {
		t.Errorf("expected content, got %q", out)
	}
}

func TestRead_LineRange(t *testing.T) {
	reg, dir := newFSRegistry(t)
	os.WriteFile(filepath.Join(dir, "lines.txt"), []byte("line1\nline2\nline3\nline4\n"), 0644)
	out := execTool(t, reg, dir, "Read", map[string]any{"path": "lines.txt", "start_line": 2, "end_line": 3})
	if !strings.Contains(out, "line2") || strings.Contains(out, "line4") {
		t.Errorf("expected lines 2-3, got %q", out)
	}
}

func TestRead_PathTraversal(t *testing.T) {
	reg, dir := newFSRegistry(t)
	b, _ := json.Marshal(map[string]string{"path": "../../etc/passwd"})
	_, err := reg.Execute(context.Background(), dir, "Read", b)
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
}

func TestWrite_CreatesFile(t *testing.T) {
	reg, dir := newFSRegistry(t)
	execTool(t, reg, dir, "Write", map[string]string{"path": "out.txt", "content": "written"})
	data, err := os.ReadFile(filepath.Join(dir, "out.txt"))
	if err != nil || string(data) != "written" {
		t.Errorf("expected file to be written, got err=%v content=%q", err, data)
	}
}

func TestWrite_CreatesSubdirectory(t *testing.T) {
	reg, dir := newFSRegistry(t)
	execTool(t, reg, dir, "Write", map[string]string{"path": "sub/dir/file.txt", "content": "nested"})
	data, _ := os.ReadFile(filepath.Join(dir, "sub/dir/file.txt"))
	if string(data) != "nested" {
		t.Errorf("expected nested file, got %q", data)
	}
}

func TestWrite_BlocksForbiddenPath(t *testing.T) {
	reg, dir := newFSRegistry(t)
	b, _ := json.Marshal(map[string]string{"path": ".env", "content": "SECRET=foo"})
	_, err := reg.Execute(context.Background(), dir, "Write", b)
	if err == nil {
		t.Fatal("expected error writing to .env")
	}
}

func TestEdit_ReplacesString(t *testing.T) {
	reg, dir := newFSRegistry(t)
	os.WriteFile(filepath.Join(dir, "edit.txt"), []byte("hello world"), 0644)
	execTool(t, reg, dir, "Edit", map[string]string{"path": "edit.txt", "old_string": "world", "new_string": "Go"})
	data, _ := os.ReadFile(filepath.Join(dir, "edit.txt"))
	if string(data) != "hello Go" {
		t.Errorf("expected 'hello Go', got %q", data)
	}
}

func TestEdit_OldStringNotFound(t *testing.T) {
	reg, dir := newFSRegistry(t)
	os.WriteFile(filepath.Join(dir, "edit.txt"), []byte("hello world"), 0644)
	b, _ := json.Marshal(map[string]string{"path": "edit.txt", "old_string": "missing", "new_string": "x"})
	_, err := reg.Execute(context.Background(), dir, "Edit", b)
	if err == nil {
		t.Fatal("expected error when old_string not found")
	}
}

func TestMultiEdit_AppliesAll(t *testing.T) {
	reg, dir := newFSRegistry(t)
	os.WriteFile(filepath.Join(dir, "m.txt"), []byte("aaa bbb ccc"), 0644)
	execTool(t, reg, dir, "MultiEdit", map[string]any{
		"path": "m.txt",
		"edits": []map[string]string{
			{"old_string": "aaa", "new_string": "AAA"},
			{"old_string": "bbb", "new_string": "BBB"},
		},
	})
	data, _ := os.ReadFile(filepath.Join(dir, "m.txt"))
	if string(data) != "AAA BBB ccc" {
		t.Errorf("expected 'AAA BBB ccc', got %q", data)
	}
}

func TestListDir_Basic(t *testing.T) {
	reg, dir := newFSRegistry(t)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0644)
	out := execTool(t, reg, dir, "ListDir", map[string]string{"path": "."})
	if !strings.Contains(out, "a.txt") || !strings.Contains(out, "b.txt") {
		t.Errorf("expected both files listed, got %q", out)
	}
}

func TestGlob_Basic(t *testing.T) {
	reg, dir := newFSRegistry(t)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte(""), 0644)
	os.WriteFile(filepath.Join(dir, "main_test.go"), []byte(""), 0644)
	os.WriteFile(filepath.Join(dir, "readme.md"), []byte(""), 0644)
	out := execTool(t, reg, dir, "Glob", map[string]string{"pattern": "*.go"})
	if !strings.Contains(out, "main.go") || strings.Contains(out, "readme.md") {
		t.Errorf("expected only .go files, got %q", out)
	}
}

func TestGlob_DoubleStarRecursive(t *testing.T) {
	reg, dir := newFSRegistry(t)
	sub := filepath.Join(dir, "pkg", "sub")
	os.MkdirAll(sub, 0755)
	os.WriteFile(filepath.Join(sub, "deep.go"), []byte(""), 0644)
	out := execTool(t, reg, dir, "Glob", map[string]string{"pattern": "**/*.go"})
	if !strings.Contains(out, "deep.go") {
		t.Errorf("expected deep.go in recursive glob, got %q", out)
	}
}

func TestGrep_Basic(t *testing.T) {
	reg, dir := newFSRegistry(t)
	os.WriteFile(filepath.Join(dir, "src.go"), []byte("package main\n\nfunc Hello() {}\n"), 0644)
	out := execTool(t, reg, dir, "Grep", map[string]string{"pattern": "func Hello", "path": "."})
	if !strings.Contains(out, "src.go") {
		t.Errorf("expected match in src.go, got %q", out)
	}
}

func TestGrep_CaseInsensitive(t *testing.T) {
	reg, dir := newFSRegistry(t)
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("Hello World\n"), 0644)
	out := execTool(t, reg, dir, "Grep", map[string]any{"pattern": "hello", "path": ".", "case_sensitive": false})
	if !strings.Contains(out, "f.txt") {
		t.Errorf("expected case-insensitive match, got %q", out)
	}
}

func TestReadRange_ReturnsLineRange(t *testing.T) {
	reg, dir := newFSRegistry(t)
	var lines []string
	for i := 1; i <= 100; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	content := strings.Join(lines, "\n")
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte(content), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Test basic range
	result := execTool(t, reg, dir, "ReadRange", map[string]any{"path": "test.txt", "start_line": 10, "end_line": 20})
	if !strings.Contains(result, "line 10") {
		t.Errorf("expected 'line 10' in result, got %q", result)
	}
	if !strings.Contains(result, "line 20") {
		t.Errorf("expected 'line 20' in result, got %q", result)
	}
	if strings.Contains(result, "line 9") {
		t.Errorf("should not contain 'line 9', got %q", result)
	}
	if strings.Contains(result, "line 21") {
		t.Errorf("should not contain 'line 21', got %q", result)
	}

	// Test end_line beyond file — should return up to EOF without error
	result = execTool(t, reg, dir, "ReadRange", map[string]any{"path": "test.txt", "start_line": 90, "end_line": 200})
	if !strings.Contains(result, "line 90") {
		t.Errorf("expected 'line 90', got %q", result)
	}
	if !strings.Contains(result, "line 100") {
		t.Errorf("expected 'line 100', got %q", result)
	}

	// Test start_line < 1 — should error
	b, _ := json.Marshal(map[string]any{"path": "test.txt", "start_line": 0, "end_line": 5})
	_, err := reg.Execute(context.Background(), dir, "ReadRange", b)
	if err == nil {
		t.Fatal("expected error for start_line < 1")
	}
	if !strings.Contains(err.Error(), "start_line must be >= 1") {
		t.Errorf("expected 'start_line must be >= 1' in error, got %q", err.Error())
	}

	// Test end_line < start_line — should error
	b, _ = json.Marshal(map[string]any{"path": "test.txt", "start_line": 10, "end_line": 5})
	_, err = reg.Execute(context.Background(), dir, "ReadRange", b)
	if err == nil {
		t.Fatal("expected error for end_line < start_line")
	}
	if !strings.Contains(err.Error(), "end_line") {
		t.Errorf("expected 'end_line' in error, got %q", err.Error())
	}
}
