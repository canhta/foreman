package context

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/canhta/foreman/internal/models"
)

var builtinSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(AKIA[0-9A-Z]{16})`),
	regexp.MustCompile(`(?i)(sk-[a-zA-Z0-9]{20,})`),
	regexp.MustCompile(`(?i)(ghp_[a-zA-Z0-9]{36})`),
	regexp.MustCompile(`(?i)(glpat-[a-zA-Z0-9\-]{20,})`),
	regexp.MustCompile(`(?i)-----BEGIN (RSA |EC |DSA )?PRIVATE KEY-----`),
	regexp.MustCompile(`(?i)(xox[bprs]-[a-zA-Z0-9\-]+)`),
	regexp.MustCompile(`(?i)(api[_-]?key|api[_-]?secret|api[_-]?token)\s*[:=]\s*["']?[a-zA-Z0-9\-._]{16,}`),
	regexp.MustCompile(`(?i)(password|passwd|pwd)\s*[:=]\s*["'][^"']{8,}["']`),
}

type ScanResult struct {
	Path       string
	Matches    []SecretMatch
	HasSecrets bool
}

type SecretMatch struct {
	Pattern string
	Snippet string
	Line    int
}

type SecretsScanner struct {
	patterns      []*regexp.Regexp
	alwaysExclude []string
}

func NewSecretsScanner(config *models.SecretsConfig) *SecretsScanner {
	patterns := make([]*regexp.Regexp, len(builtinSecretPatterns))
	copy(patterns, builtinSecretPatterns)
	for _, extra := range config.ExtraPatterns {
		compiled, err := regexp.Compile(extra)
		if err == nil {
			patterns = append(patterns, compiled)
		}
	}
	return &SecretsScanner{
		patterns:      patterns,
		alwaysExclude: config.AlwaysExclude,
	}
}

func (s *SecretsScanner) ScanFile(path, content string) *ScanResult {
	for _, pattern := range s.alwaysExclude {
		matched, _ := filepath.Match(pattern, filepath.Base(path))
		if matched {
			return &ScanResult{
				Path: path, HasSecrets: true,
				Matches: []SecretMatch{{Pattern: "always_exclude", Snippet: path}},
			}
		}
	}

	result := &ScanResult{Path: path}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		for _, pat := range s.patterns {
			if pat.MatchString(line) {
				result.HasSecrets = true
				match := pat.FindString(line)
				redacted := match
				if len(redacted) > 4 {
					redacted = redacted[:4] + "***"
				}
				result.Matches = append(result.Matches, SecretMatch{
					Line: i + 1, Pattern: pat.String(), Snippet: redacted,
				})
			}
		}
	}
	return result
}
