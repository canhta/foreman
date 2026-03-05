package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassifyTestFailure_Assertion(t *testing.T) {
	stdout := "--- FAIL: TestAdd (0.00s)\n    add_test.go:10: expected 4, got 0"
	stderr := ""
	result := ClassifyTestFailure(stdout, stderr)
	assert.Equal(t, FailureAssertion, result)
}

func TestClassifyTestFailure_Compile(t *testing.T) {
	stdout := ""
	stderr := "# mypackage\n./add.go:5:2: undefined: SomeFunc"
	result := ClassifyTestFailure(stdout, stderr)
	assert.Equal(t, FailureCompile, result)
}

func TestClassifyTestFailure_Import(t *testing.T) {
	stdout := ""
	stderr := "cannot find module providing package github.com/foo/bar"
	result := ClassifyTestFailure(stdout, stderr)
	assert.Equal(t, FailureImport, result)
}

func TestClassifyTestFailure_Unknown(t *testing.T) {
	stdout := "some random output"
	stderr := "something went wrong"
	result := ClassifyTestFailure(stdout, stderr)
	assert.Equal(t, FailureUnknown, result)
}

func TestClassifyTestFailure_Runtime(t *testing.T) {
	tests := []struct {
		name   string
		stdout string
		stderr string
	}{
		{
			name:   "runtime index out of range",
			stdout: "panic: runtime error: index out of range [0] with length 0",
			stderr: "",
		},
		{
			name:   "panic in stderr",
			stdout: "",
			stderr: "panic: interface conversion: interface {} is nil, not string",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := ClassifyTestFailure(tc.stdout, tc.stderr)
			assert.Equal(t, FailureRuntime, result)
		})
	}
}

func TestClassifyTestFailure_NoSuchFileNotImport(t *testing.T) {
	// Runtime file I/O error should not be classified as FailureImport
	stdout := ""
	stderr := "open /tmp/data.json: no such file or directory"
	result := ClassifyTestFailure(stdout, stderr)
	assert.NotEqual(t, FailureImport, result)
}

func TestClassifyTestFailure_PythonTypeError(t *testing.T) {
	stdout := ""
	stderr := "TypeError: unsupported operand type(s) for +: 'int' and 'str'"
	result := ClassifyTestFailure(stdout, stderr)
	assert.Equal(t, FailureCompile, result)
}

func TestClassifyTestFailure_GoTestFailLine(t *testing.T) {
	// Go's exact test failure format should be caught as assertion
	stdout := "--- FAIL: TestAdd (0.00s)"
	stderr := ""
	result := ClassifyTestFailure(stdout, stderr)
	assert.Equal(t, FailureAssertion, result)
}

func TestIsTestFile(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"internal/foo/bar_test.go", true},
		{"src/utils.test.ts", true},
		{"tests/test_handler.py", true},
		{"spec/models/user_spec.rb", true},
		{"__tests__/handler.ts", true},
		{"handler_test.py", true},
		{"internal/foo/bar.go", false},
		{"src/utils.ts", false},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			assert.Equal(t, tc.expected, IsTestFile(tc.path))
		})
	}
}

func TestNewTDDResult_ValidRed(t *testing.T) {
	result := &TDDResult{Valid: true}
	assert.True(t, result.Valid)
}

func TestNewTDDResult_InvalidRed_TestsPassed(t *testing.T) {
	result := &TDDResult{
		Valid:  false,
		Phase:  "red",
		Reason: "Tests passed without implementation code",
	}
	assert.False(t, result.Valid)
	assert.Equal(t, "red", result.Phase)
}
