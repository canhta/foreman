package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/canhta/foreman/internal/config"
	"github.com/canhta/foreman/internal/dashboard"
	"github.com/canhta/foreman/internal/db"
	"github.com/canhta/foreman/internal/telemetry"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var dashboardPort int

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Start the web dashboard",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Try loading from default config path; fall back to defaults if not found.
		cfg, err := config.LoadDefaults()
		if err != nil {
			return err
		}

		database, err := db.NewSQLiteDB(cfg.Database.SQLite.Path)
		if err != nil {
			return err
		}
		defer database.Close()

		reg := prometheus.NewRegistry()
		_ = telemetry.NewMetrics(reg)

		emitter := telemetry.NewEventEmitter(database)

		port := dashboardPort
		if port == 0 {
			port = cfg.Dashboard.Port
		}
		if port == 0 {
			port = 8080
		}

		host := cfg.Dashboard.Host
		if host == "" {
			host = "127.0.0.1"
		}

		srv := dashboard.NewServer(database, emitter, nil, reg, cfg.Cost, "0.1.0", host, port)

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		go func() {
			<-ctx.Done()
			shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			srv.Shutdown(shutCtx)
		}()

		log.Info().Int("port", port).Msg("Starting dashboard")
		return srv.Start()
	},
}

func init() {
	dashboardCmd.Flags().IntVar(&dashboardPort, "port", 0, "Override dashboard port")
	rootCmd.AddCommand(dashboardCmd)
}
