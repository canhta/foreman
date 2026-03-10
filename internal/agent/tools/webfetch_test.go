package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebFetch_PlainText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("Hello, World!"))
	}))
	defer server.Close()

	result, err := WebFetch(context.Background(), server.URL, "text", 10)
	require.NoError(t, err)
	assert.Equal(t, "Hello, World!", result)
}

func TestWebFetch_HTML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><body><h1>Title</h1><p>Content</p></body></html>"))
	}))
	defer server.Close()

	result, err := WebFetch(context.Background(), server.URL, "markdown", 10)
	require.NoError(t, err)
	assert.Contains(t, result, "Title")
	assert.Contains(t, result, "Content")
}

func TestWebFetch_SizeLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, 6*1024*1024)) // 6MB
	}))
	defer server.Close()

	_, err := WebFetch(context.Background(), server.URL, "text", 10)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "too large")
}

func TestHTMLToMarkdown_Tags(t *testing.T) {
	tests := []struct {
		input    string
		contains string
	}{
		{"<h1>Header One</h1>", "# Header One"},
		{"<h2>Header Two</h2>", "## Header Two"},
		{"<h6>Header Six</h6>", "###### Header Six"},
		{"<p>Paragraph</p>", "Paragraph"},
		{"<li>Item</li>", "- Item"},
		{"<code>snippet</code>", "`snippet`"},
	}
	for _, tc := range tests {
		result := htmlToMarkdown(tc.input)
		assert.Contains(t, result, tc.contains, "htmlToMarkdown(%q)", tc.input)
	}
}

func TestStripHTMLTags(t *testing.T) {
	input := "<div><p>Hello <b>world</b></p></div>"
	result := stripHTMLTags(input)
	assert.Equal(t, "Hello world", result)
}

func TestWebFetch_CancelledContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handler that would block — but context is already cancelled
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := WebFetch(ctx, server.URL, "text", 10)
	assert.Error(t, err)
}

func TestWebFetch_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	_, err := WebFetch(context.Background(), server.URL, "text", 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}
