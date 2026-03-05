package runner

import (
	"testing"
)

func TestParseTestOutput_GoPass(t *testing.T) {
	output := `=== RUN   TestAdd
--- PASS: TestAdd (0.00s)
PASS
ok  	example.com/pkg	0.003s`

	result := ParseTestOutput(output, "go")
	if !result.Passed {
		t.Error("expected passed")
	}
	if result.TotalTests != 1 {
		t.Errorf("expected 1 test, got %d", result.TotalTests)
	}
	if result.PassedTests != 1 {
		t.Errorf("expected 1 passed, got %d", result.PassedTests)
	}
}

func TestParseTestOutput_GoFail(t *testing.T) {
	output := `=== RUN   TestAdd
--- FAIL: TestAdd (0.00s)
    add_test.go:8: expected 5, got 0
FAIL
FAIL	example.com/pkg	0.003s`

	result := ParseTestOutput(output, "go")
	if result.Passed {
		t.Error("expected failed")
	}
	if result.FailedTests != 1 {
		t.Errorf("expected 1 failed, got %d", result.FailedTests)
	}
	if len(result.Failures) == 0 {
		t.Error("expected failure details")
	}
}

func TestParseLintOutput_Clean(t *testing.T) {
	result := ParseLintOutput("", "go")
	if !result.Clean {
		t.Error("expected clean lint")
	}
}

func TestParseLintOutput_WithErrors(t *testing.T) {
	output := `main.go:10:5: undefined: foo
main.go:15:2: syntax error`

	result := ParseLintOutput(output, "go")
	if result.Clean {
		t.Error("expected lint errors")
	}
	if len(result.Issues) != 2 {
		t.Errorf("expected 2 issues, got %d", len(result.Issues))
	}
}
