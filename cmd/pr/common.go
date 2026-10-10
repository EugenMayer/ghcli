package pr

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/go-github/v92/github"
	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"

	"github.com/EugenMayer/ghcli/internal/credentials"
	gh "github.com/EugenMayer/ghcli/internal/github"
)

// session is the repository a command works on, with a client for the token chosen for it.
type session struct {
	client *github.Client
	repo   gh.Repo
	token  credentials.Token
}

// setup resolves the target repository and the token for it from the global flags: --alias, or
// the token whose alias matches the repository owner, or the only/default one, or asks.
func setup(cmd *cobra.Command) (*session, error) {
	repoFlag, _ := cmd.Flags().GetString("repo")
	repo, err := gh.ResolveRepo(repoFlag)
	if err != nil {
		return nil, err
	}

	cfgPath, _ := cmd.Flags().GetString("config")
	alias, _ := cmd.Flags().GetString("alias")
	token, err := credentials.Resolve(cmd.Context(), credentials.Request{
		ConfigPath: cfgPath,
		Owner:      repo.Owner,
		Alias:      alias,
		Ask:        func(aliases []string) (string, error) { return askAlias(repo, aliases) },
	}, cmd.ErrOrStderr())
	if err != nil {
		return nil, err
	}

	client, err := gh.NewClient(token.Value)
	if err != nil {
		return nil, err
	}
	return &session{client: client, repo: repo, token: token}, nil
}

// askAlias lets the user pick a token when none matches the repository owner.
func askAlias(repo gh.Repo, aliases []string) (string, error) {
	prompt := promptui.Select{
		Label: fmt.Sprintf("No token for %s, which one to use? (tip: '%s')", repo.Owner, credentials.LoginCommand(repo.Owner)),
		Items: aliases,
	}
	_, alias, err := prompt.Run()
	return alias, err
}

func (s *session) listOpenPRs(ctx context.Context) ([]*github.PullRequest, error) {
	listOpts := &github.PullRequestListOptions{
		State:       "open",
		Sort:        "created",
		Direction:   "asc",
		ListOptions: github.ListOptions{PerPage: 100},
	}

	var all []*github.PullRequest
	for {
		prs, resp, err := s.client.PullRequests.List(ctx, s.repo.Owner, s.repo.Name, listOpts)
		if err != nil {
			if resp != nil && resp.StatusCode == http.StatusNotFound {
				return nil, fmt.Errorf("repository %s not found or not accessible with the token from %s. "+
					"Fine-grained tokens must be owned by %q and grant access to this repository; "+
					"store one with '%s': %w", s.repo, s.token.From(), s.repo.Owner, credentials.LoginCommand(s.repo.Owner), err)
			}
			return nil, fmt.Errorf("listing pull requests of %s: %w", s.repo, err)
		}
		all = append(all, prs...)
		if resp.NextPage == 0 {
			return all, nil
		}
		listOpts.Page = resp.NextPage
	}
}
