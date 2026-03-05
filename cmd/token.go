package cmd

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/canhta/foreman/internal/config"
	"github.com/canhta/foreman/internal/db"
	"github.com/spf13/cobra"
)

var tokenCmd = &cobra.Command{
	Use:   "token",
	Short: "Manage dashboard auth tokens",
}

var tokenName string

var tokenGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate a new dashboard auth token",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadDefaults()
		if err != nil {
			return err
		}

		database, err := db.NewSQLiteDB(cfg.Database.SQLite.Path)
		if err != nil {
			return err
		}
		defer database.Close()

		// Generate random token
		tokenBytes := make([]byte, 32)
		if _, err := rand.Read(tokenBytes); err != nil {
			return fmt.Errorf("failed to generate token: %w", err)
		}
		token := hex.EncodeToString(tokenBytes)

		// Store hash
		hash := sha256.Sum256([]byte(token))
		hashStr := hex.EncodeToString(hash[:])

		if err := database.CreateAuthToken(cmd.Context(), hashStr, tokenName); err != nil {
			return fmt.Errorf("failed to store token: %w", err)
		}

		fmt.Printf("Token generated (save this — it won't be shown again):\n\n  %s\n\n", token)
		fmt.Printf("Name: %s\n", tokenName)
		return nil
	},
}

func init() {
	tokenGenerateCmd.Flags().StringVar(&tokenName, "name", "default", "Token name for identification")
	tokenCmd.AddCommand(tokenGenerateCmd)
	rootCmd.AddCommand(tokenCmd)
}
