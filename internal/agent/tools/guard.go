package tools

import (
	"fmt"
	"path/filepath"
	"strings"
)

var secretPathPatterns = []string{".env", ".key", ".pem", ".p12", ".pfx", "id_rsa", "id_ed25519", ".secret"}
var secretContentPatterns = []string{"BEGIN RSA PRIVATE KEY", "BEGIN EC PRIVATE KEY", "BEGIN OPENSSH PRIVATE KEY"}

// ValidatePath ensures path resolves within workDir and is not absolute.
func ValidatePath(workDir, path string) error {
	if filepath.IsAbs(path) {
		return fmt.Errorf("path %q must be relative", path)
	}
	abs := filepath.Join(workDir, filepath.Clean(path))
	clean := filepath.Clean(workDir) + string(filepath.Separator)
	if !strings.HasPrefix(abs, clean) && abs != filepath.Clean(workDir) {
		return fmt.Errorf("path %q is outside the working directory", path)
	}
	return nil
}

// CheckSecrets rejects writes to sensitive file paths or content containing private key headers.
func CheckSecrets(path, content string) error {
	base := strings.ToLower(filepath.Base(path))
	for _, pat := range secretPathPatterns {
		if strings.HasSuffix(base, pat) || base == strings.TrimPrefix(pat, ".") {
			return fmt.Errorf("writing to %q is not allowed (sensitive file pattern)", path)
		}
	}
	for _, pat := range secretContentPatterns {
		if strings.Contains(content, pat) {
			return fmt.Errorf("content contains sensitive pattern %q — write blocked", pat)
		}
	}
	return nil
}

// AbsPath returns the absolute path for a workDir-relative path, after ValidatePath.
func AbsPath(workDir, path string) string {
	return filepath.Join(workDir, filepath.Clean(path))
}
