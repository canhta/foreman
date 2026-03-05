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

// languageDetectorOrder defines the priority order for language detection.
// Must match the order in repo_analyzer.go's detectLanguage to stay consistent.
// go → rust → node → python (same priority as repo_analyzer.go).
var languageDetectorOrder = []struct {
	lang    string
	markers []string
}{
	{"go", []string{"go.mod"}},
	{"rust", []string{"Cargo.toml"}},
	{"node", []string{"package.json"}},
	{"python", []string{"requirements.txt", "pyproject.toml", "setup.py"}},
}

// DetectLanguage infers the primary language from a list of file names by
// matching well-known marker files (e.g. go.mod → go, package.json → node).
// Priority: go > rust > node > python (matches repo_analyzer.go's detectLanguage).
// Note: DetectLanguage operates on a pre-collected file list, whereas
// repo_analyzer.go's detectLanguage walks the filesystem directly. Use this
// function when you already have a file list (e.g. from git tree); use
// AnalyzeRepo when you have a working directory to scan.
func DetectLanguage(files []string) string {
	fileSet := make(map[string]bool)
	for _, f := range files {
		fileSet[f] = true
	}
	for _, entry := range languageDetectorOrder {
		for _, marker := range entry.markers {
			if fileSet[marker] {
				return entry.lang
			}
		}
	}
	return ""
}
