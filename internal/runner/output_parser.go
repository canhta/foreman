package runner

import (
	"regexp"
	"strings"
)

// TestResult holds the parsed result of a test run.
type TestResult struct {
	Passed      bool
	TotalTests  int
	PassedTests int
	FailedTests int
	Failures    []TestFailure
	RawOutput   string
}

// TestFailure holds details about a single test failure.
type TestFailure struct {
	TestName string
	Message  string
	File     string
	Line     int
}

// LintResult holds the parsed result of a lint run.
type LintResult struct {
	Clean  bool
	Issues []LintIssue
}

// LintIssue holds details about a single lint issue.
type LintIssue struct {
	File    string
	Line    int
	Message string
}

var (
	goPassRe    = regexp.MustCompile(`--- PASS: (\S+)`)
	goFailRe    = regexp.MustCompile(`--- FAIL: (\S+)`)
	goFailMsgRe = regexp.MustCompile(`\s+(\S+\.go:\d+): (.+)`)
	lintIssueRe = regexp.MustCompile(`^(\S+\.go):(\d+):\d+: (.+)$`)
)

// ParseTestOutput parses test runner output and returns a TestResult.
func ParseTestOutput(output, lang string) TestResult {
	result := TestResult{RawOutput: output}

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if goPassRe.MatchString(line) {
			result.PassedTests++
			result.TotalTests++
		}
		if goFailRe.MatchString(line) {
			result.FailedTests++
			result.TotalTests++
			matches := goFailRe.FindStringSubmatch(line)
			failure := TestFailure{TestName: matches[1]}
			result.Failures = append(result.Failures, failure)
		}
	}

	// Attach failure messages to failures.
	for i, f := range result.Failures {
		for _, line := range lines {
			if goFailMsgRe.MatchString(line) && strings.Contains(output, f.TestName) {
				matches := goFailMsgRe.FindStringSubmatch(line)
				result.Failures[i].Message = matches[2]
				break
			}
		}
	}

	result.Passed = result.FailedTests == 0 && !strings.Contains(output, "FAIL")
	return result
}

// ParseLintOutput parses linter output and returns a LintResult.
func ParseLintOutput(output, lang string) LintResult {
	result := LintResult{Clean: true}
	if strings.TrimSpace(output) == "" {
		return result
	}

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if lintIssueRe.MatchString(line) {
			matches := lintIssueRe.FindStringSubmatch(line)
			result.Issues = append(result.Issues, LintIssue{
				File:    matches[1],
				Message: matches[3],
			})
			result.Clean = false
		}
	}
	return result
}
