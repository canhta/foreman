package tools_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/agent/tools"
	"github.com/canhta/foreman/internal/git"
)

// mockGitProvider implements git.GitProvider for testing.
type mockGitProvider struct {
	diffOutput  string
	logEntries  []git.CommitEntry
	treeEntries []git.FileEntry
}

func (m *mockGitProvider) EnsureRepo(_ context.Context, _ string) error              { return nil }
func (m *mockGitProvider) CreateBranch(_ context.Context, _, _ string) error         { return nil }
func (m *mockGitProvider) Commit(_ context.Context, _, _ string) (string, error)     { return "abc123", nil }
func (m *mockGitProvider) Diff(_ context.Context, _, _, _ string) (string, error)    { return m.diffOutput, nil }
func (m *mockGitProvider) DiffWorking(_ context.Context, _ string) (string, error)   { return m.diffOutput, nil }
func (m *mockGitProvider) Push(_ context.Context, _, _ string) error                 { return nil }
func (m *mockGitProvider) RebaseOnto(_ context.Context, _, _ string) (*git.RebaseResult, error) {
	return &git.RebaseResult{Success: true}, nil
}
func (m *mockGitProvider) FileTree(_ context.Context, _ string) ([]git.FileEntry, error) {
	return m.treeEntries, nil
}
func (m *mockGitProvider) Log(_ context.Context, _ string, count int) ([]git.CommitEntry, error) {
	if count > 0 && count < len(m.logEntries) {
		return m.logEntries[:count], nil
	}
	return m.logEntries, nil
}
func (m *mockGitProvider) StageAll(_ context.Context, _ string) error { return nil }

func newGitRegistry(t *testing.T, g git.GitProvider) (*tools.Registry, string) {
	t.Helper()
	reg := tools.NewRegistry(g, nil, tools.ToolHooks{})
	return reg, t.TempDir()
}

func TestGetDiff_WorkingDir(t *testing.T) {
	g := &mockGitProvider{diffOutput: "--- a/main.go\n+++ b/main.go\n@@ -1 +1 @@\n-old\n+new\n"}
	reg, dir := newGitRegistry(t, g)
	out := execTool(t, reg, dir, "GetDiff", map[string]string{})
	if !strings.Contains(out, "main.go") {
		t.Errorf("expected diff output, got %q", out)
	}
}

func TestGetDiff_BaseHead(t *testing.T) {
	g := &mockGitProvider{diffOutput: "diff between commits"}
	reg, dir := newGitRegistry(t, g)
	out := execTool(t, reg, dir, "GetDiff", map[string]string{"base": "HEAD~1", "head": "HEAD"})
	if !strings.Contains(out, "diff between commits") {
		t.Errorf("expected diff, got %q", out)
	}
}

func TestGetDiff_NilProvider(t *testing.T) {
	reg := tools.NewRegistry(nil, nil, tools.ToolHooks{})
	b, _ := json.Marshal(map[string]string{})
	_, err := reg.Execute(context.Background(), t.TempDir(), "GetDiff", b)
	if err == nil {
		t.Fatal("expected error when git provider is nil")
	}
}

func TestGetCommitLog_Basic(t *testing.T) {
	g := &mockGitProvider{logEntries: []git.CommitEntry{
		{SHA: "abc123", Message: "feat: add thing", Author: "dev", Date: time.Now()},
		{SHA: "def456", Message: "fix: bug", Author: "dev", Date: time.Now()},
	}}
	reg, dir := newGitRegistry(t, g)
	out := execTool(t, reg, dir, "GetCommitLog", map[string]any{"count": 2})
	if !strings.Contains(out, "abc123") || !strings.Contains(out, "feat: add thing") {
		t.Errorf("expected log output, got %q", out)
	}
}

func TestTreeSummary_Basic(t *testing.T) {
	g := &mockGitProvider{treeEntries: []git.FileEntry{
		{Path: "main.go", IsDir: false},
		{Path: "pkg/util.go", IsDir: false},
		{Path: "pkg", IsDir: true},
	}}
	reg, dir := newGitRegistry(t, g)
	out := execTool(t, reg, dir, "TreeSummary", map[string]any{})
	if !strings.Contains(out, "main.go") {
		t.Errorf("expected tree output, got %q", out)
	}
}
