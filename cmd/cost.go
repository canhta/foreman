package cmd

import (
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/canhta/foreman/internal/models"
	"github.com/spf13/cobra"
)

func newCostCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cost [today|week|month|per-ticket]",
		Short: "Show cost breakdown",
		Long:  "Show cost breakdown with budget comparison: today, week, month, or per-ticket.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, database, err := loadConfigAndDB()
			if err != nil {
				return err
			}
			defer database.Close()

			ctx := cmd.Context()
			period := args[0]
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)

			switch period {
			case "today":
				cost, _ := database.GetDailyCost(ctx, time.Now().Format("2006-01-02"))
				limit := cfg.Cost.MaxCostPerDayUSD
				pct := 0.0
				if limit > 0 {
					pct = (cost / limit) * 100
				}
				fmt.Fprintln(w, "Period\tSpent\tLimit\tUsage")
				fmt.Fprintln(w, "------\t-----\t-----\t-----")
				fmt.Fprintf(w, "Today\t$%.2f\t$%.2f\t%.1f%%\n", cost, limit, pct)

			case "month":
				cost, _ := database.GetMonthlyCost(ctx, time.Now().Format("2006-01"))
				limit := cfg.Cost.MaxCostPerMonthUSD
				pct := 0.0
				if limit > 0 {
					pct = (cost / limit) * 100
				}
				fmt.Fprintln(w, "Period\tSpent\tLimit\tUsage")
				fmt.Fprintln(w, "------\t-----\t-----\t-----")
				fmt.Fprintf(w, "This month\t$%.2f\t$%.2f\t%.1f%%\n", cost, limit, pct)

			case "week":
				fmt.Fprintln(w, "Day\tSpent")
				fmt.Fprintln(w, "---\t-----")
				now := time.Now()
				total := 0.0
				for i := 6; i >= 0; i-- {
					day := now.AddDate(0, 0, -i)
					cost, _ := database.GetDailyCost(ctx, day.Format("2006-01-02"))
					total += cost
					fmt.Fprintf(w, "%s\t$%.2f\n", day.Format("Mon 01/02"), cost)
				}
				fmt.Fprintf(w, "Total\t$%.2f\n", total)

			case "per-ticket":
				fmt.Fprintln(w, "Ticket\tExternal\tStatus\tCost")
				fmt.Fprintln(w, "------\t--------\t------\t----")
				tickets, _ := database.ListTickets(ctx, models.TicketFilter{})
				for _, t := range tickets {
					cost, _ := database.GetTicketCost(ctx, t.ID)
					if cost > 0 {
						short := t.ID
						if len(short) > 8 {
							short = short[:8]
						}
						fmt.Fprintf(w, "%s\t%s\t%s\t$%.2f\n", short, t.ExternalID, t.Status, cost)
					}
				}

			default:
				return fmt.Errorf("unknown period %q — use: today, week, month, per-ticket", period)
			}

			return w.Flush()
		},
	}
	return cmd
}

func init() {
	rootCmd.AddCommand(newCostCmd())
}
