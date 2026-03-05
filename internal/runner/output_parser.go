package runner

import (
	"regexp"
	"strconv"
	"strings"
)

// TestResult holds the parsed result of a test run.
type TestResult struct {
	RawOutput   string
	Failures    []TestFailure
	TotalTests  int
	PassedTests int
	FailedTests int
	Passed      bool
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
	Issues []LintIssue
	Clean  bool
}

// LintIssue holds details about a single lint issue.
type LintIssue struct {
	File    string
	Message string
	Line    int
}

var (
	goPassRe    = regexp.MustCompile(`--- PASS: (\S+)`)
	goFailRe    = regexp.MustCompile(`--- FAIL: (\S+)`)
	goFailMsgRe = regexp.MustCompile(`^\s+(\S+\.go):(\d+): (.+)$`)
	lintIssueRe = regexp.MustCompile(`^(\S+\.go):(\d+):\d+: (.+)$`)
)

// ParseTestOutput parses test runner output and returns a TestResult.
func ParseTestOutput(output, lang string) TestResult {
	result := TestResult{RawOutput: output}

	lines := strings.Split(output, "\n")

	// Stateful parser: track the current active failure block index (-1 = none).
	activeFailure := -1

	for _, line := range lines {
		if goPassRe.MatchString(line) {
			result.PassedTests++
			result.TotalTests++
			activeFailure = -1
		} else if goFailRe.MatchString(line) {
			result.FailedTests++
			result.TotalTests++
			matches := goFailRe.FindStringSubmatch(line)
			result.Failures = append(result.Failures, TestFailure{TestName: matches[1]})
			activeFailure = len(result.Failures) - 1
		} else if activeFailure >= 0 && goFailMsgRe.MatchString(line) {
			// Associate failure detail with the currently active failure block.
			matches := goFailMsgRe.FindStringSubmatch(line)
			lineNum, _ := strconv.Atoi(matches[2])
			f := &result.Failures[activeFailure]
			if f.Message == "" {
				f.File = matches[1]
				f.Line = lineNum
				f.Message = matches[3]
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
			lineNum, _ := strconv.Atoi(matches[2])
			result.Issues = append(result.Issues, LintIssue{
				File:    matches[1],
				Line:    lineNum,
				Message: matches[3],
			})
			result.Clean = false
		}
	}
	return result
}
