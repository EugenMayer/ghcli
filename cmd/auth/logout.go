package auth

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/EugenMayer/ghcli/internal/credentials"
)

func newLogoutCmd() *cobra.Command {
	var all bool

	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Remove a GitHub token from the OS keyring",
		Long:  `Remove the token of --alias ("default" if omitted) from the OS keyring, or all tokens with --all.`,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if all {
				deleted, err := credentials.DeleteAll()
				if err != nil {
					return err
				}
				if len(deleted) == 0 {
					fmt.Println("no tokens stored in the OS keyring")
				} else {
					fmt.Printf("✔ tokens removed from the OS keyring: %s\n", strings.Join(deleted, ", "))
				}
				return nil
			}

			alias, err := aliasFlag(cmd)
			if err != nil {
				return err
			}
			deleted, err := credentials.Delete(alias)
			if err != nil {
				return err
			}
			if deleted {
				fmt.Printf("✔ token for alias %q removed from the OS keyring\n", alias)
			} else {
				fmt.Printf("no token for alias %q stored in the OS keyring\n", alias)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "remove the tokens of all aliases")

	return cmd
}
