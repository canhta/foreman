package tools

import (
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

	result, err := WebFetch(server.URL, "text", 10)
	require.NoError(t, err)
	assert.Equal(t, "Hello, World!", result)
}

func TestWebFetch_HTML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><body><h1>Title</h1><p>Content</p></body></html>"))
	}))
	defer server.Close()

	result, err := WebFetch(server.URL, "markdown", 10)
	require.NoError(t, err)
	assert.Contains(t, result, "Title")
	assert.Contains(t, result, "Content")
}

func TestWebFetch_SizeLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, 6*1024*1024)) // 6MB
	}))
	defer server.Close()

	_, err := WebFetch(server.URL, "text", 10)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "too large")
}
