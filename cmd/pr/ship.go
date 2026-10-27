package pr

import (
	"github.com/spf13/cobra"
)

func newShipCmd() *cobra.Command {
	opts := &mergeOptions{allReady: true}

	cmd := &cobra.Command{
		Use:   "ship",
		Short: "Label pull requests as ready to merge, then merge all ready ones",
		Long: `Combines pr label and pr merge --all-ready.

First the open pull requests without the ready-to-merge label ('done' unless pr.ready_to_merge_label
in the config or --ready-to-merge-label says otherwise) are listed: space checks a pull request,
ctrl+a checks all, enter labels the checked ones. Then every open pull request carrying the label,
including those labeled before, is merged one after another like pr merge --all-ready does.
q or esc in the list quits without labeling or merging.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runShip(cmd, opts)
		},
	}
	addMergeFlags(cmd, opts)

	return cmd
}

func runShip(cmd *cobra.Command, opts *mergeOptions) error {
	if err := opts.resolve(cmd); err != nil {
		return err
	}

	ctx := cmd.Context()
	s, err := setup(cmd)
	if err != nil {
		return err
	}

	prs, err := s.listOpenPRs(ctx)
	if err != nil {
		return err
	}
	m, err := s.labelInteractively(ctx, opts.readyLabel, prs, "label checked and merge", opts.blockLabels)
	if err != nil {
		return err
	}
	if m != nil {
		if err := m.summary(cmd.OutOrStdout()); err != nil {
			return err
		}
		if !m.finished {
			return nil
		}
		// Labels changed, list again to merge exactly what GitHub has now.
		if prs, err = s.listOpenPRs(ctx); err != nil {
			return err
		}
	}

	return mergeAllReady(ctx, s.client, s.repo, prs, opts)
}
