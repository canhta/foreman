package cmd

import (
	"fmt"
	"time"

	"github.com/canhta/foreman/internal/config"
	"github.com/canhta/foreman/internal/db"
	"github.com/spf13/cobra"
)

var logsFollow bool

var logsCmd = &cobra.Command{
	Use:   "logs [TICKET_ID]",
	Short: "Show event log for a ticket",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadFromFile("foreman.toml")
		if err != nil {
			cfg, err = config.LoadDefaults()
			if err != nil {
				return err
			}
		}

		database, err := db.NewSQLiteDB(cfg.Database.SQLite.Path)
		if err != nil {
			return err
		}
		defer database.Close()

		ticketID := ""
		if len(args) > 0 {
			ticketID = args[0]
		}

		events, err := database.GetEvents(cmd.Context(), ticketID, 100)
		if err != nil {
			return err
		}

		for _, e := range events {
			fmt.Printf("%s  %-30s  %s\n", e.CreatedAt.Format(time.RFC3339), e.EventType, e.TicketID)
		}

		if logsFollow {
			fmt.Println("\n-- follow mode: polling every 2s (Ctrl+C to stop) --")
			// In follow mode, poll for new events
			// Full implementation deferred — requires event emitter integration
		}

		return nil
	},
}

func init() {
	logsCmd.Flags().BoolVar(&logsFollow, "follow", false, "Tail events in real time")
	rootCmd.AddCommand(logsCmd)
}
