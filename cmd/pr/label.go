package pr

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/go-github/v92/github"
	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"

	gh "github.com/EugenMayer/ghcli/internal/github"
)

func newLabelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "label",
		Short: "Label open pull requests interactively",
		Long: `Pick a label, then label open pull requests that don't have it yet. The ready-to-merge
label ('done' unless pr.ready_to_merge_label in the config says otherwise) is preselected.

In the list, space checks or unchecks a pull request, ctrl+a checks or unchecks all. enter labels
the checked pull requests one after another. q or esc quits without labeling, ctrl+c while
labeling aborts.

Adding labels to pull requests needs the repository permission "Pull requests: read and write";
read is not enough.`,
		Args: cobra.NoArgs,
		RunE: runLabel,
	}
}

func runLabel(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	cfg, err := loadPRConfig(cmd)
	if err != nil {
		return err
	}
	s, err := setup(cmd)
	if err != nil {
		return err
	}
	repo := s.repo

	labels, err := listLabels(ctx, s.client, s.repo)
	if err != nil {
		return err
	}
	if len(labels) == 0 {
		return fmt.Errorf("repository %s has no labels", repo)
	}
	label, err := selectLabel(repo, labels, resolveReadyLabel("", cfg))
	if err != nil {
		return err
	}

	prs, err := s.listOpenPRs(ctx)
	if err != nil {
		return err
	}
	m, err := s.labelInteractively(ctx, label, prs, "label checked", nil)
	if err != nil || m == nil {
		return err
	}
	return m.summary(cmd.OutOrStdout())
}

// labelInteractively shows the open pull requests without the label and lets the user label them.
// finishHint describes what enter does. It returns nil if all pull requests already have the label.
// Pull requests carrying one of blockLabels are listed but cannot be checked.
func (s *session) labelInteractively(ctx context.Context, label string, prs []*github.PullRequest, finishHint string, blockLabels []string) (*labelModel, error) {
	var unlabeled []*github.PullRequest
	for _, pr := range prs {
		if !hasLabel(pr, label) {
			unlabeled = append(unlabeled, pr)
		}
	}
	if len(unlabeled) == 0 {
		fmt.Printf("All open pull requests in %s are labeled %q\n", s.repo, label)
		return nil, nil
	}

	apply := func(ctx context.Context, number int) error {
		_, _, err := s.client.Issues.AddLabelsToIssue(ctx, s.repo.Owner, s.repo.Name, number, []string{label})
		return labelsAPIError(err)
	}
	m := newLabelModel(ctx, s.repo, label, unlabeled, apply)
	m.finishHint = finishHint
	m.blockLabels = blockLabels
	final, err := tea.NewProgram(m, tea.WithContext(ctx)).Run()
	if err != nil {
		return nil, err
	}
	return final.(*labelModel), nil
}

func listLabels(ctx context.Context, client *github.Client, repo gh.Repo) ([]*github.Label, error) {
	opts := &github.ListOptions{PerPage: 100}
	var all []*github.Label
	for {
		labels, resp, err := client.Issues.ListLabels(ctx, repo.Owner, repo.Name, opts)
		if err != nil {
			return nil, fmt.Errorf("listing labels of %s: %w", repo, labelsAPIError(err))
		}
		all = append(all, labels...)
		if resp.NextPage == 0 {
			return all, nil
		}
		opts.Page = resp.NextPage
	}
}

// labelsAPIError shortens an error of the labels API to status and message, e.g. "403 Resource not
// accessible by personal access token", and explains a 403 with the permission GitHub asks for.
func labelsAPIError(err error) error {
	var ghErr *github.ErrorResponse
	if !errors.As(err, &ghErr) || ghErr.Response == nil {
		return err
	}
	msg := fmt.Sprintf("%d %s", ghErr.Response.StatusCode, ghErr.Message)
	if ghErr.Response.StatusCode == http.StatusForbidden {
		// GitHub names the permissions that would allow the request.
		if accepted := ghErr.Response.Header.Get("X-Accepted-GitHub-Permissions"); accepted != "" {
			msg += " (the token needs " + describeAcceptedPermissions(accepted) + `; set "Pull requests" to "Read and write")`
		} else {
			msg += ` (the token needs the repository permission "Pull requests: read and write")`
		}
	}
	return errors.New(msg)
}

// describeAcceptedPermissions turns GitHub's X-Accepted-GitHub-Permissions header into text. The
// header lists alternative permission sets separated by ";", the permissions of a set by ",":
// "issues=write; pull_requests=write" means either one is enough.
func describeAcceptedPermissions(header string) string {
	var sets []string
	for _, set := range strings.Split(header, ";") {
		var perms []string
		for _, p := range strings.Split(set, ",") {
			if p = strings.TrimSpace(p); p != "" {
				perms = append(perms, p)
			}
		}
		if len(perms) > 0 {
			sets = append(sets, strings.Join(perms, " and "))
		}
	}
	if len(sets) == 1 {
		return sets[0]
	}
	return "one of: " + strings.Join(sets, ", or ")
}

// selectLabel lets the user pick a label, with the cursor on the preselected label if it exists.
func selectLabel(repo gh.Repo, labels []*github.Label, preselected string) (string, error) {
	items := make([]string, 0, len(labels))
	cursor := 0
	for i, l := range labels {
		item := l.GetName()
		if d := l.GetDescription(); d != "" {
			item += " – " + d
		}
		items = append(items, item)
		if strings.EqualFold(l.GetName(), preselected) {
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

// labelModel is a checkbox list: space checks pull requests, enter labels the checked ones. Labels
// are applied one at a time, so GitHub's limits for write requests aren't triggered.
type labelModel struct {
	ctx   context.Context
	repo  gh.Repo
	label string
	apply labeler

	finishHint  string   // what enter does, shown in the help line
	blockLabels []string // pull requests with one of these labels cannot be checked

	prs     []*github.PullRequest
	checked map[int]bool // PR number -> checked
	cursor  int
	offset  int // first visible row
	height  int // terminal height, 0 until known
	width   int // terminal width, 0 until known

	queue    []*github.PullRequest // checked pull requests not labeled yet, once enter was pressed
	inFlight *github.PullRequest

	labeled  []*github.PullRequest
	failed   map[int]error
	messages []string // results shown at the bottom, newest last

	applying bool // enter was pressed, the checked pull requests are being labeled
	finished bool // all checked pull requests were processed
	aborted  bool // ctrl+c while labeling
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
	return &labelModel{
		ctx: ctx, repo: repo, label: label, apply: apply, finishHint: "label checked",
		prs: slices.Clone(prs), checked: map[int]bool{}, failed: map[int]error{},
	}
}

func (m *labelModel) Init() tea.Cmd { return nil }

func (m *labelModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height, m.width = msg.Height, msg.Width
		m.scroll()

	case tea.KeyPressMsg:
		return m.key(msg.String())

	case labelDoneMsg:
		m.inFlight = nil
		number := msg.pr.GetNumber()
		if msg.err != nil {
			m.failed[number] = msg.err
			m.message(styleErr.Render(fmt.Sprintf("✘ #%d %s: %v", number, msg.pr.GetTitle(), msg.err)))
		} else {
			m.labeled = append(m.labeled, msg.pr)
			m.message(styleOK.Render(fmt.Sprintf("✔ #%d %s labeled %q", number, msg.pr.GetTitle(), m.label)))
		}
		return m, m.next()
	}
	return m, nil
}

func (m *labelModel) key(key string) (tea.Model, tea.Cmd) {
	if m.applying {
		if key == "ctrl+c" {
			m.aborted = true
			return m, m.quit()
		}
		return m, nil
	}

	switch key {
	case "ctrl+c", "q", "esc":
		return m, m.quit()
	case "up", "k":
		m.cursor = max(0, m.cursor-1)
	case "down", "j":
		m.cursor = min(len(m.prs)-1, m.cursor+1)
	case "home", "g":
		m.cursor = 0
	case "end", "G":
		m.cursor = len(m.prs) - 1
	case "space":
		if len(m.prs) > 0 && m.blockedBy(m.prs[m.cursor]) == "" {
			n := m.prs[m.cursor].GetNumber()
			m.checked[n] = !m.checked[n]
		}
	case "ctrl+a":
		all := !m.allChecked()
		for _, pr := range m.prs {
			m.checked[pr.GetNumber()] = all && m.blockedBy(pr) == ""
		}
	case "enter":
		m.applying = true
		for _, pr := range m.prs {
			if m.checked[pr.GetNumber()] {
				m.queue = append(m.queue, pr)
			}
		}
		return m, m.next()
	}
	m.cursor = max(0, m.cursor)
	m.scroll()
	return m, nil
}

func (m *labelModel) allChecked() bool {
	return !slices.ContainsFunc(m.prs, func(pr *github.PullRequest) bool {
		return m.blockedBy(pr) == "" && !m.checked[pr.GetNumber()]
	})
}

// blockedBy returns the block label that keeps pr from being checked, or "" if there is none.
func (m *labelModel) blockedBy(pr *github.PullRequest) string {
	return blockingLabel(pr, m.blockLabels)
}

// next starts labeling the next checked pull request, or quits when all are processed.
func (m *labelModel) next() tea.Cmd {
	if m.inFlight != nil {
		return nil
	}
	if len(m.queue) == 0 {
		m.finished = true
		return m.quit()
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
	m.offset = max(0, min(m.offset, len(m.prs)-rows))
}

func (m *labelModel) View() tea.View {
	if m.done {
		return tea.NewView("") // the summary is printed after the program ends
	}

	var b strings.Builder
	b.WriteString(m.fit(styleTitle.Render(fmt.Sprintf("Label pull requests of %s with %q", m.repo, m.label))) + "\n")
	b.WriteString(m.fit(styleHelp.Render("↑/↓ move · space check · ctrl+a check all · enter "+m.finishHint+" · q quit")) + "\n\n")

	end := min(len(m.prs), m.offset+m.rows())
	for i := m.offset; i < end; i++ {
		pr := m.prs[i]
		box := "[ ]"
		if m.checked[pr.GetNumber()] {
			box = "[x]"
		}
		blocked := m.blockedBy(pr)
		if blocked != "" {
			box = "[-]"
		}
		line := box + " " + describePR(pr)
		if blocked != "" {
			line = styleHelp.Render(line + fmt.Sprintf("  (blocked by label %q)", blocked))
		}
		if err, ok := m.failed[pr.GetNumber()]; ok {
			line += styleErr.Render(fmt.Sprintf("  (failed: %v)", err))
		}
		if i == m.cursor && !m.applying {
			line = styleCursor.Render("› " + line)
		} else {
			line = "  " + line
		}
		b.WriteString(m.fit(line) + "\n")
	}
	if hidden := len(m.prs) - end; hidden > 0 {
		b.WriteString(styleHelp.Render(fmt.Sprintf("  … %d more", hidden)) + "\n")
	}

	b.WriteString("\n")
	for _, msg := range m.messages {
		b.WriteString(m.wrap(msg) + "\n")
	}
	if m.inFlight != nil {
		status := fmt.Sprintf("… labeling #%d", m.inFlight.GetNumber())
		if n := len(m.queue); n > 0 {
			status += fmt.Sprintf(", %d left", n)
		}
		status += " (ctrl+c to abort)"
		b.WriteString(m.wrap(styleHelp.Render(status)) + "\n")
	}
	return tea.NewView(b.String())
}

// fit cuts a line to the terminal width, so it isn't clipped mid-way by the terminal.
func (m *labelModel) fit(line string) string {
	if m.width <= 0 {
		return line
	}
	return ansi.Truncate(line, m.width, "…")
}

// wrap breaks a message into lines of the terminal width, so long errors stay readable.
func (m *labelModel) wrap(msg string) string {
	if m.width <= 0 {
		return msg
	}
	return lipgloss.Wrap(msg, m.width, " ")
}

// summary prints the result and returns an error if pull requests could not be labeled.
func (m *labelModel) summary(w io.Writer) error {
	if !m.applying {
		fmt.Fprintln(w, "quit, no pull requests were labeled")
		return nil
	}
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
