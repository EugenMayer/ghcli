package cmd

import (
	"runtime/debug"

	"github.com/spf13/cobra"

	"github.com/EugenMayer/ghcli/cmd/auth"
	"github.com/EugenMayer/ghcli/cmd/pr"
)

// Set via -ldflags at build time (Makefile, releases).
var (
	version = "dev"
	commit  = "none"
)

// versionString returns the version and commit. Builds without ldflags, such as
// `go install github.com/EugenMayer/ghcli@v1.2.3`, fall back to the build info Go embeds.
func versionString() string {
	v, c := version, commit
	if info, ok := debug.ReadBuildInfo(); ok {
		if v == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
			v = info.Main.Version
		}
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" && c == "none" {
				c = s.Value[:min(7, len(s.Value))]
			}
		}
	}
	return v + " (" + c + ")"
}

var rootCmd = &cobra.Command{
	Use:           "ghcli",
	Short:         "GitHub automation CLI",
	Version:       versionString(),
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
