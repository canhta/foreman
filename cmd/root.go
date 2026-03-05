package cmd

import (
	"github.com/spf13/cobra"
)

var appVersion string

var rootCmd = &cobra.Command{
	Use:   "foreman",
	Short: "Autonomous software development daemon",
	Long:  "An autonomous coding daemon that turns issue tracker tickets into tested, reviewed pull requests.",
}

func SetVersion(v string) {
	appVersion = v
	rootCmd.Version = v
}

func Execute() error {
	return rootCmd.Execute()
}
