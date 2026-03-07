package tools_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/canhta/foreman/internal/agent/tools"
	"github.com/canhta/foreman/internal/runner"
)

// mockCmdRunner satisfies runner.CommandRunner for testing.
type mockCmdRunner struct {
	stdout string
	stderr string
}

func (m *mockCmdRunner) Run(_ context.Context, _, _ string, _ []string, _ int) (*runner.CommandOutput, error) {
	return &runner.CommandOutput{Stdout: m.stdout, Stderr: m.stderr}, nil
}
func (m *mockCmdRunner) CommandExists(_ context.Context, _ string) bool { return true }

func newCodeRegistry(t *testing.T, cmd runner.CommandRunner) (*tools.Registry, string) {
	t.Helper()
	reg := tools.NewRegistry(nil, cmd, tools.ToolHooks{})
	return reg, t.TempDir()
}

func TestGetSymbol_FindsFunction(t *testing.T) {
	reg, dir := newFSRegistry(t)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc MyHandler() {}\nfunc other() {}"), 0644)
	b, _ := json.Marshal(map[string]string{"symbol": "MyHandler", "kind": "func"})
	out, err := reg.Execute(context.Background(), dir, "GetSymbol", b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "MyHandler") {
		t.Errorf("expected MyHandler in output, got %q", out)
	}
}

func TestGetSymbol_NotFound(t *testing.T) {
	reg, dir := newFSRegistry(t)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644)
	b, _ := json.Marshal(map[string]string{"symbol": "NonExistent"})
	out, err := reg.Execute(context.Background(), dir, "GetSymbol", b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "not found") {
		t.Errorf("expected 'not found' message, got %q", out)
	}
}

func TestGetErrors_ParsesLintOutput(t *testing.T) {
	cmd := &mockCmdRunner{
		stdout: "main.go:10:5: undefined: foo\nmain.go:20:1: unused import\n",
	}
	reg, dir := newCodeRegistry(t, cmd)
	out := execTool(t, reg, dir, "GetErrors", map[string]string{"tool": "golangci-lint"})
	if !strings.Contains(out, "main.go") || !strings.Contains(out, "undefined") {
		t.Errorf("expected lint issues, got %q", out)
	}
}

func TestGetErrors_NilRunner(t *testing.T) {
	reg := tools.NewRegistry(nil, nil, tools.ToolHooks{})
	b, _ := json.Marshal(map[string]string{"tool": "golangci-lint"})
	_, err := reg.Execute(context.Background(), t.TempDir(), "GetErrors", b)
	if err == nil {
		t.Fatal("expected error when runner is nil")
	}
}

// --- GetTypeDefinition tests ---

func TestGetTypeDefinition_GoStruct(t *testing.T) {
	reg, dir := newFSRegistry(t)
	goSrc := `package mypkg

// MyStruct is a test struct.
type MyStruct struct {
	Name string
	Age  int
}
`
	os.WriteFile(filepath.Join(dir, "types.go"), []byte(goSrc), 0644)
	out := execTool(t, reg, dir, "get_type_definition", map[string]string{
		"symbol": "MyStruct",
		"file":   "types.go",
	})
	if !strings.Contains(out, "MyStruct") {
		t.Errorf("expected MyStruct in output, got %q", out)
	}
	if !strings.Contains(out, "Name string") {
		t.Errorf("expected struct fields in output, got %q", out)
	}
}

func TestGetTypeDefinition_GoInterface(t *testing.T) {
	reg, dir := newFSRegistry(t)
	goSrc := `package mypkg

// MyInterface defines behavior.
type MyInterface interface {
	DoSomething() error
	GetName() string
}
`
	os.WriteFile(filepath.Join(dir, "iface.go"), []byte(goSrc), 0644)
	out := execTool(t, reg, dir, "get_type_definition", map[string]string{
		"symbol": "MyInterface",
		"file":   "iface.go",
	})
	if !strings.Contains(out, "MyInterface") {
		t.Errorf("expected MyInterface in output, got %q", out)
	}
	if !strings.Contains(out, "DoSomething") {
		t.Errorf("expected interface methods in output, got %q", out)
	}
}

func TestGetTypeDefinition_CrossFileResolution(t *testing.T) {
	reg, dir := newFSRegistry(t)
	// Symbol is defined in types.go, but hint points to main.go
	typeSrc := `package mypkg

type CrossType struct {
	Value int
}
`
	mainSrc := `package mypkg

func main() {}
`
	os.WriteFile(filepath.Join(dir, "types.go"), []byte(typeSrc), 0644)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainSrc), 0644)

	out := execTool(t, reg, dir, "get_type_definition", map[string]string{
		"symbol": "CrossType",
		"file":   "main.go", // hint to a different file from where symbol is defined
	})
	if !strings.Contains(out, "CrossType") {
		t.Errorf("expected CrossType found via cross-file resolution, got %q", out)
	}
	if !strings.Contains(out, "Value int") {
		t.Errorf("expected struct fields in output, got %q", out)
	}
}

func TestGetTypeDefinition_NotFound(t *testing.T) {
	reg, dir := newFSRegistry(t)
	goSrc := `package mypkg

type OtherType struct{}
`
	os.WriteFile(filepath.Join(dir, "types.go"), []byte(goSrc), 0644)
	out := execTool(t, reg, dir, "get_type_definition", map[string]string{
		"symbol": "NonExistentType",
		"file":   "types.go",
	})
	if !strings.Contains(out, "not found") {
		t.Errorf("expected 'not found' message, got %q", out)
	}
}

func TestGetTypeDefinition_FallbackRegex_TypeScript(t *testing.T) {
	reg, dir := newFSRegistry(t)
	tsSrc := `// UserProfile defines the user shape.
interface UserProfile {
  id: number;
  name: string;
  email: string;
}

interface OtherInterface {
  foo: string;
}
`
	os.WriteFile(filepath.Join(dir, "types.ts"), []byte(tsSrc), 0644)
	out := execTool(t, reg, dir, "get_type_definition", map[string]string{
		"symbol": "UserProfile",
		"file":   "types.ts",
	})
	if !strings.Contains(out, "UserProfile") {
		t.Errorf("expected UserProfile in output, got %q", out)
	}
	if !strings.Contains(out, "id: number") {
		t.Errorf("expected interface body in output, got %q", out)
	}
}

func TestGetTypeDefinition_TypeAlias(t *testing.T) {
	reg, dir := newFSRegistry(t)
	goSrc := `package mypkg

type StringSlice = []string

type MyAlias string
`
	os.WriteFile(filepath.Join(dir, "aliases.go"), []byte(goSrc), 0644)

	// Test type alias (=)
	out := execTool(t, reg, dir, "get_type_definition", map[string]string{
		"symbol": "StringSlice",
		"file":   "aliases.go",
	})
	if !strings.Contains(out, "StringSlice") {
		t.Errorf("expected StringSlice in output, got %q", out)
	}

	// Test named type
	out2 := execTool(t, reg, dir, "get_type_definition", map[string]string{
		"symbol": "MyAlias",
		"file":   "aliases.go",
	})
	if !strings.Contains(out2, "MyAlias") {
		t.Errorf("expected MyAlias in output, got %q", out2)
	}
}

func TestGetTypeDefinition_NoFileHint(t *testing.T) {
	reg, dir := newFSRegistry(t)
	goSrc := `package mypkg

type NoHintStruct struct {
	ID int
}
`
	os.WriteFile(filepath.Join(dir, "stuff.go"), []byte(goSrc), 0644)
	// Call without file hint — should still find the type by walking workDir
	out := execTool(t, reg, dir, "get_type_definition", map[string]string{
		"symbol": "NoHintStruct",
	})
	if !strings.Contains(out, "NoHintStruct") {
		t.Errorf("expected NoHintStruct without file hint, got %q", out)
	}
}
