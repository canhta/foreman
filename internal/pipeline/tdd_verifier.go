package pipeline

import (
	"path/filepath"
	"strings"
)

// TDDResult holds the outcome of mechanical TDD verification.
type TDDResult struct {
	Valid  bool
	Reason string
	Phase  string // "red" or "green"
}

// TestFailureType categorizes how a test failed.
type TestFailureType string

const (
	FailureAssertion TestFailureType = "assertion" // Valid RED
	FailureCompile   TestFailureType = "compile"   // Invalid RED
	FailureImport    TestFailureType = "import"    // Invalid RED
	FailureRuntime   TestFailureType = "runtime"   // Ambiguous - treat as invalid
	FailureUnknown   TestFailureType = "unknown"
)

// ClassifyTestFailure parses test output to determine failure type.
func ClassifyTestFailure(stdout, stderr string) TestFailureType {
	combined := stdout + "\n" + stderr
	lower := strings.ToLower(combined)

	// Check for runtime errors (panics) — ambiguous, treat as invalid
	runtimePatterns := []string{
		"panic: runtime error",
		"panic:",
	}
	for _, p := range runtimePatterns {
		if strings.Contains(lower, p) {
			return FailureRuntime
		}
	}

	// Check for import errors first (more specific than compile)
	importPatterns := []string{
		"cannot find module",
		"module not found",
		"importerror",
		"import error",
		"could not resolve",
		"cannot resolve",
	}
	for _, p := range importPatterns {
		if strings.Contains(lower, p) {
			return FailureImport
		}
	}

	// Check for compile/syntax errors
	compilePatterns := []string{
		"syntaxerror",
		"syntax error",
		"compilation failed",
		"build failed",
		"error ts",
		"type error",
		"typeerror",
		"undefined: ",
		"cannot find symbol",
		"error[e",
	}
	for _, p := range compilePatterns {
		if strings.Contains(lower, p) {
			return FailureCompile
		}
	}

	// Check for assertion failures (valid RED)
	assertionPatterns := []string{
		"assertionerror",
		"assertion failed",
		"expect(",
		"expected",
		"assert.",
		"assertequal",
		"--- fail:",
		"not equal",
		"not to equal",
		"tobetruthy",
		"tobefalsy",
		"should have",
		"should be",
	}
	for _, p := range assertionPatterns {
		if strings.Contains(lower, p) {
			return FailureAssertion
		}
	}

	return FailureUnknown
}

// IsTestFile returns true if the file path looks like a test file.
func IsTestFile(path string) bool {
	base := filepath.Base(path)
	lower := strings.ToLower(base)

	// Go: _test.go
	if strings.HasSuffix(lower, "_test.go") {
		return true
	}
	// JS/TS: .test.ts, .test.js, .spec.ts, .spec.js
	for _, suffix := range []string{".test.ts", ".test.tsx", ".test.js", ".test.jsx", ".spec.ts", ".spec.tsx", ".spec.js", ".spec.jsx"} {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	// Python: test_*.py or *_test.py
	if strings.HasSuffix(lower, ".py") && (strings.HasPrefix(lower, "test_") || strings.HasSuffix(strings.TrimSuffix(lower, ".py"), "_test")) {
		return true
	}
	// Ruby: _spec.rb
	if strings.HasSuffix(lower, "_spec.rb") {
		return true
	}
	// Directory-based: tests/, test/, spec/
	dir := filepath.Dir(path)
	parts := strings.Split(dir, string(filepath.Separator))
	for _, part := range parts {
		if part == "tests" || part == "test" || part == "spec" || part == "__tests__" {
			return true
		}
	}
	return false
}
