package tools

import (
	"fmt"
	"strings"
	"unicode"
)

// maxFuzzyFileLines caps the file size for fuzzy matching to avoid O(n*m) slowness.
const maxFuzzyFileLines = 500

// ApplyEditWithFallback tries multiple replacement strategies in order.
// Returns the result, strategy name used, and error.
func ApplyEditWithFallback(content, oldStr, newStr string) (string, string, error) {
	if oldStr == "" {
		return "", "", fmt.Errorf("old_string must not be empty")
	}

	strategies := []struct {
		name string
		fn   func(string, string, string) (string, bool)
	}{
		{"simple", SimpleReplace},
		{"line_trimmed", LineTrimmedReplace},
		{"block_anchor", BlockAnchorReplace},
		{"whitespace_normalized", WhitespaceNormalizedReplace},
		{"indent_flexible", IndentFlexibleReplace},
	}

	for _, s := range strategies {
		if result, ok := s.fn(content, oldStr, newStr); ok {
			return result, s.name, nil
		}
	}

	// Last resort: find best match and auto-apply if confidence is high.
	// Only attempt on files within the size cap to avoid latency.
	if strings.Count(content, "\n") <= maxFuzzyFileLines {
		match, sim := FindBestMatch(content, oldStr)
		if sim > 0.8 && match != "" {
			result := strings.Replace(content, match, newStr, 1)
			return result, fmt.Sprintf("fuzzy(%.0f%%)", sim*100), nil
		}
		return "", "", fmt.Errorf("no match found for old_string (best similarity: %.0f%%)", sim*100)
	}

	return "", "", fmt.Errorf("no match found for old_string")
}

// SimpleReplace does exact string replacement.
func SimpleReplace(content, oldStr, newStr string) (string, bool) {
	if !strings.Contains(content, oldStr) {
		return "", false
	}
	return strings.Replace(content, oldStr, newStr, 1), true
}

// LineTrimmedReplace trims each line before matching.
func LineTrimmedReplace(content, oldStr, newStr string) (string, bool) {
	contentLines := strings.Split(content, "\n")
	oldLines := strings.Split(oldStr, "\n")

	idx := findLineTrimmedMatch(contentLines, oldLines)
	if idx < 0 {
		return "", false
	}

	before := strings.Join(contentLines[:idx], "\n")
	after := strings.Join(contentLines[idx+len(oldLines):], "\n")
	result := before
	if before != "" {
		result += "\n"
	}
	result += newStr
	if after != "" {
		result += "\n" + after
	}
	return result, true
}

func findLineTrimmedMatch(content, search []string) int {
	for i := range len(content) - len(search) + 1 {
		match := true
		for j := range len(search) {
			if strings.TrimSpace(content[i+j]) != strings.TrimSpace(search[j]) {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// WhitespaceNormalizedReplace collapses all non-newline whitespace runs to single space before matching.
func WhitespaceNormalizedReplace(content, oldStr, newStr string) (string, bool) {
	normalize := func(s string) string {
		var b strings.Builder
		prevSpace := false
		for _, r := range s {
			if unicode.IsSpace(r) && r != '\n' {
				if !prevSpace {
					b.WriteRune(' ')
				}
				prevSpace = true
			} else {
				b.WriteRune(r)
				prevSpace = false
			}
		}
		return b.String()
	}

	contentLines := strings.Split(content, "\n")
	oldLines := strings.Split(oldStr, "\n")

	for i := range len(contentLines) - len(oldLines) + 1 {
		match := true
		for j := range len(oldLines) {
			if normalize(contentLines[i+j]) != normalize(oldLines[j]) {
				match = false
				break
			}
		}
		if match {
			before := strings.Join(contentLines[:i], "\n")
			after := strings.Join(contentLines[i+len(oldLines):], "\n")
			result := before
			if before != "" {
				result += "\n"
			}
			result += newStr
			if after != "" {
				result += "\n" + after
			}
			return result, true
		}
	}

	return "", false
}

// IndentFlexibleReplace matches content ignoring indentation level differences.
func IndentFlexibleReplace(content, oldStr, newStr string) (string, bool) {
	contentLines := strings.Split(content, "\n")
	oldLines := strings.Split(oldStr, "\n")

	if len(oldLines) == 0 {
		return "", false
	}

	for i := range len(contentLines) - len(oldLines) + 1 {
		offset := detectIndentOffset(contentLines[i], oldLines[0])
		if offset == -1 {
			continue
		}

		match := true
		for j := 1; j < len(oldLines); j++ {
			if strings.TrimSpace(contentLines[i+j]) != strings.TrimSpace(oldLines[j]) {
				match = false
				break
			}
		}

		if match {
			newLines := strings.Split(newStr, "\n")
			adjusted := make([]string, len(newLines))
			for k, line := range newLines {
				adjusted[k] = applyIndentOffset(line, offset)
			}

			before := strings.Join(contentLines[:i], "\n")
			after := strings.Join(contentLines[i+len(oldLines):], "\n")
			result := before
			if before != "" {
				result += "\n"
			}
			result += strings.Join(adjusted, "\n")
			if after != "" {
				result += "\n" + after
			}
			return result, true
		}
	}
	return "", false
}

// BlockAnchorReplace matches using first and last lines as anchors.
func BlockAnchorReplace(content, oldStr, newStr string) (string, bool) {
	oldLines := strings.Split(oldStr, "\n")
	if len(oldLines) < 2 {
		return "", false
	}

	firstLine := strings.TrimSpace(oldLines[0])
	lastLine := strings.TrimSpace(oldLines[len(oldLines)-1])

	// Skip if anchors are identical — too ambiguous.
	if firstLine == lastLine {
		return "", false
	}

	contentLines := strings.Split(content, "\n")
	for i := range len(contentLines) {
		if strings.TrimSpace(contentLines[i]) != firstLine {
			continue
		}
		for j := i + 1; j < len(contentLines); j++ {
			if strings.TrimSpace(contentLines[j]) != lastLine {
				continue
			}
			before := strings.Join(contentLines[:i], "\n")
			after := strings.Join(contentLines[j+1:], "\n")
			result := before
			if before != "" {
				result += "\n"
			}
			result += newStr
			if after != "" {
				result += "\n" + after
			}
			return result, true
		}
	}
	return "", false
}

// FindBestMatch finds the most similar substring using Levenshtein distance.
func FindBestMatch(content, search string) (string, float64) {
	contentLines := strings.Split(content, "\n")
	searchLines := strings.Split(search, "\n")
	searchLen := len(searchLines)

	if searchLen == 0 || len(contentLines) == 0 {
		return "", 0
	}

	bestSim := 0.0
	bestMatch := ""

	for i := range len(contentLines) - searchLen + 1 {
		candidate := strings.Join(contentLines[i:i+searchLen], "\n")
		sim := stringSimilarity(candidate, search)
		if sim > bestSim {
			bestSim = sim
			bestMatch = candidate
		}
	}

	return bestMatch, bestSim
}

func stringSimilarity(a, b string) float64 {
	if a == b {
		return 1.0
	}
	maxLen := max(len(a), len(b))
	if maxLen == 0 {
		return 1.0
	}
	dist := levenshtein(a, b)
	return 1.0 - float64(dist)/float64(maxLen)
}

func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := range lb + 1 {
		prev[j] = j
	}

	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(curr[j-1]+1, min(prev[j]+1, prev[j-1]+cost))
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

func detectIndentOffset(contentLine, searchLine string) int {
	trimC := strings.TrimSpace(contentLine)
	trimS := strings.TrimSpace(searchLine)
	if trimC != trimS {
		return -1
	}
	return countLeadingWhitespace(contentLine) - countLeadingWhitespace(searchLine)
}

func applyIndentOffset(line string, offset int) string {
	if line == "" || offset == 0 {
		return line
	}
	if offset > 0 {
		// Preserve existing indentation style: detect if line uses spaces or tabs
		leading := countLeadingWhitespace(line)
		if leading > 0 && line[0] == ' ' {
			return strings.Repeat(" ", offset*4) + line
		}
		return strings.Repeat("\t", offset) + line
	}
	// Remove leading whitespace characters (negative offset = shallower indent)
	remove := -offset
	count := 0
	for i, r := range line {
		if count >= remove {
			return line[i:]
		}
		if r == '\t' || r == ' ' {
			count++
		} else {
			break
		}
	}
	return strings.TrimLeft(line, "\t ")
}

func countLeadingWhitespace(s string) int {
	count := 0
	for _, r := range s {
		if r == '\t' || r == ' ' {
			count++
		} else {
			break
		}
	}
	return count
}
