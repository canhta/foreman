package context

import (
	"strings"
	"testing"
)

func TestChunkFile_SmallFile(t *testing.T) {
	// < 10 lines → single chunk
	lines := []string{"line1", "line2", "line3", "line4", "line5"}
	content := strings.Join(lines, "\n")

	chunks := ChunkFile("small.go", content)

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	c := chunks[0]
	if c.FilePath != "small.go" {
		t.Errorf("expected FilePath=small.go, got %q", c.FilePath)
	}
	if c.StartLine != 1 {
		t.Errorf("expected StartLine=1, got %d", c.StartLine)
	}
	if c.EndLine != 5 {
		t.Errorf("expected EndLine=5, got %d", c.EndLine)
	}
	if c.Text != content {
		t.Errorf("expected Text=%q, got %q", content, c.Text)
	}
}

func TestChunkFile_LargeFile(t *testing.T) {
	// 100 lines → multiple overlapping chunks with 10-line overlap
	var lines []string
	for i := 1; i <= 100; i++ {
		lines = append(lines, strings.Repeat("x", 20)) // non-trivial line content
	}
	content := strings.Join(lines, "\n")

	chunks := ChunkFile("large.go", content)

	// With window=50, step=40, total=100:
	// chunk 0: lines 1-50   (start=0, end=50)
	// chunk 1: lines 41-90  (start=40, end=90)
	// chunk 2: lines 81-100 (start=80, end=100)
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}

	// Verify first chunk.
	if chunks[0].StartLine != 1 || chunks[0].EndLine != 50 {
		t.Errorf("chunk 0: expected lines 1-50, got %d-%d", chunks[0].StartLine, chunks[0].EndLine)
	}

	// Verify second chunk overlaps with first (10-line overlap).
	if chunks[1].StartLine != 41 || chunks[1].EndLine != 90 {
		t.Errorf("chunk 1: expected lines 41-90, got %d-%d", chunks[1].StartLine, chunks[1].EndLine)
	}

	// Verify 10-line overlap between chunk 0 and chunk 1.
	overlap := chunks[0].EndLine - chunks[1].StartLine + 1
	if overlap != 10 {
		t.Errorf("expected 10-line overlap between chunk 0 and 1, got %d", overlap)
	}

	// Verify third chunk covers end of file.
	if chunks[2].StartLine != 81 || chunks[2].EndLine != 100 {
		t.Errorf("chunk 2: expected lines 81-100, got %d-%d", chunks[2].StartLine, chunks[2].EndLine)
	}
}

func TestChunkFile_WindowBoundary(t *testing.T) {
	// File of exactly 50 lines → single chunk
	var lines []string
	for i := 1; i <= 50; i++ {
		lines = append(lines, "line")
	}
	content := strings.Join(lines, "\n")

	chunks := ChunkFile("boundary.go", content)

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for exactly 50-line file, got %d", len(chunks))
	}
	c := chunks[0]
	if c.StartLine != 1 || c.EndLine != 50 {
		t.Errorf("expected lines 1-50, got %d-%d", c.StartLine, c.EndLine)
	}
}

func TestChunkFile_EmptyContent(t *testing.T) {
	chunks := ChunkFile("empty.go", "")
	if len(chunks) != 0 {
		t.Errorf("expected no chunks for empty content, got %d", len(chunks))
	}
}

func TestChunkFile_ExactlyNineLines(t *testing.T) {
	// 9 lines < 10 → single chunk
	var lines []string
	for i := 1; i <= 9; i++ {
		lines = append(lines, "line")
	}
	content := strings.Join(lines, "\n")

	chunks := ChunkFile("nine.go", content)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for 9-line file, got %d", len(chunks))
	}
	if chunks[0].StartLine != 1 || chunks[0].EndLine != 9 {
		t.Errorf("expected lines 1-9, got %d-%d", chunks[0].StartLine, chunks[0].EndLine)
	}
}
