package git

import (
	"path/filepath"
	"strings"
)

// DefaultForbiddenPatterns are file patterns that must never be committed.
var DefaultForbiddenPatterns = []string{
	".env",
	".env.*",
	"*.pem",
	"*.key",
	"*.p12",
	".ssh/*",
	".aws/*",
	".gnupg/*",
	"*credentials*",
	"*.pfx",
}

// CheckForbiddenFiles returns any files from the given list that match forbidden patterns.
// This should be called before every git commit.
func CheckForbiddenFiles(files []string, patterns []string) []string {
	var forbidden []string
	for _, f := range files {
		base := filepath.Base(f)
		for _, pattern := range patterns {
			// Check both full path and base name
			matched, _ := filepath.Match(pattern, f)
			if !matched {
				matched, _ = filepath.Match(pattern, base)
			}
			// Also check directory prefix patterns like ".ssh/*"
			if !matched && strings.Contains(pattern, "/") {
				dir := strings.TrimSuffix(pattern, "/*")
				if strings.HasPrefix(f, dir+"/") || strings.HasPrefix(f, dir+"\\") {
					matched = true
				}
			}
			if matched {
				forbidden = append(forbidden, f)
				break
			}
		}
	}
	return forbidden
}
