package pr

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/go-github/v92/github"
	"github.com/spf13/cobra"

	"github.com/EugenMayer/ghcli/internal/credentials"
	gh "github.com/EugenMayer/ghcli/internal/github"
)

// setup resolves the target repository and an authenticated client from the global flags.
func setup(cmd *cobra.Command) (*github.Client, gh.Repo, error) {
	repoFlag, _ := cmd.Flags().GetString("repo")
	repo, err := gh.ResolveRepo(repoFlag)
	if err != nil {
		return nil, gh.Repo{}, err
	}

	cfgPath, _ := cmd.Flags().GetString("config")
	token, _, err := credentials.Resolve(cmd.Context(), cfgPath, cmd.ErrOrStderr())
	if err != nil {
		return nil, gh.Repo{}, err
	}

	client, err := gh.NewClient(token)
	if err != nil {
		return nil, gh.Repo{}, err
	}
	return client, repo, nil
}

func listOpenPRs(ctx context.Context, client *github.Client, repo gh.Repo) ([]*github.PullRequest, error) {
	listOpts := &github.PullRequestListOptions{
		State:       "open",
		Sort:        "created",
		Direction:   "asc",
		ListOptions: github.ListOptions{PerPage: 100},
	}

	var all []*github.PullRequest
	for {
		prs, resp, err := client.PullRequests.List(ctx, repo.Owner, repo.Name, listOpts)
		if err != nil {
			if resp != nil && resp.StatusCode == http.StatusNotFound {
				return nil, fmt.Errorf("repository %s not found or not accessible with the configured PAT "+
					"(fine-grained tokens must be owned by %q and grant access to this repository): %w", repo, repo.Owner, err)
			}
			return nil, fmt.Errorf("listing pull requests of %s: %w", repo, err)
		}
		all = append(all, prs...)
		if resp.NextPage == 0 {
			return all, nil
		}
		listOpts.Page = resp.NextPage
	}
}
