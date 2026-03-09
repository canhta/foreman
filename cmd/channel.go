package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/canhta/foreman/internal/channel/whatsapp"
	"github.com/spf13/cobra"
)

func newChannelCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "channel",
		Short: "Manage messaging channels",
	}
	cmd.AddCommand(newChannelLoginCmd())
	cmd.AddCommand(newChannelStatusCmd())
	return cmd
}

func newChannelLoginCmd() *cobra.Command {
	var phone string
	var mode string

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Link a WhatsApp account to Foreman",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, _, err := loadConfigAndDB()
			if err != nil {
				return err
			}

			sessionDB := cfg.Channel.WhatsApp.SessionDB
			if sessionDB == "" {
				sessionDB = "~/.foreman/whatsapp.db"
			}
			sessionDB = expandHomePath(sessionDB)

			// Fall back to config pairing_mode when --mode is not explicitly set.
			if !cmd.Flags().Changed("mode") && cfg.Channel.WhatsApp.PairingMode != "" {
				mode = cfg.Channel.WhatsApp.PairingMode
			}

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			switch mode {
			case "qr":
				return whatsapp.LoginWithQR(ctx, sessionDB)
			default: // "code" (default)
				if phone == "" {
					return fmt.Errorf("--phone is required for pairing code mode")
				}
				return whatsapp.LoginWithPairingCode(ctx, sessionDB, phone)
			}
		},
	}

	cmd.Flags().StringVar(&phone, "phone", "", "Phone number in E.164 format (e.g., +84123456789)")
	cmd.Flags().StringVar(&mode, "mode", "code", "Login mode: code or qr")
	return cmd
}

func newChannelStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show channel connection status",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, _, err := loadConfigAndDB()
			if err != nil {
				return err
			}

			if cfg.Channel.Provider == "" {
				fmt.Println("No channel configured.")
				return nil
			}

			sessionDB := cfg.Channel.WhatsApp.SessionDB
			if sessionDB == "" {
				sessionDB = "~/.foreman/whatsapp.db"
			}
			sessionDB = expandHomePath(sessionDB)

			if _, err := os.Stat(sessionDB); os.IsNotExist(err) {
				fmt.Println("whatsapp    not linked    Run: foreman channel login")
				return nil
			}

			fmt.Printf("whatsapp    session exists    %s\n", sessionDB)
			return nil
		},
	}
}

func init() {
	rootCmd.AddCommand(newChannelCmd())
}
