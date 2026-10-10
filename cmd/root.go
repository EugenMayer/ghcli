package cmd

import (
	"github.com/spf13/cobra"

	"github.com/EugenMayer/ghcli/cmd/auth"
	"github.com/EugenMayer/ghcli/cmd/pr"
)

// Set via -ldflags at build time.
var (
	version = "dev"
	commit  = "none"
)

var rootCmd = &cobra.Command{
	Use:           "ghcli",
	Short:         "GitHub automation CLI",
	Version:       version + " (" + commit + ")",
	SilenceUsage:  true,
	SilenceErrors: false,
}

func init() {
	rootCmd.PersistentFlags().String("config", "", "path to config file (default ~/.ghcli/config.yml)")
	rootCmd.PersistentFlags().StringP("repo", "R", "", "repository as owner/name (default: detected from git remote 'origin' in the current directory)")

	rootCmd.AddCommand(auth.NewCmd(), pr.NewCmd())
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}
