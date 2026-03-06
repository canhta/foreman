package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/canhta/foreman/internal/config"
	"github.com/spf13/cobra"
)

func newPairingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pairing",
		Short: "Manage sender pairing for messaging channels",
	}
	cmd.AddCommand(newPairingListCmd())
	cmd.AddCommand(newPairingApproveCmd())
	cmd.AddCommand(newPairingRevokeCmd())
	return cmd
}

func newPairingListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List pending pairing requests",
		RunE: func(_ *cobra.Command, _ []string) error {
			_, database, err := loadConfigAndDB()
			if err != nil {
				return err
			}
			defer database.Close()

			pairings, err := database.ListPairings(context.Background(), "whatsapp")
			if err != nil {
				return err
			}

			if len(pairings) == 0 {
				fmt.Println("No pending pairing requests.")
				return nil
			}

			fmt.Printf("%-12s %-20s %s\n", "CODE", "SENDER", "EXPIRES")
			for _, p := range pairings {
				remaining := time.Until(p.ExpiresAt).Round(time.Minute)
				fmt.Printf("%-12s %-20s in %s\n", p.Code, p.SenderID, remaining)
			}
			return nil
		},
	}
}

func newPairingApproveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "approve <CODE>",
		Short: "Approve a pending pairing request",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			_, database, err := loadConfigAndDB()
			if err != nil {
				return err
			}
			defer database.Close()

			code := args[0]
			p, err := database.GetPairing(context.Background(), code)
			if err != nil {
				return err
			}
			if p == nil {
				return fmt.Errorf("pairing code %q not found", code)
			}
			if time.Now().After(p.ExpiresAt) {
				_ = database.DeletePairing(context.Background(), code)
				return fmt.Errorf("pairing code %q has expired", code)
			}

			// Add to config file
			if err := config.AddAllowedNumber("foreman.toml", p.SenderID); err != nil {
				return fmt.Errorf("update config: %w", err)
			}

			// Delete pairing
			if err := database.DeletePairing(context.Background(), code); err != nil {
				return fmt.Errorf("delete pairing: %w", err)
			}

			fmt.Printf("Approved %s — added to allowed_numbers.\n", p.SenderID)
			return nil
		},
	}
}

func newPairingRevokeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke <PHONE>",
		Short: "Remove a phone number from the allowlist",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			phone := args[0]
			if err := config.RemoveAllowedNumber("foreman.toml", phone); err != nil {
				return fmt.Errorf("update config: %w", err)
			}
			fmt.Printf("Revoked %s — removed from allowed_numbers.\n", phone)
			return nil
		},
	}
}

func init() {
	rootCmd.AddCommand(newPairingCmd())
}
