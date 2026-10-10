package pr

import "github.com/spf13/cobra"

// NewCmd returns the 'pr' namespace command.
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "pr",
		Aliases: []string{"pullrequest"},
		Short:   "Work with pull requests",
	}

	cmd.AddCommand(newListCmd(), newMergeCmd())

	return cmd
}
