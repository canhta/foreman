package pipeline

import (
	"regexp"
	"strings"
)

// statusRe matches STATUS: APPROVED|REJECTED|CHANGES_REQUESTED at line boundaries.
var statusRe = regexp.MustCompile(`(?i)(?:^|\n)STATUS:\s*(APPROVED|REJECTED|CHANGES_REQUESTED)`)

// ReviewResult holds the parsed output from any reviewer (spec, quality, final).
type ReviewResult struct {
	Approved    bool
	Issues      []string
	HasCritical bool
	Summary     string
	ReviewNotes string
	RawOutput   string
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
