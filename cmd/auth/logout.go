package auth

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/EugenMayer/ghcli/internal/credentials"
)

func newLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove the GitHub token from the OS keyring",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			deleted, err := credentials.Delete()
			if err != nil {
				return err
			}
			if deleted {
				fmt.Println("✔ token removed from the OS keyring")
			} else {
				fmt.Println("no token stored in the OS keyring")
			}
			return nil
		},
	}
}
