package tools

import (
	"fmt"
	"strings"
	"unicode"
)

// ApplyEditWithFallback tries multiple replacement strategies in order.
// Returns the result, strategy name used, and error.
func ApplyEditWithFallback(content, old, new string) (string, string, error) {
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
		if result, ok := s.fn(content, old, new); ok {
			return result, s.name, nil
		}
	}

	// Last resort: find best match and auto-apply if confidence is high
	match, sim := FindBestMatch(content, old)
	if sim > 0.8 && match != "" {
		result := strings.Replace(content, match, new, 1)
		return result, "fuzzy", nil
	}

	return "", "", fmt.Errorf("no match found for old_string (best similarity: %.0f%%)", sim*100)
}

// SimpleReplace does exact string replacement.
func SimpleReplace(content, old, new string) (string, bool) {
	if !strings.Contains(content, old) {
		return "", false
	}
	return strings.Replace(content, old, new, 1), true
}

// LineTrimmedReplace trims each line before matching.
func LineTrimmedReplace(content, old, new string) (string, bool) {
	contentLines := strings.Split(content, "\n")
	oldLines := strings.Split(old, "\n")

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
	result += new
	if after != "" {
		result += "\n" + after
	}
	return result, true
}

func findLineTrimmedMatch(content, search []string) int {
	for i := 0; i <= len(content)-len(search); i++ {
		match := true
		for j := 0; j < len(search); j++ {
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

// WhitespaceNormalizedReplace collapses all whitespace runs to single space.
func WhitespaceNormalizedReplace(content, old, new string) (string, bool) {
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

	normContent := normalize(content)
	normOld := normalize(old)

	if !strings.Contains(normContent, normOld) {
		return "", false
	}

	// Find the matching region in the original content by comparing
	// normalized lines to locate which content lines to replace.
	contentLines := strings.Split(content, "\n")
	oldLines := strings.Split(old, "\n")

	for i := 0; i <= len(contentLines)-len(oldLines); i++ {
		match := true
		for j := 0; j < len(oldLines); j++ {
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
			result += new
			if after != "" {
				result += "\n" + after
			}
			return result, true
		}
	}

	return "", false
}

// IndentFlexibleReplace matches content ignoring indentation level differences.
func IndentFlexibleReplace(content, old, new string) (string, bool) {
	contentLines := strings.Split(content, "\n")
	oldLines := strings.Split(old, "\n")

	if len(oldLines) == 0 {
		return "", false
	}

	for i := 0; i <= len(contentLines)-len(oldLines); i++ {
		offset := detectIndentOffset(contentLines[i], oldLines[0])
		if offset < 0 {
			continue
		}

		match := true
		for j := 1; j < len(oldLines); j++ {
			if !matchWithIndentOffset(contentLines[i+j], oldLines[j]) {
				match = false
				break
			}
		}

		if match {
			newLines := strings.Split(new, "\n")
			var adjusted []string
			for _, line := range newLines {
				adjusted = append(adjusted, applyIndentOffset(line, offset))
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
func BlockAnchorReplace(content, old, new string) (string, bool) {
	oldLines := strings.Split(old, "\n")
	if len(oldLines) < 2 {
		return "", false
	}

	contentLines := strings.Split(content, "\n")
	firstLine := strings.TrimSpace(oldLines[0])
	lastLine := strings.TrimSpace(oldLines[len(oldLines)-1])

	for i := 0; i < len(contentLines); i++ {
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
			result += new
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

	for i := 0; i <= len(contentLines)-searchLen; i++ {
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
	for j := 0; j <= lb; j++ {
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
	contentIndent := countLeadingWhitespace(contentLine)
	searchIndent := countLeadingWhitespace(searchLine)
	trimC := strings.TrimSpace(contentLine)
	trimS := strings.TrimSpace(searchLine)
	if trimC != trimS {
		return -1
	}
	return contentIndent - searchIndent
}

func matchWithIndentOffset(contentLine, searchLine string) bool {
	return strings.TrimSpace(contentLine) == strings.TrimSpace(searchLine)
}

func applyIndentOffset(line string, offset int) string {
	if offset <= 0 || line == "" {
		return line
	}
	return strings.Repeat("\t", offset) + line
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
