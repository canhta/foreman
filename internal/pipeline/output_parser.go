package pipeline

import (
	"fmt"
	"regexp"
	"strings"
)

type ParsedOutput struct {
	Files       []FileChange
	ParseErrors []string
}

type FileChange struct {
	Path    string
	Content string
	Patches []SearchReplace
	IsNew   bool
}

type SearchReplace struct {
	Search     string
	Replace    string
	FuzzyMatch bool
	Similarity float64
}

var (
	newFileRe     = regexp.MustCompile(`^\s*===\s*NEW FILE:\s*(.+?)\s*===\s*$`)
	newFileDashRe = regexp.MustCompile(`^\s*---\s*NEW FILE\s+(.+?)\s*---\s*$`)
	modifyFileRe  = regexp.MustCompile(`^\s*===\s*MODIFY FILE:\s*(.+?)\s*===\s*$`)
	modifyDashRe  = regexp.MustCompile(`^\s*---\s*MODIFY FILE\s+(.+?)\s*---\s*$`)
	endFileRe     = regexp.MustCompile(`^\s*===\s*END FILE\s*===\s*$`)
	endFileDashRe = regexp.MustCompile(`^\s*---\s*END FILE\s*---\s*$`)
	searchRe      = regexp.MustCompile(`<<<<\s*SEARCH`)
	replaceRe     = regexp.MustCompile(`<<<<\s*REPLACE|====\s*REPLACE`)
	endBlockRe    = regexp.MustCompile(`>>>>`)
)

func ParseImplementerOutput(raw string, similarityThreshold float64) (*ParsedOutput, error) {
	// Strategy 1: strict
	result, err := parseStrict(raw)
	if err == nil && len(result.Files) > 0 {
		return result, nil
	}

	// Strategy 2: permissive (strip markdown fences, commentary)
	cleaned := stripMarkdownFences(raw)
	result, err = parseStrict(cleaned)
	if err == nil && len(result.Files) > 0 {
		return result, nil
	}

	return nil, fmt.Errorf("failed to parse implementer output (all strategies failed). Raw length: %d", len(raw))
}

func parseStrict(raw string) (*ParsedOutput, error) {
	result := &ParsedOutput{}
	lines := strings.Split(raw, "\n")
	i := 0

	for i < len(lines) {
		line := lines[i]

		if path, ok := parseNewFileHeader(line); ok {
			i++
			var contentLines []string
			for i < len(lines) && !isEndFileLine(lines[i]) {
				contentLines = append(contentLines, lines[i])
				i++
			}
			if i < len(lines) {
				i++ // skip END FILE
			}
			result.Files = append(result.Files, FileChange{
				Path:    path,
				IsNew:   true,
				Content: strings.Join(contentLines, "\n"),
			})
			continue
		}

		if path, ok := parseModifyFileHeader(line); ok {
			i++
			var patches []SearchReplace
			for i < len(lines) && !isEndFileLine(lines[i]) {
				if searchRe.MatchString(lines[i]) {
					i++
					var searchLines []string
					for i < len(lines) && !endBlockRe.MatchString(lines[i]) {
						searchLines = append(searchLines, lines[i])
						i++
					}
					if i < len(lines) {
						i++ // skip >>>>
					}

					if i < len(lines) && replaceRe.MatchString(lines[i]) {
						i++
						var replaceLines []string
						for i < len(lines) && !endBlockRe.MatchString(lines[i]) {
							replaceLines = append(replaceLines, lines[i])
							i++
						}
						if i < len(lines) {
							i++ // skip >>>>
						}
						patches = append(patches, SearchReplace{
							Search:  strings.Join(searchLines, "\n"),
							Replace: strings.Join(replaceLines, "\n"),
						})
					}
				} else {
					i++
				}
			}
			if i < len(lines) {
				i++ // skip END FILE
			}
			result.Files = append(result.Files, FileChange{
				Path:    path,
				IsNew:   false,
				Patches: patches,
			})
			continue
		}

		i++
	}

	if len(result.Files) == 0 {
		return nil, fmt.Errorf("no files found in output")
	}
	return result, nil
}

func stripMarkdownFences(raw string) string {
	lines := strings.Split(raw, "\n")
	var result []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			continue
		}
		if strings.HasPrefix(trimmed, "~~~") {
			continue
		}
		result = append(result, line)
	}
	return strings.Join(result, "\n")
}

func parseNewFileHeader(line string) (string, bool) {
	if m := newFileRe.FindStringSubmatch(line); m != nil {
		return strings.TrimSpace(m[1]), true
	}
	if m := newFileDashRe.FindStringSubmatch(line); m != nil {
		return strings.TrimSpace(m[1]), true
	}
	return "", false
}

func parseModifyFileHeader(line string) (string, bool) {
	if m := modifyFileRe.FindStringSubmatch(line); m != nil {
		return strings.TrimSpace(m[1]), true
	}
	if m := modifyDashRe.FindStringSubmatch(line); m != nil {
		return strings.TrimSpace(m[1]), true
	}
	return "", false
}

func isEndFileLine(line string) bool {
	return endFileRe.MatchString(line) || endFileDashRe.MatchString(line)
}

// ApplySearchReplace applies a search/replace patch to file content.
func ApplySearchReplace(content string, sr *SearchReplace, threshold float64) (string, error) {
	// Try exact match first
	if idx := strings.Index(content, sr.Search); idx != -1 {
		return content[:idx] + sr.Replace + content[idx+len(sr.Search):], nil
	}

	// Fuzzy match: slide a window over lines
	searchLines := strings.Split(sr.Search, "\n")
	contentLines := strings.Split(content, "\n")
	windowSize := len(searchLines)

	if windowSize > len(contentLines) {
		return "", fmt.Errorf("SEARCH block (%d lines) larger than file (%d lines)", windowSize, len(contentLines))
	}

	bestSimilarity := 0.0
	bestStart := -1

	for i := 0; i <= len(contentLines)-windowSize; i++ {
		candidate := strings.Join(contentLines[i:i+windowSize], "\n")
		sim := normalizedSimilarity(sr.Search, candidate)
		if sim > bestSimilarity {
			bestSimilarity = sim
			bestStart = i
		}
	}

	if bestSimilarity >= threshold {
		sr.FuzzyMatch = true
		sr.Similarity = bestSimilarity
		var result []string
		result = append(result, contentLines[:bestStart]...)
		result = append(result, strings.Split(sr.Replace, "\n")...)
		result = append(result, contentLines[bestStart+windowSize:]...)
		return strings.Join(result, "\n"), nil
	}

	return "", fmt.Errorf("SEARCH block not found (best similarity: %.2f, threshold: %.2f)", bestSimilarity, threshold)
}

// normalizedSimilarity computes a simple character-level similarity ratio.
func normalizedSimilarity(a, b string) float64 {
	if a == b {
		return 1.0
	}
	maxLen := len(a)
	if len(b) > maxLen {
		maxLen = len(b)
	}
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
