package git

import (
	"testing"
)

func TestCheckForbiddenFiles_Clean(t *testing.T) {
	files := []string{"main.go", "auth/login.go", "README.md"}
	forbidden := CheckForbiddenFiles(files, DefaultForbiddenPatterns)
	if len(forbidden) != 0 {
		t.Errorf("expected no forbidden files, got %v", forbidden)
	}
}

func TestCheckForbiddenFiles_Secrets(t *testing.T) {
	files := []string{"main.go", ".env", "certs/server.key", "config.pem"}
	forbidden := CheckForbiddenFiles(files, DefaultForbiddenPatterns)
	if len(forbidden) != 3 {
		t.Errorf("expected 3 forbidden files, got %d: %v", len(forbidden), forbidden)
	}
}

func TestCheckForbiddenFiles_SSHDir(t *testing.T) {
	files := []string{".ssh/id_rsa", ".aws/credentials"}
	forbidden := CheckForbiddenFiles(files, DefaultForbiddenPatterns)
	if len(forbidden) != 2 {
		t.Errorf("expected 2 forbidden files, got %d: %v", len(forbidden), forbidden)
	}
}
