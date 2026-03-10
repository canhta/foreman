package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	maxResponseSize     = 5 * 1024 * 1024 // 5MB
	defaultFetchTimeout = 30 * time.Second
)

// WebFetch fetches a URL and returns content in the specified format.
// format: "text" (raw/stripped), "markdown" (HTML->markdown), "html" (raw HTML).
func WebFetch(ctx context.Context, url, format string, timeoutSecs int) (string, error) {
	timeout := defaultFetchTimeout
	if timeoutSecs > 0 {
		timeout = time.Duration(timeoutSecs) * time.Second
	}

	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	req.Header.Set("User-Agent", "Foreman/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxResponseSize)+1))
	if err != nil {
		return "", fmt.Errorf("read failed: %w", err)
	}
	if len(body) > maxResponseSize {
		return "", fmt.Errorf("response too large (>5MB)")
	}

	content := string(body)
	contentType := resp.Header.Get("Content-Type")

	switch format {
	case "markdown":
		if strings.Contains(contentType, "text/html") {
			return htmlToMarkdown(content), nil
		}
		return content, nil
	case "html":
		return content, nil
	default: // "text"
		if strings.Contains(contentType, "text/html") {
			return stripHTMLTags(content), nil
		}
		return content, nil
	}
}

func htmlToMarkdown(html string) string {
	result := html
	for i := 6; i >= 1; i-- {
		prefix := strings.Repeat("#", i) + " "
		result = strings.ReplaceAll(result, fmt.Sprintf("<h%d>", i), prefix)
		result = strings.ReplaceAll(result, fmt.Sprintf("</h%d>", i), "\n")
	}
	result = strings.ReplaceAll(result, "<p>", "\n\n")
	result = strings.ReplaceAll(result, "</p>", "")
	result = strings.ReplaceAll(result, "<br>", "\n")
	result = strings.ReplaceAll(result, "<br/>", "\n")
	result = strings.ReplaceAll(result, "<br />", "\n")
	result = strings.ReplaceAll(result, "<li>", "\n- ")
	result = strings.ReplaceAll(result, "</li>", "")
	result = strings.ReplaceAll(result, "<code>", "`")
	result = strings.ReplaceAll(result, "</code>", "`")
	result = strings.ReplaceAll(result, "<pre>", "\n```\n")
	result = strings.ReplaceAll(result, "</pre>", "\n```\n")
	result = stripHTMLTags(result)
	return strings.TrimSpace(result)
}

func stripHTMLTags(s string) string {
	var result strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			result.WriteRune(r)
		}
	}
	return result.String()
}

// webFetchTool implements the Tool interface for HTTP content retrieval.
type webFetchTool struct{}

func (t *webFetchTool) Name() string { return "WebFetch" }
func (t *webFetchTool) Description() string {
	return "Fetch content from a URL, optionally converting HTML to markdown"
}
func (t *webFetchTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"url": {"type": "string", "description": "The URL to fetch"},
			"format": {"type": "string", "enum": ["text", "markdown", "html"], "description": "Output format (default: text)"},
			"timeout": {"type": "integer", "description": "Request timeout in seconds (default: 30)"}
		},
		"required": ["url"]
	}`)
}
func (t *webFetchTool) Execute(ctx context.Context, _ string, input json.RawMessage) (string, error) {
	var args struct {
		URL     string `json:"url"`
		Format  string `json:"format"`
		Timeout int    `json:"timeout"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("webfetch: %w", err)
	}
	if args.Format == "" {
		args.Format = "text"
	}
	return WebFetch(ctx, args.URL, args.Format, args.Timeout)
}
