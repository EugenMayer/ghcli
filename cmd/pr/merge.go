package pr

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/go-github/v92/github"
	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"

	gh "github.com/EugenMayer/ghcli/internal/github"
)

// Merge strategies of pr merge; defaultMergeStrategy applies unless the config or --strategy says otherwise.
const (
	strategySquash       = "squash"
	strategyMerge        = "merge"
	defaultMergeStrategy = strategySquash
)

var mergeStrategies = []string{strategySquash, strategyMerge}

type mergeOptions struct {
	allReady   bool
	readyLabel string
	strategy   string
	yes        bool

	waitTimeout     time.Duration
	pollInterval    time.Duration
	maxPollInterval time.Duration
	updateBranch    bool

	backoffOnce sync.Once
	backoff     *pollBackoff
}

// pollBackoff returns the poll interval shared by all pull requests of this run.
func (opts *mergeOptions) pollBackoff() *pollBackoff {
	opts.backoffOnce.Do(func() {
		opts.backoff = newPollBackoff(opts.pollInterval, max(opts.pollInterval, opts.maxPollInterval))
	})
	return opts.backoff
}

func newMergeCmd() *cobra.Command {
	opts := &mergeOptions{}

	cmd := &cobra.Command{
		Use:   "merge",
		Short: "Merge open pull requests",
		Long: `Lists open pull requests and lets you select one to merge.

With --all-ready, every open pull request carrying the ready-to-merge label ('done' unless
pr.ready_to_merge_label in the config or --ready-to-merge-label says otherwise) is merged,
one after another, regardless of its review status.

Before each merge ghcli waits until the pull request is mergeable: it waits while GitHub
recomputes mergeability or required checks are running (e.g. after the previous merge),
and updates the branch when it is behind its base. Pull requests with conflicts are skipped.
The merge strategy is squash unless pr.merge_strategy in the config or --strategy says otherwise.
If the repository does not allow merge commits, the merge strategy falls back to squash.

Progress is shown as one line per pull request with a status icon, notes like branch updates
in a box below it. Polling starts at --poll-interval and doubles after every 3 failed attempts
(API errors, merges GitHub rejected for now) up to --max-poll-interval, to stay below the rate limit.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMerge(cmd, opts)
		},
	}

	cmd.Flags().BoolVar(&opts.allReady, "all-ready", false, "merge all open PRs labeled with the ready-to-merge label, ignoring review status")
	addMergeFlags(cmd, opts)

	return cmd
}

// addMergeFlags adds the flags pr merge and pr ship share.
func addMergeFlags(cmd *cobra.Command, opts *mergeOptions) {
	cmd.Flags().StringVar(&opts.readyLabel, "ready-to-merge-label", "",
		"ready-to-merge label (default \"done\", or pr.ready_to_merge_label from the config)")
	cmd.Flags().StringVarP(&opts.strategy, "strategy", "s", "",
		"merge strategy: squash or merge (default squash, or pr.merge_strategy from the config)")
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "do not ask for confirmation")
	cmd.Flags().DurationVar(&opts.waitTimeout, "wait-timeout", 15*time.Minute, "how long to wait for a pull request to become mergeable")
	cmd.Flags().DurationVar(&opts.pollInterval, "poll-interval", defaultPollInterval,
		"initial interval between checks whether a pull request is mergeable; it doubles after every 3 failed attempts")
	cmd.Flags().DurationVar(&opts.maxPollInterval, "max-poll-interval", defaultMaxPollInterval, "upper bound of the poll interval")
	cmd.Flags().BoolVar(&opts.updateBranch, "update-branch", true, "update a pull request branch that is behind its base before merging")
}

// resolve fills in strategy and ready-to-merge label from the config where no flag was given.
func (opts *mergeOptions) resolve(cmd *cobra.Command) error {
	cfg, err := loadPRConfig(cmd)
	if err != nil {
		return err
	}
	if opts.strategy, err = resolveStrategy(opts.strategy, cfg); err != nil {
		return err
	}
	opts.readyLabel = resolveReadyLabel(opts.readyLabel, cfg)
	return nil
}

func runMerge(cmd *cobra.Command, opts *mergeOptions) error {
	if err := opts.resolve(cmd); err != nil {
		return err
	}

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

	if opts.allReady {
		return mergeAllReady(ctx, client, repo, prs, opts)
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

	return mergeBoard(ctx, client, repo, []*github.PullRequest{selected}, opts)
}

func mergeAllReady(ctx context.Context, client *github.Client, repo gh.Repo, prs []*github.PullRequest, opts *mergeOptions) error {
	var ready []*github.PullRequest
	for _, pr := range prs {
		if hasLabel(pr, opts.readyLabel) {
			ready = append(ready, pr)
		}
	}

	if len(ready) == 0 {
		fmt.Printf("No open pull requests labeled %q in %s\n", opts.readyLabel, repo)
		return nil
	}

	fmt.Printf("Open pull requests labeled %q in %s:\n", opts.readyLabel, repo)
	for _, pr := range ready {
		fmt.Println("  " + describePR(pr))
	}

	if !opts.yes {
		ok, err := confirm(fmt.Sprintf("Merge these %d pull requests using %s?", len(ready), opts.strategy))
		if err != nil || !ok {
			return err
		}
	}

	return mergeBoard(ctx, client, repo, ready, opts)
}

// mergeBoard merges prs one after another, since every merge changes the base of the next one,
// showing the progress of all of them on a board.
func mergeBoard(ctx context.Context, client *github.Client, repo gh.Repo, prs []*github.PullRequest, opts *mergeOptions) error {
	labels := make([]string, len(prs))
	for i, pr := range prs {
		labels[i] = fmt.Sprintf("#%d %s", pr.GetNumber(), truncate(pr.GetTitle(), 40))
	}
	b := newBoard(os.Stdout, labels)

	var failed []int
	for i, pr := range prs {
		summary, err := mergePR(ctx, client, repo, pr, opts, b.row(i))
		if err != nil {
			b.fail(i, fmt.Sprintf("%v (%s)", err, pr.GetHTMLURL()))
			failed = append(failed, pr.GetNumber())
			continue
		}
		b.done(i, summary)
	}

	if len(failed) > 0 {
		return fmt.Errorf("%d of %d pull requests could not be merged: %v", len(failed), len(prs), failed)
	}
	return nil
}

// mergePR waits until the PR is mergeable and merges it, returning a one-line summary. After a
// previous merge GitHub recomputes mergeability and required checks may re-run, so the PR is
// polled until it can be merged, its branch is updated when it is behind the base, or
// opts.waitTimeout is reached. API errors and merges GitHub rejects for now count as failed
// attempts for the poll backoff. Progress goes to rep.
func mergePR(ctx context.Context, client *github.Client, repo gh.Repo, pr *github.PullRequest, opts *mergeOptions, rep reporter) (string, error) {
	number := pr.GetNumber()
	backoff := opts.pollBackoff()
	fail := func(format string, args ...any) (string, error) {
		return "", fmt.Errorf(format, args...)
	}

	deadline := time.Now().Add(opts.waitTimeout)
	strategy := opts.strategy
	branchUpdated := false

	for {
		var (
			status  string
			current *github.PullRequest
		)
		current, _, err := client.PullRequests.Get(ctx, repo.Owner, repo.Name, number)
		if err != nil {
			backoff.failure()
			status = fmt.Sprintf("fetching pull request failed: %v", err)
			current = nil
		}

		state := ""
		if current != nil {
			state = current.GetMergeableState()
		}
		switch {
		case current == nil:
			// fetching failed, retry below
		case current.GetMerged():
			return "was already merged", nil
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
			rep.Note("branch was behind " + current.GetBase().GetRef() + ", updated it")
			status = "waiting for the updated branch"
		default:
			// clean, unstable, has_hooks, blocked or behind: try it, review status is ignored on purpose.
			result, _, err := client.PullRequests.Merge(ctx, repo.Owner, repo.Name, number, "",
				&github.PullRequestOptions{MergeMethod: strategy})
			if err == nil {
				return fmt.Sprintf("merged using %s (%s)", strategy, shortSHA(result.GetSHA())), nil
			}
			var ghErr *github.ErrorResponse
			if !errors.As(err, &ghErr) || ghErr.Response == nil ||
				(ghErr.Response.StatusCode != http.StatusMethodNotAllowed && ghErr.Response.StatusCode != http.StatusConflict) {
				return fail("merging: %v", err)
			}
			if strategy == strategyMerge && mergeCommitsNotAllowed(ghErr) {
				// The repository only allows squash (or rebase): fall back to squash and retry right away.
				rep.Note(fmt.Sprintf("merge commits are not allowed in %s, using squash", repo))
				strategy = strategySquash
				continue
			}
			// 405/409: not mergeable yet, e.g. required checks still running or base branch just changed.
			backoff.failure()
			status = fmt.Sprintf("not mergeable yet (%s): %s", state, ghErr.Message)
		}

		if time.Now().After(deadline) {
			return fail("still not mergeable after %s: %s", opts.waitTimeout, status)
		}
		interval := backoff.current()
		rep.Status(fmt.Sprintf("%s, next check in %s", status, interval))

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(interval):
		}
	}
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
