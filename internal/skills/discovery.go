package skills

import (
	"os"
	"path/filepath"
	"strings"
)

// DiscoverSkillPaths finds all skill files from multiple locations.
// Scans in order: project skills/, .foreman/skills/, hierarchy walk.
// If startDir is provided, walks up to rootDir collecting skills at each level.
// additionalPaths are appended last.
func DiscoverSkillPaths(rootDir, startDir string, additionalPaths ...string) []string {
	seen := make(map[string]bool)
	var paths []string

	addDir := func(dir string) {
		candidates := []string{
			filepath.Join(dir, "skills"),
			filepath.Join(dir, ".foreman", "skills"),
		}
		for _, candidate := range candidates {
			info, err := os.Stat(candidate)
			if err != nil || !info.IsDir() {
				continue
			}
			_ = filepath.Walk(candidate, func(path string, info os.FileInfo, err error) error {
				if err != nil || info.IsDir() {
					return nil
				}
				ext := strings.ToLower(filepath.Ext(path))
				if (ext == ".yml" || ext == ".yaml" || ext == ".md") && !seen[path] {
					seen[path] = true
					paths = append(paths, path)
				}
				return nil
			})
		}
	}

	if startDir != "" && startDir != rootDir {
		dir := startDir
		for {
			addDir(dir)
			if dir == rootDir || dir == filepath.Dir(dir) {
				break
			}
			dir = filepath.Dir(dir)
		}
	} else {
		addDir(rootDir)
	}

	for _, extra := range additionalPaths {
		if info, err := os.Stat(extra); err == nil {
			if info.IsDir() {
				// Scan the directory itself for skill files, as well as subdirectories.
				_ = filepath.Walk(extra, func(path string, fi os.FileInfo, err error) error {
					if err != nil || fi.IsDir() {
						return nil
					}
					ext := strings.ToLower(filepath.Ext(path))
					if (ext == ".yml" || ext == ".yaml" || ext == ".md") && !seen[path] {
						seen[path] = true
						paths = append(paths, path)
					}
					return nil
				})
			} else if !seen[extra] {
				seen[extra] = true
				paths = append(paths, extra)
			}
		}
	}

	return paths
}
