package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseImplementerOutput_NewFile(t *testing.T) {
	raw := `=== NEW FILE: src/hello.ts ===
export function hello(): string {
  return "hello";
}
=== END FILE ===`

	result, err := ParseImplementerOutput(raw, 0.92, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(result.Files))
	}
	if !result.Files[0].IsNew {
		t.Error("expected file to be new")
	}
	if result.Files[0].Path != "src/hello.ts" {
		t.Errorf("expected path 'src/hello.ts', got %q", result.Files[0].Path)
	}
}

func TestParseImplementerOutput_ModifyFile(t *testing.T) {
	raw := `=== MODIFY FILE: src/app.ts ===
<<<< SEARCH
import { Router } from 'express';
import { authMiddleware } from '../lib/auth';
const app = express();
>>>>
<<<< REPLACE
import { Router } from 'express';
import { authMiddleware } from '../lib/auth';
import { validateInput } from '../lib/validation';
const app = express();
>>>>
=== END FILE ===`

	result, err := ParseImplementerOutput(raw, 0.92, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(result.Files))
	}
	if result.Files[0].IsNew {
		t.Error("expected file to be a modification")
	}
	if len(result.Files[0].Patches) != 1 {
		t.Fatalf("expected 1 patch, got %d", len(result.Files[0].Patches))
	}
}

func TestParseImplementerOutput_MultipleFiles(t *testing.T) {
	raw := `=== NEW FILE: src/test.ts ===
test content
=== END FILE ===

=== NEW FILE: src/impl.ts ===
impl content
=== END FILE ===`

	result, err := ParseImplementerOutput(raw, 0.92, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(result.Files))
	}
}

func TestParseImplementerOutput_PermissiveParsing(t *testing.T) {
	// LLM wraps in markdown fences
	raw := "```\n=== NEW FILE: src/hello.ts ===\nhello content\n=== END FILE ===\n```"

	result, err := ParseImplementerOutput(raw, 0.92, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(result.Files))
	}
}

func TestApplySearchReplace_ExactMatch(t *testing.T) {
	content := "line1\nline2\nline3\nline4"
	sr := &SearchReplace{
		Search:  "line2\nline3",
		Replace: "lineA\nlineB",
	}

	result, err := ApplySearchReplace(content, sr, 0.92)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "line1\nlineA\nlineB\nline4"
	if result != expected {
		t.Errorf("got %q, want %q", result, expected)
	}
}

func TestApplySearchReplace_FuzzyMatch(t *testing.T) {
	// Content with a minor whitespace difference from the search block
	content := "func hello() {\n\treturn  \"world\"\n}\n"
	sr := &SearchReplace{
		Search:  "return \"world\"",
		Replace: "return \"earth\"",
	}
	result, err := ApplySearchReplace(content, sr, 0.6)
	require.NoError(t, err)
	assert.Contains(t, result, "return \"earth\"")
	assert.True(t, sr.FuzzyMatch)
	assert.Greater(t, sr.Similarity, 0.6)
}

func TestApplySearchReplace_BelowThreshold(t *testing.T) {
	content := "completely unrelated content here\nnothing matches at all\n"
	sr := &SearchReplace{
		Search:  "func hello() { return 42 }",
		Replace: "func hello() { return 99 }",
	}
	_, err := ApplySearchReplace(content, sr, 0.9)
	require.Error(t, err)
}

func TestApplySearchReplace_SearchLargerThanFile(t *testing.T) {
	content := "short"
	sr := &SearchReplace{
		Search:  "line1\nline2\nline3\nline4\nline5\nline6",
		Replace: "replacement",
	}
	_, err := ApplySearchReplace(content, sr, 0.8)
	require.Error(t, err)
}

func TestNormalizedSimilarity_Identical(t *testing.T) {
	assert.Equal(t, 1.0, normalizedSimilarity("hello", "hello"))
}

func TestNormalizedSimilarity_BothEmpty(t *testing.T) {
	assert.Equal(t, 1.0, normalizedSimilarity("", ""))
}

func TestNormalizedSimilarity_OneEmpty(t *testing.T) {
	sim := normalizedSimilarity("hello", "")
	assert.Less(t, sim, 1.0)
	assert.GreaterOrEqual(t, sim, 0.0)
}

func TestLevenshtein_Insertions(t *testing.T) {
	assert.Equal(t, 1, levenshtein("cat", "cats"))
}

func TestLevenshtein_Deletions(t *testing.T) {
	assert.Equal(t, 1, levenshtein("cats", "cat"))
}

func TestLevenshtein_Substitutions(t *testing.T) {
	assert.Equal(t, 1, levenshtein("cat", "bat"))
}

func TestLevenshtein_EmptyStrings(t *testing.T) {
	assert.Equal(t, 5, levenshtein("hello", ""))
	assert.Equal(t, 5, levenshtein("", "hello"))
	assert.Equal(t, 0, levenshtein("", ""))
}
