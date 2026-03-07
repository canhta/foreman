// internal/pipeline/error_classifier_test.go
package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassifyRetryError(t *testing.T) {
	cases := []struct {
		name     string
		feedback string
		want     ErrorType
	}{
		{
			name:     "compile error - undefined",
			feedback: "undefined: NewHandler\nbuild failed",
			want:     ErrorTypeCompile,
		},
		{
			name:     "compile error - syntax",
			feedback: "syntax error: unexpected }, expected statement",
			want:     ErrorTypeCompile,
		},
		{
			name:     "type error - cannot use",
			feedback: "cannot use x (type int) as type string",
			want:     ErrorTypeTypeError,
		},
		{
			name:     "type error - does not implement",
			feedback: "handler does not implement http.Handler",
			want:     ErrorTypeTypeError,
		},
		{
			name:     "lint/style",
			feedback: "golangci-lint: exported function missing comment",
			want:     ErrorTypeLintStyle,
		},
		{
			name:     "test assertion",
			feedback: "--- FAIL: TestGetUsers\nexpected: 200\ngot: 500",
			want:     ErrorTypeTestAssertion,
		},
		{
			name:     "test runtime - panic",
			feedback: "panic: nil pointer dereference\ngoroutine 1 [running]",
			want:     ErrorTypeTestRuntime,
		},
		{
			name:     "spec violation",
			feedback: "spec reviewer rejected: acceptance criteria not met",
			want:     ErrorTypeSpecViolation,
		},
		{
			name:     "quality concern",
			feedback: "quality reviewer: code quality issues found",
			want:     ErrorTypeQualityConcern,
		},
		{
			name:     "unknown",
			feedback: "something went completely wrong in an unrecognised way",
			want:     ErrorTypeUnknown,
		},
		{
			name:     "empty",
			feedback: "",
			want:     ErrorTypeUnknown,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyRetryError(tc.feedback)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestClassifyRetryError_QualityBeforeCompile ensures quality concern takes
// priority over generic compile signals when both match.
func TestClassifyRetryError_QualityBeforeCompile(t *testing.T) {
	feedback := "quality reviewer: undefined variable usage detected"
	got := ClassifyRetryError(feedback)
	assert.Equal(t, ErrorTypeQualityConcern, got)
}
