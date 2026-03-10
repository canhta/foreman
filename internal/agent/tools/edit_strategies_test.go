package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSimpleReplacer(t *testing.T) {
	content := "func main() {\n\tfmt.Println(\"hello\")\n}"
	old := "func main() {\n\tfmt.Println(\"hello\")\n}"
	new := "func main() {\n\tfmt.Println(\"world\")\n}"

	result, ok := SimpleReplace(content, old, new)
	assert.True(t, ok)
	assert.Contains(t, result, "world")
}

func TestLineTrimmedReplacer(t *testing.T) {
	content := "func main() {\n\tfmt.Println(\"hello\")\n}"
	old := "func main() {\n  fmt.Println(\"hello\")\n}" // spaces instead of tab

	result, ok := LineTrimmedReplace(content, old, "func main() {\n\tfmt.Println(\"world\")\n}")
	assert.True(t, ok)
	assert.Contains(t, result, "world")
}

func TestIndentationFlexibleReplacer(t *testing.T) {
	content := "\t\tfunc inner() {\n\t\t\treturn nil\n\t\t}"
	old := "func inner() {\n\treturn nil\n}" // different indentation level

	result, ok := IndentFlexibleReplace(content, old, "func inner() {\n\treturn true\n}")
	assert.True(t, ok)
	assert.Contains(t, result, "return true")
}

func TestWhitespaceNormalizedReplacer(t *testing.T) {
	content := "if   err != nil {\n\treturn   err\n}"
	old := "if err != nil {\n\treturn err\n}"

	result, ok := WhitespaceNormalizedReplace(content, old, "if err != nil {\n\treturn fmt.Errorf(\"wrap: %w\", err)\n}")
	assert.True(t, ok)
	assert.Contains(t, result, "wrap")
}

func TestBlockAnchorReplacer(t *testing.T) {
	content := "func foo() {\n\tline1\n\tline2\n\tline3\n\tline4\n}"
	old := "func foo() {\n\tline1\n\tline2\n\tline3\n\tline4\n}"

	result, ok := BlockAnchorReplace(content, old, "func bar() {\n\tnewcode\n}")
	assert.True(t, ok)
	assert.Contains(t, result, "bar")
}

func TestFindBestMatch_Levenshtein(t *testing.T) {
	content := "func handleRequest(w http.ResponseWriter, r *http.Request) {"
	search := "func handleReqeust(w http.ResponseWriter, r *http.Request) {" // typo

	match, similarity := FindBestMatch(content, search)
	assert.NotEmpty(t, match)
	assert.Greater(t, similarity, 0.8)
}

func TestApplyEditWithFallback(t *testing.T) {
	content := "func main() {\n  fmt.Println(\"hello\")\n}"
	old := "func main() {\n\tfmt.Println(\"hello\")\n}" // tab vs spaces
	newStr := "func main() {\n\tfmt.Println(\"world\")\n}"

	result, strategy, err := ApplyEditWithFallback(content, old, newStr)
	assert.NoError(t, err)
	assert.Contains(t, result, "world")
	assert.NotEqual(t, "simple", strategy)
}
