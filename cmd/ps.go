package cmd

import (
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/canhta/foreman/internal/models"
	"github.com/spf13/cobra"
)

func newPsCmd() *cobra.Command {
	var showAll bool

	cmd := &cobra.Command{
		Use:   "ps",
		Short: "List active pipelines",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, database, err := loadConfigAndDB()
			if err != nil {
				return err
			}
			defer database.Close()

			ctx := cmd.Context()

			filter := models.TicketFilter{}
			if !showAll {
				filter.StatusIn = []models.TicketStatus{
					models.TicketStatusQueued,
					models.TicketStatusPlanning,
					models.TicketStatusPlanValidating,
					models.TicketStatusImplementing,
					models.TicketStatusReviewing,
					models.TicketStatusAwaitingMerge,
					models.TicketStatusClarificationNeeded,
					models.TicketStatusDecomposing,
				}
			}

			tickets, err := database.ListTickets(ctx, filter)
			if err != nil {
				return fmt.Errorf("listing tickets: %w", err)
			}

			if len(tickets) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No pipelines found.")
				return nil
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tExternal\tStatus\tDuration\tTasks")
			fmt.Fprintln(w, "--\t--------\t------\t--------\t-----")

			for _, t := range tickets {
				duration := "-"
				if t.StartedAt != nil {
					d := time.Since(*t.StartedAt)
					if t.CompletedAt != nil {
						d = t.CompletedAt.Sub(*t.StartedAt)
					}
					duration = formatDuration(d)
				}

				tasks, _ := database.ListTasks(ctx, t.ID)
				doneCount := 0
				for _, task := range tasks {
					if task.Status == models.TaskStatusDone {
						doneCount++
					}
				}
				taskStr := fmt.Sprintf("%d/%d", doneCount, len(tasks))

				short := t.ID
				if len(short) > 8 {
					short = short[:8]
				}

				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
					short, t.ExternalID, t.Status, duration, taskStr)
			}
			return w.Flush()
		},
	}

	cmd.Flags().BoolVar(&showAll, "all", false, "Show all pipelines including completed")
	return cmd
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}

func init() {
	rootCmd.AddCommand(newPsCmd())
}
