package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// HashPromptTemplates walks dir recursively for *.md.j2 files, computes SHA256(content)
// for each, and returns a map of relative-path → hex-encoded SHA256.
// The relative path uses forward slashes (e.g. "implementer.md.j2", "retry/compile.md.j2").
// Returns an empty map (not an error) if dir does not exist.
func HashPromptTemplates(dir string) (map[string]string, error) {
	result := make(map[string]string)

	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return nil, fmt.Errorf("stat prompts dir %q: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("prompts dir %q is not a directory", dir)
	}

	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".j2" {
			return nil
		}
		// Only process *.md.j2 files.
		base := d.Name()
		if len(base) < 6 || base[len(base)-6:] != ".md.j2" {
			return nil
		}

		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("read template %q: %w", path, readErr)
		}

		sum := sha256.Sum256(content)
		hexHash := hex.EncodeToString(sum[:])

		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return fmt.Errorf("rel path for %q: %w", path, relErr)
		}
		// Normalise to forward slashes for cross-platform consistency.
		rel = filepath.ToSlash(rel)
		result[rel] = hexHash
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk prompts dir %q: %w", dir, err)
	}

	return result, nil
}
