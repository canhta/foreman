package cmd

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/canhta/foreman/internal/sshkey"
	"github.com/spf13/cobra"
)

var setupSSHCmd = &cobra.Command{
	Use:   "setup-ssh",
	Short: "Generate a dedicated SSH key for Foreman and print setup instructions",
	Long: `Generates a dedicated ed25519 SSH key at ~/.foreman/ssh/id_ed25519 (if one
does not already exist) and prints the public key to add as a GitHub Deploy Key.

This key is used exclusively by Foreman for git clone/push operations via
GIT_SSH_COMMAND — your ~/.ssh/config and ssh-agent are not modified.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		w := cmd.OutOrStdout()

		dir, err := sshkey.DefaultDir()
		if err != nil {
			return err
		}

		kp, err := sshkey.Ensure(dir)
		if err != nil {
			return fmt.Errorf("generate SSH key: %w", err)
		}

		fmt.Fprintln(w, "SSH key ready:")
		fmt.Fprintf(w, "  Private key : %s\n", kp.PrivateKeyPath)
		fmt.Fprintf(w, "  Public key  : %s\n\n", kp.PublicKeyPath)
		fmt.Fprintln(w, "Public key (paste this into GitHub → repo → Settings → Deploy Keys):")
		fmt.Fprintln(w)
		fmt.Fprintln(w, kp.PublicKeyLine)
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Steps:")
		fmt.Fprintln(w, "  1. Copy the public key above")
		fmt.Fprintln(w, "  2. Open: https://github.com/<org>/<repo>/settings/keys/new")
		fmt.Fprintln(w, "  3. Title: Foreman, paste the key, allow write access if needed")
		fmt.Fprintln(w, "  4. Run: foreman doctor   (to verify SSH connectivity)")

		// Optionally copy to clipboard on macOS / Linux.
		if copied := tryClipboard(kp.PublicKeyLine); copied {
			fmt.Fprintln(w, "\n(Public key copied to clipboard)")
		}

		return nil
	},
}

// tryClipboard attempts to copy text to the system clipboard.
// Returns true on success; silently fails otherwise.
func tryClipboard(text string) bool {
	for _, tool := range []string{"pbcopy", "xclip", "xsel"} {
		if path, err := exec.LookPath(tool); err == nil {
			var args []string
			switch tool {
			case "xclip":
				args = []string{"-selection", "clipboard"}
			case "xsel":
				args = []string{"--clipboard", "--input"}
			}
			c := exec.CommandContext(context.Background(), path, args...)
			c.Stdin = strings.NewReader(text)
			if c.Run() == nil {
				return true
			}
		}
	}
	return false
}

func init() {
	rootCmd.AddCommand(setupSSHCmd)
}
