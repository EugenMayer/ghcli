package auth

import (
	"github.com/spf13/cobra"

	"github.com/EugenMayer/ghcli/internal/credentials"
)

// NewCmd returns the 'auth' namespace command.
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage the GitHub tokens",
		Long: `Manage the GitHub tokens used by ghcli.

A fine-grained token belongs to one organization (or user), so ghcli keeps one token per alias.
Name the alias after the organization, e.g. 'ghcli auth login --alias geek-labs': the token is
then picked automatically for that organization's repositories. The alias "default" is used for
everything else.

The token for a repository is looked up in this order:
  1. the GHCLI_PAT environment variable, used for every repository
  2. the output of os.keyring_command in ~/.ghcli/config.yml ({alias} is replaced by the alias)
  3. the OS keyring (GNOME Keyring / KWallet, macOS Keychain, Windows Credential Manager)
  4. github.<alias>.pat in ~/.ghcli/config.yml`,
	}

	cmd.AddCommand(newLoginCmd(), newLogoutCmd(), newStatusCmd())

	return cmd
}

// aliasFlag returns --alias, or the default alias.
func aliasFlag(cmd *cobra.Command) (string, error) {
	alias, _ := cmd.Flags().GetString("alias")
	if alias == "" {
		alias = credentials.DefaultAlias
	}
	return alias, credentials.ValidateAlias(alias)
}
