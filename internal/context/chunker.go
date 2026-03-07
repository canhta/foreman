package context

import "strings"

// Chunk represents a slice of a source file.
type Chunk struct {
	FilePath  string
	Text      string
	StartLine int // 1-indexed, inclusive
	EndLine   int // 1-indexed, inclusive
}

// ChunkFile splits file content into overlapping 50-line chunks with 10-line overlap.
// Lines are 1-indexed. Window size is 50 lines with a step of 40 lines (10-line overlap).
// If the file has fewer than 10 lines, it is returned as a single chunk.
// Empty content returns an empty slice.
func ChunkFile(filePath, content string) []Chunk {
	if content == "" {
		return nil
	}

	lines := strings.Split(content, "\n")
	// Remove trailing empty line that results from a trailing newline.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	total := len(lines)
	if total == 0 {
		return nil
	}

	// Files with fewer than 10 lines → single chunk.
	if total < 10 {
		return []Chunk{{
			FilePath:  filePath,
			StartLine: 1,
			EndLine:   total,
			Text:      strings.Join(lines, "\n"),
		}}
	}

	const windowSize = 50
	const step = 40

	var chunks []Chunk
	for start := 0; start < total; start += step {
		end := start + windowSize
		if end > total {
			end = total
		}
		chunks = append(chunks, Chunk{
			FilePath:  filePath,
			StartLine: start + 1, // convert to 1-indexed
			EndLine:   end,       // already 1-indexed (exclusive becomes inclusive)
			Text:      strings.Join(lines[start:end], "\n"),
		})
		if end == total {
			break
		}
	}
	return chunks
}
