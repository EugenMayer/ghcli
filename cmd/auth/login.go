package auth

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/EugenMayer/ghcli/internal/browser"
	"github.com/EugenMayer/ghcli/internal/credentials"
	"github.com/EugenMayer/ghcli/pkg/keychain/keychaincmd"
)

func newLoginCmd() *cobra.Command {
	var (
		withToken bool
		expires   string
		workflows bool
		web       bool
	)

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Store a GitHub token in the OS keyring",
		Long: `Store a GitHub token in the OS keyring, under the alias given with --alias ("default" if
omitted). Use the organization name as alias, so the token is picked for its repositories.

Before asking for the token, ghcli prints a link to GitHub's form for a new fine-grained token,
pre-filled with the organization, expiry and the permissions ghcli needs. GitHub lets you confirm
and adjust everything there; only the repositories have to be selected by hand.`,
		Example: `  ghcli auth login                      # alias "default"
  ghcli auth login --alias geek-labs    # token of the geek-labs organization
  ghcli auth login --alias geek-labs --web --expires 30
  pass show github/geek-labs | ghcli auth login --alias geek-labs --with-token`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			alias, err := aliasFlag(cmd)
			if err != nil {
				return err
			}
			if err := credentials.ValidateExpiry(expires); err != nil {
				return err
			}

			errOut := cmd.ErrOrStderr()
			if !withToken {
				link := credentials.TokenURL(alias, expires, workflows)
				fmt.Fprintf(errOut, "Click here to create a PAT with the right permissions - it will let you confirm and adjust:\n  %s\n", link)
				if alias == credentials.DefaultAlias {
					fmt.Fprintln(errOut, "  Select the resource owner (organization or user) in the form; for an organization, prefer --alias <org>.")
				}
				fmt.Fprintln(errOut, "  Select the repositories there, then paste the new token below.")
				if web {
					if err := browser.Open(link); err != nil {
						fmt.Fprintf(errOut, "  (could not open a browser: %v)\n", err)
					}
				}
				fmt.Fprintln(errOut)
			}

			prompt := fmt.Sprintf("GitHub token for %s: ", alias)
			token, err := keychaincmd.ReadSecret(cmd.InOrStdin(), errOut, prompt, withToken)
			if err != nil {
				return err
			}
			if err := credentials.Store(alias, token); err != nil {
				return err
			}
			fmt.Printf("✔ token for alias %q stored in the OS keyring\n", alias)

			cfgPath, _ := cmd.Flags().GetString("config")
			if notice := credentials.KeyringOverride(cfgPath); notice != "" {
				fmt.Fprintln(errOut, "⚠ "+notice)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&withToken, "with-token", false, "read the token from standard input")
	cmd.Flags().StringVar(&expires, "expires", credentials.DefaultExpiry, "token lifetime in days (1-366) or none, pre-filled in the link")
	cmd.Flags().BoolVar(&workflows, "workflows", false, "also request Workflows: write, needed to merge PRs that change .github/workflows/")
	cmd.Flags().BoolVar(&web, "web", false, "open the link in the browser")

	return cmd
}
