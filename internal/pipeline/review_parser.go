package pipeline

import (
	"regexp"
	"strings"

	"github.com/canhta/foreman/internal/models"
)

// statusRe matches STATUS: APPROVED|REJECTED|CHANGES_REQUESTED at line boundaries.
var statusRe = regexp.MustCompile(`(?i)(?:^|\n)STATUS:\s*(APPROVED|REJECTED|CHANGES_REQUESTED)`)

// ReviewResult holds the parsed output from any reviewer (spec, quality, final).
type ReviewResult struct {
	Summary     string
	ReviewNotes string
	RawOutput   string
	Issues      []string
	Approved    bool
	HasCritical bool
}

// ParseReviewOutput parses STATUS: APPROVED|REJECTED|CHANGES_REQUESTED from reviewer LLM output.
func ParseReviewOutput(raw string) *ReviewResult {
	result := &ReviewResult{RawOutput: raw}

	// Extract STATUS line
	if m := statusRe.FindStringSubmatch(raw); len(m) > 1 {
		status := strings.ToUpper(m[1])
		result.Approved = status == "APPROVED"
	}

	// Extract ISSUES section
	result.Issues = extractListSection(raw, "ISSUES")

	// Check for CRITICAL severity
	for _, issue := range result.Issues {
		if strings.Contains(strings.ToUpper(issue), "[CRITICAL]") {
			result.HasCritical = true
			break
		}
	}

	// Extract SUMMARY (final reviewer)
	if m := extractSingleLine(raw, "SUMMARY"); m != "" {
		result.Summary = m
	}

	// Extract REVIEW_NOTES (final reviewer)
	if m := extractSingleLine(raw, "REVIEW_NOTES"); m != "" {
		result.ReviewNotes = m
	}

	return result
}

// IssuesText returns all issues as a single string for feedback.
func (r *ReviewResult) IssuesText() string {
	return strings.Join(r.Issues, "\n")
}

func extractListSection(raw, header string) []string {
	// Match header as a full line (case-insensitive) using simple string comparison
	headerLine := strings.ToUpper(header) + ":"
	lines := strings.Split(raw, "\n")
	var items []string
	inSection := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.ToUpper(trimmed) == headerLine {
			inSection = true
			continue
		}
		if inSection {
			if strings.HasPrefix(trimmed, "- ") {
				item := strings.TrimPrefix(trimmed, "- ")
				// Skip "None" entries
				if strings.ToLower(item) != "none" {
					items = append(items, item)
				}
			} else if trimmed == "" {
				// Blank line might still be in section
				continue
			} else {
				// Non-list line ends the section
				inSection = false
			}
		}
	}
	return items
}

func extractSingleLine(raw, key string) string {
	re := regexp.MustCompile(`(?i)(?:^|\n)` + key + `:\s*(.+)`)
	if m := re.FindStringSubmatch(raw); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// issueSeverityRe matches optional severity tags at the start of an issue line,
// e.g. "[CRITICAL]", "[MAJOR]", "[IMPORTANT]", "[MINOR]".
var issueSeverityRe = regexp.MustCompile(`(?i)^\[([A-Z]+)\]\s*`)

// issueFileRe matches an optional file path prefix before a colon, e.g. "some/path.go: ".
var issueFileRe = regexp.MustCompile(`^([^\s:][^:]*\.\w+):\s*(.+)$`)

// ParseReviewOutputTyped parses reviewer LLM output into a typed models.ReviewOutput.
// It reuses the existing text-parsing logic and maps the results to the typed contract
// types defined in internal/models.
//
// Severity mapping (case-insensitive):
//   - [CRITICAL]  → "critical"
//   - [MAJOR]     → "major"
//   - [IMPORTANT] → "major"
//   - [MINOR]     → "minor"
//   - (untagged)  → "minor"
//
// The overall ReviewOutput.Severity is the highest severity found across all issues,
// or "none" when there are no issues.
func ParseReviewOutputTyped(raw string) *models.ReviewOutput {
	base := ParseReviewOutput(raw)

	out := &models.ReviewOutput{
		Approved:    base.Approved,
		Summary:     base.Summary,
		ReviewNotes: base.ReviewNotes,
	}

	for _, issueText := range base.Issues {
		issue := parseIssueTyped(issueText)
		out.Issues = append(out.Issues, issue)
	}

	out.Severity = computeOverallSeverity(out.Issues)
	return out
}

// parseIssueTyped converts a raw issue string into a typed ReviewIssue,
// extracting the severity tag and optional file prefix.
func parseIssueTyped(raw string) models.ReviewIssue {
	issue := models.ReviewIssue{}
	rest := raw

	// Extract severity tag, e.g. "[CRITICAL] ..."
	severity := "minor" // default
	if m := issueSeverityRe.FindStringSubmatch(rest); m != nil {
		severity = normalizeSeverity(m[1])
		rest = rest[len(m[0]):]
	}
	issue.Severity = severity

	// Extract file prefix, e.g. "some/path.go: description"
	if m := issueFileRe.FindStringSubmatch(strings.TrimSpace(rest)); m != nil {
		issue.File = m[1]
		issue.Description = m[2]
	} else {
		issue.Description = strings.TrimSpace(rest)
	}

	return issue
}

// normalizeSeverity maps raw severity tag text to canonical severity strings.
func normalizeSeverity(tag string) string {
	switch strings.ToUpper(tag) {
	case "CRITICAL":
		return "critical"
	case "MAJOR", "IMPORTANT":
		return "major"
	default:
		return "minor"
	}
}

// severityRank returns a numeric rank for comparison: higher = more severe.
func severityRank(s string) int {
	switch s {
	case "critical":
		return 3
	case "major":
		return 2
	case "minor":
		return 1
	default:
		return 0
	}
}

// computeOverallSeverity returns the highest severity across all issues,
// or "none" when the slice is empty or all issues have unrecognized severity strings.
func computeOverallSeverity(issues []models.ReviewIssue) string {
	best := ""
	for _, issue := range issues {
		if severityRank(issue.Severity) > severityRank(best) {
			best = issue.Severity
		}
	}
	if best == "" {
		return "none"
	}
	return best
}
