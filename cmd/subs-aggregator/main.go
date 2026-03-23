package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var version = "dev"

const appName = "subs-aggregator"

var configPath string

var rootCmd = &cobra.Command{
	Use:   appName,
	Short: "Subscription aggregator for 3X-UI panels",
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run server in foreground",
	Run: func(cmd *cobra.Command, args []string) {
		cmdRun(configPath)
	},
}

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start server in background",
	Run: func(cmd *cobra.Command, args []string) {
		cmdStart(configPath)
	},
}

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop running server",
	Run: func(cmd *cobra.Command, args []string) {
		cmdStop()
	},
}

var restartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart server",
	Run: func(cmd *cobra.Command, args []string) {
		cmdRestart(configPath)
	},
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check server status",
	Run: func(cmd *cobra.Command, args []string) {
		cmdStatus()
	},
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Show config file path",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(defaultConfigPath())
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("%s %s\n", appName, version)
	},
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Check for updates and install the latest version",
	Run: func(cmd *cobra.Command, args []string) {
		cmdUpdate()
	},
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", defaultConfigPath(), "path to config file")

	rootCmd.AddCommand(runCmd, startCmd, stopCmd, restartCmd, statusCmd, configCmd, versionCmd, updateCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
