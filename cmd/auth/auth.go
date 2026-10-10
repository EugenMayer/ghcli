package auth

import "github.com/spf13/cobra"

// NewCmd returns the 'auth' namespace command.
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage the GitHub token",
		Long: `Manage the GitHub token used by ghcli.

The token is looked up in this order:
  1. the GHCLI_PAT environment variable
  2. the output of os.keyring_command in ~/.ghcli/config.yml
  3. the OS keyring (Secret Service / GNOME Keyring / KWallet, macOS Keychain, Windows Credential Manager)
  4. github.pat in ~/.ghcli/config.yml`,
	}

	cmd.AddCommand(newLoginCmd(), newLogoutCmd(), newStatusCmd())

	return cmd
}
