package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// AnthropicEmbedder generates embeddings using the Voyage AI embeddings API
// (Anthropic's recommended partner for code embeddings).
type AnthropicEmbedder struct {
	httpClient *http.Client
	apiKey     string
	model      string
}

// NewAnthropicEmbedder creates an AnthropicEmbedder.
func NewAnthropicEmbedder(apiKey, model string) *AnthropicEmbedder {
	return &AnthropicEmbedder{
		apiKey:     apiKey,
		model:      model,
		httpClient: &http.Client{Timeout: 2 * time.Minute},
	}
}

type anthropicEmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type anthropicEmbedResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
}

// Embed returns embedding vectors for the given texts.
// Texts are batched in groups of 128 to stay within Voyage AI API limits.
func (e *AnthropicEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	const batchSize = 128
	var all [][]float32
	for i := 0; i < len(texts); i += batchSize {
		end := i + batchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch, err := e.embedBatch(ctx, texts[i:end])
		if err != nil {
			return nil, err
		}
		all = append(all, batch...)
	}
	return all, nil
}

func (e *AnthropicEmbedder) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	reqBody, err := json.Marshal(anthropicEmbedRequest{
		Model: e.model,
		Input: texts,
	})
	if err != nil {
		return nil, fmt.Errorf("anthropic embed: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.voyageai.com/v1/embeddings", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("anthropic embed: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic embed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("anthropic embed: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic embed: status %d: %s", resp.StatusCode, body)
	}

	var embedResp anthropicEmbedResponse
	if err := json.Unmarshal(body, &embedResp); err != nil {
		return nil, fmt.Errorf("anthropic embed: parse response: %w", err)
	}

	result := make([][]float32, len(embedResp.Data))
	for i, d := range embedResp.Data {
		vec := make([]float32, len(d.Embedding))
		for j, v := range d.Embedding {
			vec[j] = float32(v)
		}
		result[i] = vec
	}
	return result, nil
}
