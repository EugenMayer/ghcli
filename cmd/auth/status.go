package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/go-github/v92/github"
	"github.com/spf13/cobra"

	"github.com/EugenMayer/ghcli/internal/credentials"
	gh "github.com/EugenMayer/ghcli/internal/github"
	"github.com/EugenMayer/ghcli/pkg/keychain"
	"github.com/EugenMayer/ghcli/pkg/keychain/keychaincmd"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the stored tokens, their accounts and API rate limits, and which one this repository uses",
		Args:  cobra.NoArgs,
		RunE:  runStatus,
	}
}

func runStatus(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()
	cfgPath, _ := cmd.Flags().GetString("config")
	aliasArg, _ := cmd.Flags().GetString("alias")

	if backend, err := credentials.KeyringBackend(ctx); err != nil {
		fmt.Fprintf(out, "keyring: unavailable: %s\n", keychain.UnavailableReason(err))
	} else {
		fmt.Fprintf(out, "keyring: %s\n", backend)
	}

	if notice := credentials.KeyringOverride(cfgPath); notice != "" {
		fmt.Fprintln(errOut, "⚠ "+notice)
	}

	// The repository is optional here: outside a repository, only the tokens are listed.
	repoFlag, _ := cmd.Flags().GetString("repo")
	repo, repoErr := gh.ResolveRepo(repoFlag)

	req := credentials.Request{ConfigPath: cfgPath, Alias: aliasArg, NoAliasNotice: true}
	if repoErr == nil {
		req.Owner = repo.Owner
	}
	used, usedErr := credentials.Resolve(ctx, req, errOut)

	// GHCLI_PAT and an os.keyring_command without {alias} serve every repository.
	if usedErr == nil && used.Alias == "" {
		fmt.Fprintf(out, "token:   from %s (%s), used for every repository\n", used.From(), keychaincmd.Mask(used.Value))
		printAccount(ctx, out, "         ", used.Value)
		printTip(ctx, errOut, used)
		return nil
	}

	aliases, err := credentials.Aliases(cfgPath)
	if err != nil {
		return err
	}
	if len(aliases) == 0 && usedErr == nil {
		aliases = []string{used.Alias} // e.g. only reachable via os.keyring_command with {alias}
	}
	if len(aliases) == 0 {
		return usedErr
	}

	fmt.Fprintln(out, "tokens:")
	for _, alias := range aliases {
		marker := " "
		if usedErr == nil && alias == used.Alias {
			marker = "›"
		}
		t, err := credentials.ResolveAlias(ctx, cfgPath, alias)
		if err != nil {
			fmt.Fprintf(out, "%s %-12s %v\n", marker, alias, err)
			continue
		}
		fmt.Fprintf(out, "%s %-12s from %s (%s)\n", marker, alias, t.Where(), keychaincmd.Mask(t.Value))
		printAccount(ctx, out, strings.Repeat(" ", 15), t.Value)
	}

	switch {
	case repoErr != nil:
		fmt.Fprintln(out, "repository: none detected, run inside a repository or pass --repo to see which token it uses")
	case usedErr != nil:
		fmt.Fprintf(out, "repository %s: %v\n", repo, usedErr)
	default:
		reason := used.Reason
		if reason == "" {
			reason = "the default"
		}
		fmt.Fprintf(out, "repository %s: uses alias %q (%s)\n", repo, used.Alias, reason)
		if used.Source != credentials.SourceConfig { // plain text got its tip from Resolve already
			printTip(ctx, errOut, used)
		}
	}
	return nil
}

// printAccount prints the account a token belongs to and its API rate limit.
func printAccount(ctx context.Context, out io.Writer, indent, token string) {
	client, err := gh.NewClient(token)
	if err != nil {
		fmt.Fprintf(out, "%s%v\n", indent, err)
		return
	}
	// /rate_limit does not count against the rate limit, so it works even when exhausted.
	limits, _, err := client.RateLimit.Get(ctx)
	if err != nil {
		fmt.Fprintf(out, "%scheck failed: %v\n", indent, err)
		return
	}
	core := limits.GetCore()
	user, _, err := client.Users.Get(ctx, "")
	var ghErr *github.ErrorResponse
	switch {
	case errors.As(err, &ghErr):
		fmt.Fprintf(out, "%saccount unknown (%s), ", indent, ghErr.Message)
	case err != nil:
		fmt.Fprintf(out, "%saccount unknown (%v), ", indent, err)
	default:
		fmt.Fprintf(out, "%suser %s (ID %d), ", indent, user.GetLogin(), user.GetID())
	}
	fmt.Fprintf(out, "%d of %d API requests left, resets %s\n",
		core.Remaining, core.Limit, core.Reset.Local().Format(time.TimeOnly))
}

func printTip(ctx context.Context, w io.Writer, t credentials.Token) {
	if tip := credentials.Recommendation(ctx, t); tip != "" {
		fmt.Fprintln(w, "ℹ "+tip)
	}
}
