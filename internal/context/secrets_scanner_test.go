package context

import (
	"testing"

	"github.com/canhta/foreman/internal/models"
)

func TestSecretsScanner_ScanFile(t *testing.T) {
	scanner := NewSecretsScanner(&models.SecretsConfig{
		AlwaysExclude: []string{".env", ".env.*", "*.pem"},
	})

	tests := []struct {
		name       string
		path       string
		content    string
		hasSecrets bool
	}{
		{
			name:       "clean file",
			path:       "main.go",
			content:    "package main\nfunc main() {}",
			hasSecrets: false,
		},
		{
			name:       "AWS access key",
			path:       "config.go",
			content:    "key := \"AKIAIOSFODNN7EXAMPLE\"",
			hasSecrets: true,
		},
		{
			name:       "always excluded .env",
			path:       ".env",
			content:    "FOO=bar",
			hasSecrets: true,
		},
		{
			name:       "always excluded .env.local",
			path:       ".env.local",
			content:    "FOO=bar",
			hasSecrets: true,
		},
		{
			name:       "private key header",
			path:       "cert.go",
			content:    "-----BEGIN RSA PRIVATE KEY-----\ndata",
			hasSecrets: true,
		},
		{
			name:       "always excluded pem file",
			path:       "server.pem",
			content:    "cert data",
			hasSecrets: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := scanner.ScanFile(tt.path, tt.content)
			if result.HasSecrets != tt.hasSecrets {
				t.Errorf("ScanFile(%q) HasSecrets = %v, want %v", tt.path, result.HasSecrets, tt.hasSecrets)
			}
		})
	}
}
