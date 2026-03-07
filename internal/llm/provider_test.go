// internal/llm/provider_test.go
package llm

import (
	"testing"

	"github.com/canhta/foreman/internal/models"
)

func TestNewProviderFromConfig(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		wantName string
		cfg      models.LLMConfig
		wantErr  bool
	}{
		{
			name:     "anthropic",
			provider: "anthropic",
			cfg: models.LLMConfig{
				Anthropic: models.LLMProviderConfig{APIKey: "key", BaseURL: "http://localhost"},
			},
			wantName: "anthropic",
		},
		{
			name:     "openai",
			provider: "openai",
			cfg: models.LLMConfig{
				OpenAI: models.LLMProviderConfig{APIKey: "key", BaseURL: "http://localhost"},
			},
			wantName: "openai",
		},
		{
			name:     "openrouter",
			provider: "openrouter",
			cfg: models.LLMConfig{
				OpenRouter: models.LLMProviderConfig{APIKey: "key", BaseURL: "http://localhost"},
			},
			wantName: "openrouter",
		},
		{
			name:     "local",
			provider: "local",
			cfg: models.LLMConfig{
				Local: models.LLMProviderConfig{BaseURL: "http://localhost:11434"},
			},
			wantName: "local",
		},
		{
			name:     "unknown",
			provider: "unknown",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := NewProviderFromConfig(tt.provider, tt.cfg)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p.ProviderName() != tt.wantName {
				t.Errorf("expected %s, got %s", tt.wantName, p.ProviderName())
			}
		})
	}
}
