// Package sshkey manages a dedicated SSH keypair for Foreman's git operations.
// Keys live in ~/.foreman/ssh/ — isolated from the user's ~/.ssh — and are
// injected via GIT_SSH_COMMAND so no global SSH config is touched.
package sshkey

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

const (
	privateKeyFile = "id_ed25519"
	publicKeyFile  = "id_ed25519.pub"
)

// KeyPair holds the paths and public key text for a Foreman SSH identity.
type KeyPair struct {
	PrivateKeyPath string
	PublicKeyPath  string
	// PublicKeyLine is the authorized_keys-format line to paste into GitHub Deploy Keys.
	PublicKeyLine string
}

// DefaultDir returns the standard Foreman SSH directory: ~/.foreman/ssh.
func DefaultDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".foreman", "ssh"), nil
}

// Ensure returns the KeyPair for dir, generating a new ed25519 keypair if one
// does not already exist. The directory is created with 0700 if absent.
// Idempotent: safe to call on every startup.
func Ensure(dir string) (*KeyPair, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", dir, err)
	}

	privPath := filepath.Join(dir, privateKeyFile)
	pubPath := filepath.Join(dir, publicKeyFile)

	// Already exists — read and return.
	if _, err := os.Stat(privPath); err == nil {
		pub, err := os.ReadFile(pubPath)
		if err != nil {
			return nil, fmt.Errorf("read public key: %w", err)
		}
		return &KeyPair{
			PrivateKeyPath: privPath,
			PublicKeyPath:  pubPath,
			PublicKeyLine:  strings.TrimSpace(string(pub)),
		}, nil
	}

	// Generate a new ed25519 keypair.
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate keypair: %w", err)
	}

	// Marshal private key → OpenSSH PEM, write with mode 0600.
	privPEMBlock, err := ssh.MarshalPrivateKey(priv, "foreman")
	if err != nil {
		return nil, fmt.Errorf("marshal private key: %w", err)
	}
	if err = os.WriteFile(privPath, pem.EncodeToMemory(privPEMBlock), 0o600); err != nil {
		return nil, fmt.Errorf("write private key: %w", err)
	}

	// Marshal public key → authorized_keys format, write with mode 0644.
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		_ = os.Remove(privPath)
		return nil, fmt.Errorf("marshal public key: %w", err)
	}
	pubLine := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub))) + " foreman"
	if err = os.WriteFile(pubPath, []byte(pubLine+"\n"), 0o644); err != nil {
		_ = os.Remove(privPath)
		return nil, fmt.Errorf("write public key: %w", err)
	}

	return &KeyPair{
		PrivateKeyPath: privPath,
		PublicKeyPath:  pubPath,
		PublicKeyLine:  pubLine,
	}, nil
}

// GITSSHCommand returns the value for the GIT_SSH_COMMAND environment variable
// that instructs git to use the Foreman private key exclusively, with no
// interference from the user's ssh-agent or ~/.ssh/config.
func (kp *KeyPair) GITSSHCommand() string {
	return fmt.Sprintf(
		"ssh -i %s -o StrictHostKeyChecking=accept-new -o BatchMode=yes -o IdentitiesOnly=yes",
		kp.PrivateKeyPath,
	)
}
