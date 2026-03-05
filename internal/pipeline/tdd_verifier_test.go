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

func TestIsTestFile(t *testing.T) {
	assert.True(t, IsTestFile("internal/foo/bar_test.go"))
	assert.True(t, IsTestFile("src/utils.test.ts"))
	assert.True(t, IsTestFile("tests/test_handler.py"))
	assert.True(t, IsTestFile("spec/models/user_spec.rb"))
	assert.False(t, IsTestFile("internal/foo/bar.go"))
	assert.False(t, IsTestFile("src/utils.ts"))
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
