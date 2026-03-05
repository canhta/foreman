package pipeline

import (
	"testing"
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
