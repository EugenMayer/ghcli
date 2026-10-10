package pr

import (
	"context"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/google/go-github/v92/github"
	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"

	gh "github.com/EugenMayer/ghcli/internal/github"
)

func newLabelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "label",
		Short: "Label open pull requests interactively",
		Long: `Pick a label (preselected: 'done'), then label open pull requests that don't have it yet.

In the list, enter labels the selected pull request and removes it from the list; the label is
applied in the background. ctrl+a labels all remaining pull requests. q or esc quits once the
pending labels are applied, ctrl+c twice aborts.

The token needs the repository permissions "Issues: read and write" (GitHub manages pull request
labels through the Issues API) and "Pull requests: read".`,
		Args: cobra.NoArgs,
		RunE: runLabel,
	}
}

func runLabel(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	s, err := setup(cmd)
	if err != nil {
		return err
	}
	client, repo := s.client, s.repo

	labels, err := listLabels(ctx, client, repo)
	if err != nil {
		return err
	}
	if len(labels) == 0 {
		return fmt.Errorf("repository %s has no labels", repo)
	}
	label, err := selectLabel(repo, labels)
	if err != nil {
		return err
	}

	prs, err := s.listOpenPRs(ctx)
	if err != nil {
		return err
	}
	var unlabeled []*github.PullRequest
	for _, pr := range prs {
		if !hasLabel(pr, label) {
			unlabeled = append(unlabeled, pr)
		}
	}
	if len(unlabeled) == 0 {
		fmt.Printf("All open pull requests in %s are labeled %q\n", repo, label)
		return nil
	}

	apply := func(ctx context.Context, number int) error {
		_, resp, err := client.Issues.AddLabelsToIssue(ctx, repo.Owner, repo.Name, number, []string{label})
		if err != nil {
			return fmt.Errorf("%w%s", err, issuesPermissionHint(resp))
		}
		return nil
	}
	final, err := tea.NewProgram(newLabelModel(ctx, repo, label, unlabeled, apply), tea.WithContext(ctx)).Run()
	if err != nil {
		return err
	}
	return final.(*labelModel).summary(cmd.OutOrStdout())
}

func listLabels(ctx context.Context, client *github.Client, repo gh.Repo) ([]*github.Label, error) {
	opts := &github.ListOptions{PerPage: 100}
	var all []*github.Label
	for {
		labels, resp, err := client.Issues.ListLabels(ctx, repo.Owner, repo.Name, opts)
		if err != nil {
			return nil, fmt.Errorf("listing labels of %s: %w%s", repo, err, issuesPermissionHint(resp))
		}
		all = append(all, labels...)
		if resp.NextPage == 0 {
			return all, nil
		}
		opts.Page = resp.NextPage
	}
}

// issuesPermissionHint explains a 403 from the labels API, which needs the Issues permission.
func issuesPermissionHint(resp *github.Response) string {
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		return ""
	}
	return ` (the token needs the repository permission "Issues: read and write" for labels)`
}

// selectLabel lets the user pick a label, with the cursor on the done label if it exists.
func selectLabel(repo gh.Repo, labels []*github.Label) (string, error) {
	items := make([]string, 0, len(labels))
	cursor := 0
	for i, l := range labels {
		item := l.GetName()
		if d := l.GetDescription(); d != "" {
			item += " – " + d
		}
		items = append(items, item)
		if strings.EqualFold(l.GetName(), defaultDoneLabel) {
			cursor = i
		}
	}

	prompt := promptui.Select{
		Label: fmt.Sprintf("Label for pull requests of %s", repo),
		Items: items,
		Size:  15,
		Searcher: func(input string, index int) bool {
			return strings.Contains(strings.ToLower(items[index]), strings.ToLower(input))
		},
	}
	idx, _, err := prompt.RunCursorAt(cursor, max(0, cursor-prompt.Size+1))
	if err != nil {
		return "", err
	}
	return labels[idx].GetName(), nil
}

// labeler applies the label to a pull request.
type labeler func(ctx context.Context, number int) error

// labelDoneMsg reports the result of labeling one pull request.
type labelDoneMsg struct {
	pr  *github.PullRequest
	err error
}

// labelModel is the interactive list. Labeling is optimistic: a pull request leaves the list as
// soon as it is chosen, and returns with an error message if labeling fails. Labels are applied
// one at a time in the background, so GitHub's limits for write requests aren't triggered.
type labelModel struct {
	ctx   context.Context
	repo  gh.Repo
	label string
	apply labeler

	remaining []*github.PullRequest
	order     map[int]int // PR number -> position in the original list, to put failed ones back
	cursor    int
	offset    int // first visible row
	height    int // terminal height, 0 until known

	queue    []*github.PullRequest
	inFlight *github.PullRequest

	labeled  []*github.PullRequest
	failed   map[int]error
	messages []string // confirmations shown at the bottom, newest last

	quitting bool // waiting for pending labels before quitting
	aborted  bool
	done     bool
}

const maxMessages = 3

var (
	styleTitle  = lipgloss.NewStyle().Bold(true)
	styleHelp   = lipgloss.NewStyle().Faint(true)
	styleCursor = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	styleOK     = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styleErr    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
)

func newLabelModel(ctx context.Context, repo gh.Repo, label string, prs []*github.PullRequest, apply labeler) *labelModel {
	order := make(map[int]int, len(prs))
	for i, pr := range prs {
		order[pr.GetNumber()] = i
	}
	return &labelModel{
		ctx: ctx, repo: repo, label: label, apply: apply,
		remaining: slices.Clone(prs), order: order, failed: map[int]error{},
	}
}

func (m *labelModel) Init() tea.Cmd { return nil }

func (m *labelModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
		m.scroll()

	case tea.KeyPressMsg:
		return m.key(msg.String())

	case labelDoneMsg:
		m.inFlight = nil
		number := msg.pr.GetNumber()
		if msg.err != nil {
			m.failed[number] = msg.err
			m.putBack(msg.pr)
			m.message(styleErr.Render(fmt.Sprintf("✘ #%d %s: %v", number, msg.pr.GetTitle(), msg.err)))
		} else {
			delete(m.failed, number)
			m.labeled = append(m.labeled, msg.pr)
			m.message(styleOK.Render(fmt.Sprintf("✔ #%d %s labeled %q", number, msg.pr.GetTitle(), m.label)))
		}
		return m, m.next()
	}
	return m, nil
}

func (m *labelModel) key(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "ctrl+c":
		if m.quitting {
			m.aborted = true
			return m, m.quit()
		}
		m.quitting = true
		return m, m.next()
	case "q", "esc":
		m.quitting = true
		return m, m.next()
	}
	if m.quitting {
		return m, nil
	}

	switch key {
	case "up", "k":
		m.cursor = max(0, m.cursor-1)
	case "down", "j":
		m.cursor = min(len(m.remaining)-1, m.cursor+1)
	case "home", "g":
		m.cursor = 0
	case "end", "G":
		m.cursor = len(m.remaining) - 1
	case "enter", "space":
		if len(m.remaining) > 0 {
			m.queue = append(m.queue, m.remaining[m.cursor])
			m.remaining = slices.Delete(m.remaining, m.cursor, m.cursor+1)
			m.cursor = min(m.cursor, len(m.remaining)-1)
		}
	case "ctrl+a":
		m.queue = append(m.queue, m.remaining...)
		m.remaining = nil
		m.cursor = 0
	}
	m.cursor = max(0, m.cursor)
	m.scroll()
	return m, m.next()
}

// next starts labeling the next queued pull request, or quits when there is nothing left to do.
func (m *labelModel) next() tea.Cmd {
	if m.inFlight != nil {
		return nil
	}
	if len(m.queue) == 0 {
		if m.quitting || len(m.remaining) == 0 {
			return m.quit()
		}
		return nil
	}

	pr := m.queue[0]
	m.queue = m.queue[1:]
	m.inFlight = pr
	ctx, apply := m.ctx, m.apply
	return func() tea.Msg { return labelDoneMsg{pr: pr, err: apply(ctx, pr.GetNumber())} }
}

func (m *labelModel) quit() tea.Cmd {
	m.done = true
	return tea.Quit
}

// putBack re-inserts a pull request at its original position.
func (m *labelModel) putBack(pr *github.PullRequest) {
	pos, _ := slices.BinarySearchFunc(m.remaining, m.order[pr.GetNumber()], func(p *github.PullRequest, target int) int {
		return m.order[p.GetNumber()] - target
	})
	m.remaining = slices.Insert(m.remaining, pos, pr)
	if pos <= m.cursor && len(m.remaining) > 1 {
		m.cursor++ // keep the cursor on the same pull request
	}
	m.cursor = min(m.cursor, len(m.remaining)-1)
	m.scroll()
}

func (m *labelModel) message(s string) {
	m.messages = append(m.messages, s)
	if len(m.messages) > maxMessages {
		m.messages = m.messages[len(m.messages)-maxMessages:]
	}
}

// rows is the number of list rows that fit the terminal next to title, help and messages.
func (m *labelModel) rows() int {
	if m.height == 0 {
		return 15
	}
	return max(3, m.height-5-maxMessages)
}

// scroll keeps the cursor within the visible rows.
func (m *labelModel) scroll() {
	rows := m.rows()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+rows {
		m.offset = m.cursor - rows + 1
	}
	m.offset = max(0, min(m.offset, len(m.remaining)-rows))
}

func (m *labelModel) View() tea.View {
	if m.done {
		return tea.NewView("") // the summary is printed after the program ends
	}

	var b strings.Builder
	b.WriteString(styleTitle.Render(fmt.Sprintf("Label pull requests of %s with %q", m.repo, m.label)) + "\n")
	b.WriteString(styleHelp.Render("↑/↓ move · enter label · ctrl+a label all · q quit") + "\n\n")

	if len(m.remaining) == 0 {
		b.WriteString(styleHelp.Render("  no unlabeled pull requests left") + "\n")
	}
	end := min(len(m.remaining), m.offset+m.rows())
	for i := m.offset; i < end; i++ {
		line := describePR(m.remaining[i])
		if err, ok := m.failed[m.remaining[i].GetNumber()]; ok {
			line += styleErr.Render(fmt.Sprintf("  (failed: %v)", err))
		}
		if i == m.cursor {
			b.WriteString(styleCursor.Render("› "+line) + "\n")
		} else {
			b.WriteString("  " + line + "\n")
		}
	}
	if hidden := len(m.remaining) - end; hidden > 0 {
		b.WriteString(styleHelp.Render(fmt.Sprintf("  … %d more", hidden)) + "\n")
	}

	b.WriteString("\n")
	for _, msg := range m.messages {
		b.WriteString(msg + "\n")
	}
	if m.inFlight != nil {
		status := fmt.Sprintf("… labeling #%d", m.inFlight.GetNumber())
		if n := len(m.queue); n > 0 {
			status += fmt.Sprintf(", %d queued", n)
		}
		if m.quitting {
			status += " (quitting when done, ctrl+c to abort)"
		}
		b.WriteString(styleHelp.Render(status) + "\n")
	}
	return tea.NewView(b.String())
}

// summary prints the result and returns an error if pull requests could not be labeled.
func (m *labelModel) summary(w io.Writer) error {
	for _, pr := range m.labeled {
		fmt.Fprintf(w, "✔ #%d %s labeled %q\n", pr.GetNumber(), pr.GetTitle(), m.label)
	}
	notDone := len(m.queue)
	if m.inFlight != nil {
		notDone++
	}
	if m.aborted && notDone > 0 {
		fmt.Fprintf(w, "aborted, %d pull requests were not labeled\n", notDone)
	}
	if len(m.failed) > 0 {
		numbers := slices.Sorted(maps.Keys(m.failed))
		for _, n := range numbers {
			fmt.Fprintf(w, "✘ #%d: %v\n", n, m.failed[n])
		}
		return fmt.Errorf("%d pull requests could not be labeled: %v", len(numbers), numbers)
	}
	return nil
}
