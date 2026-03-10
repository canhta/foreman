package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidatePatch_ValidHunk(t *testing.T) {
	original := "line1\nline2\nline3\n"
	patch := "--- a/test.go\n+++ b/test.go\n@@ -1,3 +1,3 @@\n line1\n-line2\n+line2_modified\n line3\n"

	errs := ValidatePatchHunks(original, patch)
	assert.Empty(t, errs)
}

func TestValidatePatch_InvalidContext(t *testing.T) {
	original := "line1\nline2\nline3\n"
	patch := "--- a/test.go\n+++ b/test.go\n@@ -1,3 +1,3 @@\n lineX\n-line2\n+line2_modified\n line3\n"
	// "lineX" doesn't match "line1"
	errs := ValidatePatchHunks(original, patch)
	assert.NotEmpty(t, errs)
}

func TestValidatePatch_NewFile(t *testing.T) {
	patch := "--- /dev/null\n+++ b/new_file.go\n@@ -0,0 +1,3 @@\n+package main\n+\n+func main() {}\n"
	// New file — no original content to validate against
	errs := ValidatePatchHunks("", patch)
	assert.Empty(t, errs)
}

func TestValidatePatch_EmptyOriginal(t *testing.T) {
	// When original is empty but patch has a valid header, no context to validate
	patch := "--- a/test.go\n+++ b/test.go\n@@ -1,1 +1,1 @@\n-old\n+new\n"
	errs := ValidatePatchHunks("", patch)
	assert.Empty(t, errs)
}
