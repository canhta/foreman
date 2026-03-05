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
		t.Fatal("expected failure details")
	}
	if result.Failures[0].TestName != "TestAdd" {
		t.Errorf("expected TestName 'TestAdd', got %q", result.Failures[0].TestName)
	}
	if result.Failures[0].Message == "" {
		t.Error("expected non-empty failure message")
	}
	if result.Failures[0].File == "" {
		t.Error("expected non-empty failure file")
	}
	if result.Failures[0].Line == 0 {
		t.Error("expected non-zero failure line number")
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
		t.Fatalf("expected 2 issues, got %d", len(result.Issues))
	}
	if result.Issues[0].Line <= 0 {
		t.Errorf("expected Issues[0].Line > 0, got %d", result.Issues[0].Line)
	}
	if result.Issues[0].File != "main.go" {
		t.Errorf("expected Issues[0].File 'main.go', got %q", result.Issues[0].File)
	}
}
