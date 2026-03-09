// Package envloader loads .env files into the process environment and copies
// them into git worktrees.
package envloader

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
)

// Load parses each source file in files (map[dest]src) and sets the
// KEY=VALUE pairs as process environment variables via os.Setenv.
// Missing source files are logged as warnings and skipped (not errors).
// Later files win on key conflicts.
func Load(files map[string]string) error {
	for _, src := range files {
		if err := loadFile(src); err != nil {
			return err
		}
	}
	return nil
}

// CopyInto copies each source file to <worktreeDir>/<dest>, creating
// intermediate directories as needed. Missing source files are skipped.
func CopyInto(files map[string]string, worktreeDir string) error {
	for dest, src := range files {
		destPath := filepath.Join(worktreeDir, filepath.FromSlash(dest))
		if err := copyFile(src, destPath); err != nil {
			return fmt.Errorf("copy env file %s -> %s: %w", src, destPath, err)
		}
	}
	return nil
}

func loadFile(src string) error {
	f, err := os.Open(src)
	if err != nil {
		if os.IsNotExist(err) {
			log.Warn().Str("src", src).Msg("env file not found, skipping")
			return nil
		}
		return fmt.Errorf("open env file %s: %w", src, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		// Strip surrounding quotes (single or double).
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') ||
				(val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		if err := os.Setenv(key, val); err != nil {
			return fmt.Errorf("setenv %s: %w", key, err)
		}
	}
	return scanner.Err()
}

func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		if os.IsNotExist(err) {
			log.Warn().Str("src", src).Msg("env source file not found, skipping copy")
			return nil
		}
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
