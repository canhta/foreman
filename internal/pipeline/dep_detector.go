// internal/pipeline/dep_detector.go
package pipeline

import (
	"path/filepath"
)

// DepChangeResult describes a detected dependency file change.
type DepChangeResult struct {
	Changed bool
	File    string   // The dependency file that changed (basename)
	Command string   // Install command to run
	Args    []string // Install command arguments
}

// depFileMapping maps dependency file basenames to install commands.
var depFileMapping = map[string]struct {
	Command string
	Args    []string
}{
	"package.json":      {Command: "npm", Args: []string{"install"}},
	"package-lock.json": {Command: "npm", Args: []string{"install"}},
	"yarn.lock":         {Command: "yarn", Args: []string{"install"}},
	"pnpm-lock.yaml":    {Command: "pnpm", Args: []string{"install"}},
	"go.mod":            {Command: "go", Args: []string{"mod", "download"}},
	"go.sum":            {Command: "go", Args: []string{"mod", "download"}},
	"Cargo.toml":        {Command: "cargo", Args: []string{"fetch"}},
	"Cargo.lock":        {Command: "cargo", Args: []string{"fetch"}},
	"requirements.txt":  {Command: "pip", Args: []string{"install", "-r", "requirements.txt"}},
	"pyproject.toml":    {Command: "poetry", Args: []string{"install"}},
	"poetry.lock":       {Command: "poetry", Args: []string{"install"}},
	"Gemfile":           {Command: "bundle", Args: []string{"install"}},
	"Gemfile.lock":      {Command: "bundle", Args: []string{"install"}},
}

// DetectDepChange checks if any modified files are dependency manifests
// and returns the install command to run if so.
func DetectDepChange(modifiedFiles []string) DepChangeResult {
	for _, path := range modifiedFiles {
		base := filepath.Base(path)
		if mapping, ok := depFileMapping[base]; ok {
			args := make([]string, len(mapping.Args))
			copy(args, mapping.Args)
			return DepChangeResult{
				Changed: true,
				File:    base,
				Command: mapping.Command,
				Args:    args,
			}
		}
	}
	return DepChangeResult{Changed: false}
}
