package auth

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/EugenMayer/ghcli/internal/credentials"
	"github.com/EugenMayer/ghcli/pkg/keychain/keychaincmd"
)

func newLoginCmd() *cobra.Command {
	var withToken bool

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Store a GitHub token in the OS keyring",
		Example: `  ghcli auth login
  pass show github/ghcli | ghcli auth login --with-token`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			token, err := keychaincmd.ReadSecret(cmd.InOrStdin(), cmd.ErrOrStderr(), "GitHub token: ", withToken)
			if err != nil {
				return err
			}
			if err := credentials.Store(token); err != nil {
				return err
			}
			fmt.Println("✔ token stored in the OS keyring")
			return nil
		},
	}

	cmd.Flags().BoolVar(&withToken, "with-token", false, "read the token from standard input")

	return cmd
}
