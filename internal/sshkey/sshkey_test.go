package sshkey_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/canhta/foreman/internal/sshkey"
)

func TestEnsure_CreatesKeypair(t *testing.T) {
	dir := t.TempDir()

	kp, err := sshkey.Ensure(dir)
	if err != nil {
		t.Fatalf("Ensure failed: %v", err)
	}

	// Paths are inside dir.
	if !strings.HasPrefix(kp.PrivateKeyPath, dir) {
		t.Errorf("private key path %q not under %q", kp.PrivateKeyPath, dir)
	}
	if !strings.HasPrefix(kp.PublicKeyPath, dir) {
		t.Errorf("public key path %q not under %q", kp.PublicKeyPath, dir)
	}

	// Files exist on disk.
	if _, statErr := os.Stat(kp.PrivateKeyPath); statErr != nil {
		t.Errorf("private key file missing: %v", statErr)
	}
	if _, statErr := os.Stat(kp.PublicKeyPath); statErr != nil {
		t.Errorf("public key file missing: %v", statErr)
	}

	// Private key has mode 0600.
	info, err := os.Stat(kp.PrivateKeyPath)
	if err != nil {
		t.Fatalf("stat private key: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("private key mode = %o, want 0600", info.Mode().Perm())
	}

	// Public key line looks like an SSH public key.
	if !strings.HasPrefix(kp.PublicKeyLine, "ssh-ed25519 ") {
		t.Errorf("unexpected public key format: %q", kp.PublicKeyLine)
	}
}

func TestEnsure_Idempotent(t *testing.T) {
	dir := t.TempDir()

	kp1, err := sshkey.Ensure(dir)
	if err != nil {
		t.Fatalf("first Ensure: %v", err)
	}

	kp2, err := sshkey.Ensure(dir)
	if err != nil {
		t.Fatalf("second Ensure: %v", err)
	}

	// Second call returns the same key.
	if kp1.PublicKeyLine != kp2.PublicKeyLine {
		t.Errorf("public key changed between calls:\n  first:  %s\n  second: %s",
			kp1.PublicKeyLine, kp2.PublicKeyLine)
	}
	if kp1.PrivateKeyPath != kp2.PrivateKeyPath {
		t.Errorf("private key path changed: %q vs %q", kp1.PrivateKeyPath, kp2.PrivateKeyPath)
	}
}

func TestEnsure_CreatesDir(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "nested", "ssh")

	_, err := sshkey.Ensure(dir)
	if err != nil {
		t.Fatalf("Ensure with non-existent dir: %v", err)
	}

	if _, err := os.Stat(dir); err != nil {
		t.Errorf("directory not created: %v", err)
	}
}

func TestGITSSHCommand(t *testing.T) {
	dir := t.TempDir()

	kp, err := sshkey.Ensure(dir)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	cmd := kp.GITSSHCommand()
	if !strings.Contains(cmd, kp.PrivateKeyPath) {
		t.Errorf("GITSSHCommand %q does not reference private key path %q", cmd, kp.PrivateKeyPath)
	}
	if !strings.Contains(cmd, "IdentitiesOnly=yes") {
		t.Errorf("GITSSHCommand %q missing IdentitiesOnly=yes", cmd)
	}
	if !strings.Contains(cmd, "BatchMode=yes") {
		t.Errorf("GITSSHCommand %q missing BatchMode=yes", cmd)
	}
}

func TestDefaultDir(t *testing.T) {
	dir, err := sshkey.DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	if !strings.Contains(dir, ".foreman") {
		t.Errorf("DefaultDir %q does not contain .foreman", dir)
	}
	if !strings.HasSuffix(dir, "ssh") {
		t.Errorf("DefaultDir %q does not end with ssh", dir)
	}
}
