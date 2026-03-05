package pipeline

import (
	"fmt"
	"strings"
)

// ShouldCreatePartialPR determines whether to create a partial PR.
func ShouldCreatePartialPR(totalTasks, completedTasks int, enabled bool) bool {
	if !enabled {
		return false
	}
	if completedTasks == 0 {
		return false
	}
	if completedTasks >= totalTasks {
		return false // Not partial if all done
	}
	return true
}

// FormatPartialPRComment creates the issue tracker comment for a partial PR.
func FormatPartialPRComment(prNumber, completed, total int, failedTask, failureReason string, remainingTasks []string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "⚠️ PR #%d opened with **partial** implementation (%d/%d tasks complete).\n\n", prNumber, completed, total)
	fmt.Fprintf(&sb, "**Failed task:** %s\n**Reason:** %s\n\n", failedTask, failureReason)
	fmt.Fprintf(&sb, "**Remaining tasks:**\n%s\n\n", FormatRemainingTasks(remainingTasks))
	sb.WriteString("A human developer should review the PR and complete the remaining work.")
	return sb.String()
}

// FormatRemainingTasks formats a list of remaining task names as markdown.
func FormatRemainingTasks(tasks []string) string {
	var sb strings.Builder
	for _, t := range tasks {
		fmt.Fprintf(&sb, "- %s\n", t)
	}
	return strings.TrimSpace(sb.String())
}
