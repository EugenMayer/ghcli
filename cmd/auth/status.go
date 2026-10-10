package auth

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/EugenMayer/ghcli/internal/credentials"
	gh "github.com/EugenMayer/ghcli/internal/github"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show which token is used, the account it belongs to and its API rate limit",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfgPath, _ := cmd.Flags().GetString("config")
			token, source, err := credentials.Resolve(cmd.Context(), cfgPath, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			fmt.Printf("token:   from %s (%s)\n", source, mask(token))

			if backend, err := credentials.KeyringBackend(cmd.Context()); err != nil {
				fmt.Printf("keyring: unavailable: %v\n", err)
			} else {
				fmt.Printf("keyring: %s\n", backend)
			}

			// Plain-text tokens get the recommendation from Resolve already.
			if source != credentials.SourceConfig {
				if tip := credentials.Recommendation(cmd.Context(), source); tip != "" {
					fmt.Fprintln(cmd.ErrOrStderr(), "ℹ "+tip)
				}
			}

			client, err := gh.NewClient(token)
			if err != nil {
				return err
			}

			// /rate_limit does not count against the rate limit, so it works even when exhausted.
			limits, _, err := client.RateLimit.Get(cmd.Context())
			if err != nil {
				return fmt.Errorf("checking token: %w", err)
			}
			core := limits.GetCore()
			fmt.Printf("limit:   %d of %d API requests left, resets %s\n",
				core.Remaining, core.Limit, core.Reset.Local().Format(time.TimeOnly))

			user, _, err := client.Users.Get(cmd.Context(), "")
			if err != nil {
				return fmt.Errorf("fetching account: %w", err)
			}
			fmt.Printf("user:    %s (ID %d)\n", user.GetLogin(), user.GetID())
			return nil
		},
	}
}

func mask(token string) string {
	if len(token) <= 8 {
		return "****"
	}
	return token[:4] + "…" + token[len(token)-4:]
}
