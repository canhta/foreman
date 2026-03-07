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

// OpenAIEmbedder generates embeddings using the OpenAI embeddings API.
type OpenAIEmbedder struct {
	httpClient *http.Client
	apiKey     string
	model      string
	baseURL    string
}

// NewOpenAIEmbedder creates an OpenAIEmbedder.
func NewOpenAIEmbedder(apiKey, model, baseURL string) *OpenAIEmbedder {
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	return &OpenAIEmbedder{
		apiKey:     apiKey,
		model:      model,
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 2 * time.Minute},
	}
}

type openaiEmbedRequest struct {
	Model          string   `json:"model"`
	EncodingFormat string   `json:"encoding_format"`
	Input          []string `json:"input"`
}

type openaiEmbedResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
}

// Embed returns embedding vectors for the given texts.
// Texts are batched in groups of 100 to stay within API limits.
func (e *OpenAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	const batchSize = 100
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

func (e *OpenAIEmbedder) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	reqBody, err := json.Marshal(openaiEmbedRequest{
		Model:          e.model,
		Input:          texts,
		EncodingFormat: "float",
	})
	if err != nil {
		return nil, fmt.Errorf("openai embed: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/v1/embeddings", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("openai embed: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai embed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openai embed: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai embed: status %d: %s", resp.StatusCode, body)
	}

	var embedResp openaiEmbedResponse
	if err := json.Unmarshal(body, &embedResp); err != nil {
		return nil, fmt.Errorf("openai embed: parse response: %w", err)
	}

	if len(embedResp.Data) != len(texts) {
		return nil, fmt.Errorf("openai embed: expected %d embeddings, got %d", len(texts), len(embedResp.Data))
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
