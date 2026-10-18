package pr

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/go-github/v92/github"
	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"

	"github.com/EugenMayer/ghcli/internal/config"
	gh "github.com/EugenMayer/ghcli/internal/github"
)

const defaultDoneLabel = "done"

// Merge strategies of pr merge; defaultMergeStrategy applies unless the config or --strategy says otherwise.
const (
	strategySquash       = "squash"
	strategyMerge        = "merge"
	defaultMergeStrategy = strategySquash
)

var mergeStrategies = []string{strategySquash, strategyMerge}

type mergeOptions struct {
	allDone   bool
	doneLabel string
	strategy  string
	yes       bool

	waitTimeout  time.Duration
	pollInterval time.Duration
	updateBranch bool
}

func newMergeCmd() *cobra.Command {
	opts := &mergeOptions{}

	cmd := &cobra.Command{
		Use:   "merge",
		Short: "Merge open pull requests",
		Long: `Lists open pull requests and lets you select one to merge.

With --all-done, every open pull request carrying the 'done' label is merged,
one after another, regardless of its review status.

Before each merge ghcli waits until the pull request is mergeable: it waits while GitHub
recomputes mergeability or required checks are running (e.g. after the previous merge),
and updates the branch when it is behind its base. Pull requests with conflicts are skipped.
The merge strategy is squash unless pr.merge_strategy in the config or --strategy says otherwise.
If the repository does not allow merge commits, the merge strategy falls back to squash.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMerge(cmd, opts)
		},
	}

	cmd.Flags().BoolVar(&opts.allDone, "all-done", false, "merge all open PRs labeled with the done label, ignoring review status")
	cmd.Flags().StringVar(&opts.doneLabel, "done-label", defaultDoneLabel, "label used by --all-done")
	cmd.Flags().StringVarP(&opts.strategy, "strategy", "s", "",
		"merge strategy: squash or merge (default squash, or pr.merge_strategy from the config)")
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "do not ask for confirmation")
	cmd.Flags().DurationVar(&opts.waitTimeout, "wait-timeout", 15*time.Minute, "how long to wait for a pull request to become mergeable")
	cmd.Flags().DurationVar(&opts.pollInterval, "poll-interval", 4*time.Second, "how often to check whether a pull request is mergeable")
	cmd.Flags().BoolVar(&opts.updateBranch, "update-branch", true, "update a pull request branch that is behind its base before merging")

	return cmd
}

func runMerge(cmd *cobra.Command, opts *mergeOptions) error {
	strategy, err := resolveStrategy(cmd, opts.strategy)
	if err != nil {
		return err
	}
	opts.strategy = strategy

	ctx := cmd.Context()
	s, err := setup(cmd)
	if err != nil {
		return err
	}
	client, repo := s.client, s.repo

	prs, err := s.listOpenPRs(ctx)
	if err != nil {
		return err
	}

	if opts.allDone {
		return mergeAllDone(ctx, client, repo, prs, opts)
	}
	return mergeSelected(ctx, client, repo, prs, opts)
}

func mergeSelected(ctx context.Context, client *github.Client, repo gh.Repo, prs []*github.PullRequest, opts *mergeOptions) error {
	if len(prs) == 0 {
		fmt.Printf("No open pull requests in %s\n", repo)
		return nil
	}

	items := make([]string, 0, len(prs))
	for _, pr := range prs {
		items = append(items, describePR(pr))
	}

	prompt := promptui.Select{
		Label: fmt.Sprintf("Select a pull request of %s to merge", repo),
		Items: items,
		Size:  15,
		Searcher: func(input string, index int) bool {
			return strings.Contains(strings.ToLower(items[index]), strings.ToLower(input))
		},
	}
	idx, _, err := prompt.Run()
	if err != nil {
		return err
	}
	selected := prs[idx]

	if !opts.yes {
		ok, err := confirm(fmt.Sprintf("Merge #%d using %s?", selected.GetNumber(), opts.strategy))
		if err != nil || !ok {
			return err
		}
	}

	return mergePR(ctx, client, repo, selected, opts)
}

func mergeAllDone(ctx context.Context, client *github.Client, repo gh.Repo, prs []*github.PullRequest, opts *mergeOptions) error {
	var done []*github.PullRequest
	for _, pr := range prs {
		if hasLabel(pr, opts.doneLabel) {
			done = append(done, pr)
		}
	}

	if len(done) == 0 {
		fmt.Printf("No open pull requests labeled %q in %s\n", opts.doneLabel, repo)
		return nil
	}

	fmt.Printf("Open pull requests labeled %q in %s:\n", opts.doneLabel, repo)
	for _, pr := range done {
		fmt.Println("  " + describePR(pr))
	}

	if !opts.yes {
		ok, err := confirm(fmt.Sprintf("Merge these %d pull requests using %s?", len(done), opts.strategy))
		if err != nil || !ok {
			return err
		}
	}

	var failed []int
	for _, pr := range done {
		if err := mergePR(ctx, client, repo, pr, opts); err != nil {
			fmt.Println(err)
			failed = append(failed, pr.GetNumber())
		}
	}

	if len(failed) > 0 {
		return fmt.Errorf("%d of %d pull requests could not be merged: %v", len(failed), len(done), failed)
	}
	return nil
}

// mergePR waits until the PR is mergeable and merges it. After a previous merge GitHub recomputes
// mergeability and required checks may re-run, so the PR is polled until it can be merged, its
// branch is updated when it is behind the base, or opts.waitTimeout is reached.
func mergePR(ctx context.Context, client *github.Client, repo gh.Repo, pr *github.PullRequest, opts *mergeOptions) error {
	number := pr.GetNumber()
	fail := func(format string, args ...any) error {
		return fmt.Errorf("✘ #%d %s: %s", number, pr.GetTitle(), fmt.Sprintf(format, args...))
	}

	deadline := time.Now().Add(opts.waitTimeout)
	strategy := opts.strategy
	branchUpdated := false
	lastStatus := ""

	for {
		current, _, err := client.PullRequests.Get(ctx, repo.Owner, repo.Name, number)
		if err != nil {
			return fail("fetching pull request: %v", err)
		}

		state := current.GetMergeableState()
		status := ""
		switch {
		case current.GetMerged():
			fmt.Printf("✔ #%d %s was already merged\n", number, pr.GetTitle())
			return nil
		case current.GetState() != "open":
			return fail("pull request is %s", current.GetState())
		case current.GetDraft():
			return fail("pull request is a draft")
		case state == "dirty" || (current.Mergeable != nil && !current.GetMergeable()):
			return fail("has merge conflicts")
		case current.Mergeable == nil || state == "unknown":
			status = "GitHub is computing mergeability"
		case state == "behind" && !branchUpdated && opts.updateBranch:
			if _, _, err := client.PullRequests.UpdateBranch(ctx, repo.Owner, repo.Name, number, nil); err != nil {
				var accepted *github.AcceptedError
				if !errors.As(err, &accepted) {
					return fail("updating branch with %s: %v", current.GetBase().GetRef(), err)
				}
			}
			branchUpdated = true
			status = "branch is behind " + current.GetBase().GetRef() + ", updated it"
		default:
			// clean, unstable, has_hooks, blocked or behind: try it, review status is ignored on purpose.
			result, _, err := client.PullRequests.Merge(ctx, repo.Owner, repo.Name, number, "",
				&github.PullRequestOptions{MergeMethod: strategy})
			if err == nil {
				fmt.Printf("✔ merged #%d %s using %s (%s)\n", number, pr.GetTitle(), strategy, shortSHA(result.GetSHA()))
				return nil
			}
			var ghErr *github.ErrorResponse
			if !errors.As(err, &ghErr) || ghErr.Response == nil ||
				(ghErr.Response.StatusCode != http.StatusMethodNotAllowed && ghErr.Response.StatusCode != http.StatusConflict) {
				return fail("merging: %v", err)
			}
			if strategy == strategyMerge && mergeCommitsNotAllowed(ghErr) {
				// The repository only allows squash (or rebase): fall back to squash and retry right away.
				fmt.Printf("… #%d merge commits are not allowed in %s, using squash\n", number, repo)
				strategy = strategySquash
				continue
			}
			// 405/409: not mergeable yet, e.g. required checks still running or base branch just changed.
			status = fmt.Sprintf("not mergeable yet (%s): %s", state, ghErr.Message)
		}

		if time.Now().After(deadline) {
			return fail("still not mergeable after %s: %s", opts.waitTimeout, status)
		}
		if status != lastStatus {
			fmt.Printf("… #%d %s, waiting\n", number, status)
			lastStatus = status
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(opts.pollInterval):
		}
	}
}

// resolveStrategy returns the merge strategy: --strategy if given, else pr.merge_strategy from the
// config, else defaultMergeStrategy.
func resolveStrategy(cmd *cobra.Command, flagValue string) (string, error) {
	if flagValue != "" {
		if !slices.Contains(mergeStrategies, flagValue) {
			return "", fmt.Errorf("invalid --strategy %q, expected squash or merge", flagValue)
		}
		return flagValue, nil
	}

	cfgPath, _ := cmd.Flags().GetString("config")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return "", err
	}
	if cfg.PR.MergeStrategy == "" {
		return defaultMergeStrategy, nil
	}
	if !slices.Contains(mergeStrategies, cfg.PR.MergeStrategy) {
		return "", fmt.Errorf("invalid pr.merge_strategy %q in the config, expected squash or merge", cfg.PR.MergeStrategy)
	}
	return cfg.PR.MergeStrategy, nil
}

// mergeCommitsNotAllowed reports whether GitHub rejected the merge because the repository
// disallows merge commits ("Merge commits are not allowed on this repository.").
func mergeCommitsNotAllowed(err *github.ErrorResponse) bool {
	return err.Response.StatusCode == http.StatusMethodNotAllowed &&
		strings.Contains(strings.ToLower(err.Message), "merge commits are not allowed")
}

// confirm asks a y/N question; answering anything but y is not an error.
func confirm(label string) (bool, error) {
	prompt := promptui.Prompt{Label: label, IsConfirm: true}
	if _, err := prompt.Run(); err != nil {
		if errors.Is(err, promptui.ErrAbort) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func hasLabel(pr *github.PullRequest, name string) bool {
	return slices.ContainsFunc(pr.Labels, func(l *github.Label) bool {
		return strings.EqualFold(l.GetName(), name)
	})
}

func describePR(pr *github.PullRequest) string {
	s := fmt.Sprintf("#%d %s (%s → %s, @%s)", pr.GetNumber(), pr.GetTitle(),
		pr.GetHead().GetRef(), pr.GetBase().GetRef(), pr.GetUser().GetLogin())
	if pr.GetDraft() {
		s += " [draft]"
	}
	return s
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
