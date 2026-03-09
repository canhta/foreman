package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show daemon status and active pipelines",
		RunE: func(cmd *cobra.Command, args []string) error {
			pid, err := readPIDFile()
			if err != nil {
				fmt.Fprintln(cmd.OutOrStdout(), "Foreman status: not running")
				return nil
			}

			if !processAlive(pid) {
				// Stale PID file — process is gone.
				_ = os.Remove(pidFilePath())
				fmt.Fprintln(cmd.OutOrStdout(), "Foreman status: not running (stale PID file removed)")
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Foreman status: running (pid %d)\n", pid)
			return nil
		},
	}
}

// readPIDFile reads the daemon PID from the PID file.
func readPIDFile() (int, error) {
	data, err := os.ReadFile(pidFilePath())
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("invalid PID file: %w", err)
	}
	return pid, nil
}

// processAlive returns true if a process with the given PID exists.
func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, FindProcess always succeeds; send signal 0 to check existence.
	return proc.Signal(syscall.Signal(0)) == nil
}

func init() {
	rootCmd.AddCommand(newStatusCmd())
}
