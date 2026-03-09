package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

func SupportedLSPOperations() []string {
	return []string{
		"goToDefinition",
		"findReferences",
		"hover",
		"documentSymbol",
		"workspaceSymbol",
	}
}

func LSPToolSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"operation": {
				"type": "string",
				"enum": ["goToDefinition", "findReferences", "hover", "documentSymbol", "workspaceSymbol"],
				"description": "The LSP operation to perform"
			},
			"filePath": {"type": "string", "description": "Path to the file"},
			"line": {"type": "integer", "description": "1-based line number"},
			"character": {"type": "integer", "description": "0-based character offset"},
			"query": {"type": "string", "description": "Search query (for workspaceSymbol)"}
		},
		"required": ["operation"]
	}`)
}

func BuildLSPCommand(operation, filePath string, line, char int) *exec.Cmd {
	loc := fmt.Sprintf("%s:%d:%d", filePath, line, char)
	switch operation {
	case "goToDefinition":
		return exec.Command("gopls", "definition", loc)
	case "findReferences":
		return exec.Command("gopls", "references", loc)
	case "hover":
		return exec.Command("gopls", "hover", loc)
	case "documentSymbol":
		return exec.Command("gopls", "symbols", filePath)
	case "workspaceSymbol":
		return exec.Command("gopls", "workspace_symbol", filePath)
	default:
		return nil
	}
}

func ExecuteLSP(ctx context.Context, workDir, operation, filePath string, line, char int, query string) (string, error) {
	var cmd *exec.Cmd
	switch operation {
	case "workspaceSymbol":
		cmd = exec.CommandContext(ctx, "gopls", "workspace_symbol", query)
	default:
		cmd = BuildLSPCommand(operation, filePath, line, char)
	}
	if cmd == nil {
		return "", fmt.Errorf("unsupported LSP operation: %s", operation)
	}
	cmd.Dir = workDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s failed: %w: %s", operation, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// lspTool implements the Tool interface for LSP operations via gopls.
type lspTool struct{}

func (t *lspTool) Name() string { return "LSP" }
func (t *lspTool) Description() string {
	return "Perform language server operations (go to definition, find references, hover, symbols) via gopls"
}
func (t *lspTool) Schema() json.RawMessage { return LSPToolSchema() }
func (t *lspTool) Execute(ctx context.Context, workDir string, input json.RawMessage) (string, error) {
	var args struct {
		Operation string `json:"operation"`
		FilePath  string `json:"filePath"`
		Line      int    `json:"line"`
		Character int    `json:"character"`
		Query     string `json:"query"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("lsp: %w", err)
	}
	return ExecuteLSP(ctx, workDir, args.Operation, args.FilePath, args.Line, args.Character, args.Query)
}

func registerLSP(r *Registry) {
	if _, err := exec.LookPath("gopls"); err == nil {
		r.Register(&lspTool{})
	}
}
