package tools_test

import (
	"testing"

	"github.com/canhta/foreman/internal/agent/tools"
)

func TestValidatePath_Allowed(t *testing.T) {
	if err := tools.ValidatePath("/work", "src/main.go"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidatePath_Traversal(t *testing.T) {
	if err := tools.ValidatePath("/work", "../../etc/passwd"); err == nil {
		t.Fatal("expected error for path traversal")
	}
}

func TestValidatePath_Absolute(t *testing.T) {
	if err := tools.ValidatePath("/work", "/etc/passwd"); err == nil {
		t.Fatal("expected error for absolute path")
	}
}

func TestCheckSecrets_DotEnv(t *testing.T) {
	if err := tools.CheckSecrets(".env", ""); err == nil {
		t.Fatal("expected error for .env path")
	}
}

func TestCheckSecrets_PemFile(t *testing.T) {
	if err := tools.CheckSecrets("certs/server.pem", ""); err == nil {
		t.Fatal("expected error for .pem path")
	}
}

func TestCheckSecrets_KeyFile(t *testing.T) {
	if err := tools.CheckSecrets("private.key", ""); err == nil {
		t.Fatal("expected error for .key path")
	}
}

func TestCheckSecrets_NormalFile(t *testing.T) {
	if err := tools.CheckSecrets("main.go", "package main"); err != nil {
		t.Fatalf("expected no error for normal file, got %v", err)
	}
}

func TestCheckSecrets_ContentPattern(t *testing.T) {
	content := "-----BEGIN RSA PRIVATE KEY-----\nMIIE...\n-----END RSA PRIVATE KEY-----"
	if err := tools.CheckSecrets("notes.txt", content); err == nil {
		t.Fatal("expected error for private key content")
	}
}
