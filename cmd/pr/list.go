package pr

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-github/v92/github"
	"github.com/mattn/go-runewidth"
	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	gh "github.com/EugenMayer/ghcli/internal/github"
)

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List open pull requests",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := setup(cmd)
			if err != nil {
				return err
			}
			repo := s.repo

			prs, err := s.listOpenPRs(cmd.Context())
			if err != nil {
				return err
			}

			if len(prs) == 0 {
				fmt.Printf("No open pull requests in %s\n", repo)
				return nil
			}
			return renderPRTable(os.Stdout, repo, prs, time.Now(), terminalWidth())
		},
	}
}

// terminalWidth returns the width of the terminal on stdout, or 0 if stdout is not a terminal.
func terminalWidth() int {
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return 0
	}
	return width
}

// renderPRTable writes prs as a table. maxWidth > 0 limits the table width by truncating the title
// (and very long branch names) instead of squeezing every column.
func renderPRTable(w io.Writer, repo gh.Repo, prs []*github.PullRequest, now time.Time, maxWidth int) error {
	header := []string{"#", "Title", "Author", "Branch", "Labels", "Age"}
	const titleCol, branchCol, minColumnWidth = 1, 3, 20

	rows := make([][]string, 0, len(prs))
	for _, pr := range prs {
		title := pr.GetTitle()
		if pr.GetDraft() {
			title = "[draft] " + title
		}

		labels := make([]string, 0, len(pr.Labels))
		for _, l := range pr.Labels {
			labels = append(labels, l.GetName())
		}

		rows = append(rows, []string{
			strconv.Itoa(pr.GetNumber()),
			title,
			pr.GetUser().GetLogin(),
			truncate(pr.GetHead().GetRef()+" → "+pr.GetBase().GetRef(), 50),
			strings.Join(labels, ", "),
			age(now.Sub(pr.GetCreatedAt().Time)),
		})
	}

	if maxWidth > 0 {
		// Each column takes its content width plus 3 (padding and separator), plus the left border.
		// Title and branch share what the other columns leave; the branch gets at most a third.
		available := maxWidth - 1
		for col := range header {
			available -= 3
			if col != titleCol && col != branchCol {
				available -= columnWidth(header, rows, col)
			}
		}
		branchWidth := min(columnWidth(header, rows, branchCol), max(available/3, minColumnWidth))
		titleWidth := max(available-branchWidth, minColumnWidth)
		for _, row := range rows {
			row[titleCol] = truncate(row[titleCol], titleWidth)
			row[branchCol] = truncate(row[branchCol], branchWidth)
		}
	}

	table := tablewriter.NewTable(w,
		tablewriter.WithRendition(tw.Rendition{Symbols: tw.NewSymbols(tw.StyleRounded)}),
		tablewriter.WithHeaderAlignment(tw.AlignLeft),
		tablewriter.WithRowAlignmentConfig(tw.CellAlignment{PerColumn: []tw.Align{tw.AlignRight}}),
	)
	table.Header(header)
	if err := table.Bulk(rows); err != nil {
		return err
	}
	if err := table.Render(); err != nil {
		return err
	}

	noun := "pull requests"
	if len(prs) == 1 {
		noun = "pull request"
	}
	_, err := fmt.Fprintf(w, "%d open %s in %s\n", len(prs), noun, repo)
	return err
}

func columnWidth(header []string, rows [][]string, col int) int {
	width := runewidth.StringWidth(header[col])
	for _, row := range rows {
		width = max(width, runewidth.StringWidth(row[col]))
	}
	return width
}

// truncate shortens s to at most width display columns, ending with "…" when cut.
func truncate(s string, width int) string {
	return runewidth.Truncate(s, width, "…")
}

// age formats a duration compactly: 45m, 5h, 3d, 6w, 2y.
func age(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 14*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dw", int(d.Hours()/24/7))
	default:
		return fmt.Sprintf("%dy", int(d.Hours()/24/365))
	}
}
