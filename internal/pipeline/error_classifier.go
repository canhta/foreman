// internal/pipeline/error_classifier.go
package pipeline

import "strings"

// ErrorType categorises a task retry error so the runner can select a targeted
// retry prompt and report granular metrics.
type ErrorType string

const (
	ErrorTypeCompile        ErrorType = "compile_error"
	ErrorTypeTypeError      ErrorType = "type_error"
	ErrorTypeLintStyle      ErrorType = "lint_style"
	ErrorTypeTestAssertion  ErrorType = "test_assertion"
	ErrorTypeTestRuntime    ErrorType = "test_runtime"
	ErrorTypeSpecViolation  ErrorType = "spec_violation"
	ErrorTypeQualityConcern ErrorType = "quality_concern"
	ErrorTypeUnknown        ErrorType = "unknown"
)

// ClassifyRetryError determines the ErrorType from a feedback string.
// Classification is deterministic (no LLM call) and relies on keyword heuristics.
// The most specific match wins; ErrorTypeUnknown is the fallback.
func ClassifyRetryError(feedback string) ErrorType {
	lower := strings.ToLower(feedback)

	// Quality / style review feedback (check before generic compile patterns).
	if strings.Contains(lower, "quality reviewer") ||
		strings.Contains(lower, "quality review") ||
		strings.Contains(lower, "code quality") {
		return ErrorTypeQualityConcern
	}

	// Spec compliance violations.
	if strings.Contains(lower, "spec reviewer") ||
		strings.Contains(lower, "spec review") ||
		strings.Contains(lower, "acceptance criteria") ||
		strings.Contains(lower, "spec_violation") {
		return ErrorTypeSpecViolation
	}

	// Type errors (check before compile to avoid double-matching).
	if strings.Contains(lower, "cannot use") ||
		strings.Contains(lower, "type mismatch") ||
		strings.Contains(lower, "cannot convert") ||
		strings.Contains(lower, "does not implement") {
		return ErrorTypeTypeError
	}

	// Compile / build errors.
	if strings.Contains(lower, "syntax error") ||
		strings.Contains(lower, "compile error") ||
		strings.Contains(lower, "build failed") ||
		strings.Contains(lower, "cannot compile") ||
		strings.Contains(lower, "undefined:") ||
		strings.Contains(lower, "declared and not used") ||
		strings.Contains(lower, "missing return") ||
		strings.Contains(lower, "unexpected {") ||
		strings.Contains(lower, "unexpected }") {
		return ErrorTypeCompile
	}

	// Lint / style issues.
	if strings.Contains(lower, "lint") ||
		strings.Contains(lower, "vet:") ||
		strings.Contains(lower, "gofmt") ||
		strings.Contains(lower, "golangci") ||
		strings.Contains(lower, "eslint") {
		return ErrorTypeLintStyle
	}

	// Runtime panics / goroutine dumps — check before assertion failures.
	if strings.Contains(lower, "panic:") ||
		strings.Contains(lower, "nil pointer") ||
		strings.Contains(lower, "index out of range") ||
		strings.Contains(lower, "fatal error") ||
		strings.Contains(lower, "runtime error") {
		return ErrorTypeTestRuntime
	}

	// Test assertion failures.
	if strings.Contains(lower, "assertion failed") ||
		strings.Contains(lower, "assert.") ||
		strings.Contains(lower, "expected:") ||
		strings.Contains(lower, "testify") ||
		strings.Contains(lower, "--- fail") ||
		strings.Contains(lower, "test failed") ||
		strings.Contains(lower, "fail\t") {
		return ErrorTypeTestAssertion
	}

	return ErrorTypeUnknown
}
