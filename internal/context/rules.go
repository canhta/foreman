// internal/context/rules.go
package context

// DirectoryRules holds conventions for a detected language/framework.
type DirectoryRules struct {
	Language     string
	TestCommand  string
	LintCommand  string
	BuildCommand string
	PackageFile  string
}

var languageRules = map[string]DirectoryRules{
	"go": {
		Language:     "go",
		TestCommand:  "go test ./...",
		LintCommand:  "go vet ./...",
		BuildCommand: "go build ./...",
		PackageFile:  "go.mod",
	},
	"node": {
		Language:     "node",
		TestCommand:  "npm test",
		LintCommand:  "npx eslint .",
		BuildCommand: "npm run build",
		PackageFile:  "package.json",
	},
	"rust": {
		Language:     "rust",
		TestCommand:  "cargo test",
		LintCommand:  "cargo clippy",
		BuildCommand: "cargo build",
		PackageFile:  "Cargo.toml",
	},
	"python": {
		Language:     "python",
		TestCommand:  "pytest",
		LintCommand:  "ruff check .",
		BuildCommand: "",
		PackageFile:  "requirements.txt",
	},
}

var defaultRules = DirectoryRules{
	TestCommand: "make test",
	LintCommand: "make lint",
}

// LoadDirectoryRules returns the rules for the given language, or default rules
// if the language is empty or unknown.
func LoadDirectoryRules(language string) *DirectoryRules {
	if language == "" {
		r := defaultRules
		return &r
	}
	if rules, ok := languageRules[language]; ok {
		return &rules
	}
	r := defaultRules
	return &r
}

var languageDetectors = map[string][]string{
	"go":     {"go.mod"},
	"node":   {"package.json"},
	"rust":   {"Cargo.toml"},
	"python": {"requirements.txt", "pyproject.toml", "setup.py"},
}

// DetectLanguage infers the primary language from a list of file names by
// matching well-known marker files (e.g. go.mod → go, package.json → node).
func DetectLanguage(files []string) string {
	fileSet := make(map[string]bool)
	for _, f := range files {
		fileSet[f] = true
	}
	for lang, markers := range languageDetectors {
		for _, marker := range markers {
			if fileSet[marker] {
				return lang
			}
		}
	}
	return ""
}
