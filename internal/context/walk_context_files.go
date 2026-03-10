package context

import (
	"os"
	"path/filepath"
)

// contextFileNames are the candidate filenames checked at each directory level.
var contextFileNames = []string{
	"AGENTS.md",
	".foreman-rules.md",
	filepath.Join(".foreman", "context.md"),
}

// WalkContextFiles collects context files by walking up from startDir to workDir (inclusive).
// Files found deeper in the hierarchy are returned first (more specific context first).
// Duplicate paths are never added. workDir must be an ancestor of startDir (or equal).
func WalkContextFiles(startDir, workDir string) []string {
	var result []string
	seen := map[string]struct{}{}
	dir := startDir
	for {
		for _, name := range contextFileNames {
			p := filepath.Join(dir, name)
			if _, err := os.Stat(p); err == nil {
				if _, dup := seen[p]; !dup {
					result = append(result, p)
					seen[p] = struct{}{}
				}
			}
		}
		if dir == workDir {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root without finding workDir — stop
			break
		}
		dir = parent
	}
	return result
}
