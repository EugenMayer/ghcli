package pr

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

// Default poll interval bounds of pr merge: polling starts at defaultPollInterval and
// doubles after every backoffFailuresPerStep failed attempts, up to defaultMaxPollInterval.
const (
	defaultPollInterval    = 5 * time.Second
	defaultMaxPollInterval = 60 * time.Second
	backoffFailuresPerStep = 3
)

// pollBackoff is the poll interval shared by everything polling GitHub with one token,
// since the rate limit applies to the token. It never shrinks again.
type pollBackoff struct {
	mu       sync.Mutex
	failures int
	interval time.Duration
	max      time.Duration
}

func newPollBackoff(initial, max time.Duration) *pollBackoff {
	return &pollBackoff{interval: initial, max: max}
}

func (b *pollBackoff) current() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.interval
}

// failure records a failed attempt (an API error or a merge GitHub rejected for now).
func (b *pollBackoff) failure() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures++
	if b.failures%backoffFailuresPerStep == 0 && b.interval < b.max {
		b.interval = min(b.interval*2, b.max)
	}
}

type prState int

const (
	statePending prState = iota
	stateRunning
	stateDone
	stateFailed
)

const (
	ansiGrey  = "\x1b[90m"
	ansiGreen = "\x1b[32m"
	ansiRed   = "\x1b[31m"
	ansiCyan  = "\x1b[36m"
	ansiReset = "\x1b[0m"

	boxIndent  = "  "
	boxMinText = 10
	boxMaxText = 100
)

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// reporter receives the progress of merging one pull request.
type reporter interface {
	// Status replaces the status shown for the pull request while it is being processed.
	Status(status string)
	// Note adds a line of detail that belongs to the pull request.
	Note(line string)
}

type discardReporter struct{}

func (discardReporter) Status(string) {}
func (discardReporter) Note(string)   {}

// board shows one line per pull request, "<icon> <label>  <status>", updated in place on a
// terminal: a refresh arrow while a pull request is processed, a green check when done, a
// red cross when failed and a grey circle while it is queued. Notes are drawn in a grey box
// beneath the line of their pull request. Without a terminal every change is printed as a
// plain line instead.
type board struct {
	mu       sync.Mutex
	out      io.Writer
	tty      *os.File
	labels   []string
	states   []prState
	statuses []string
	notes    [][]string
	rendered int // number of lines currently on screen
}

func newBoard(out io.Writer, labels []string) *board {
	width := 0
	for _, l := range labels {
		width = max(width, len([]rune(l)))
	}
	b := &board{
		out:      out,
		labels:   make([]string, len(labels)),
		states:   make([]prState, len(labels)),
		statuses: make([]string, len(labels)),
		notes:    make([][]string, len(labels)),
	}
	for i, l := range labels {
		b.labels[i] = l + strings.Repeat(" ", width-len([]rune(l)))
		b.statuses[i] = "queued"
	}
	if f, ok := out.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		b.tty = f
	}
	b.mu.Lock()
	b.drawLocked()
	b.mu.Unlock()
	return b
}

// row returns the reporter of the pull request at index i.
func (b *board) row(i int) reporter { return boardRow{b, i} }

type boardRow struct {
	b *board
	i int
}

func (r boardRow) Status(status string) { r.b.update(r.i, stateRunning, status) }

func (r boardRow) Note(line string) {
	b := r.b
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.tty == nil {
		fmt.Fprintf(b.out, "%s%s\n", boxIndent+"  ", line)
		return
	}
	b.notes[r.i] = append(b.notes[r.i], line)
	b.drawLocked()
}

func (b *board) done(i int, status string) { b.update(i, stateDone, status) }

func (b *board) fail(i int, status string) { b.update(i, stateFailed, status) }

func (b *board) update(i int, state prState, status string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.states[i] == state && b.statuses[i] == status {
		return
	}
	b.states[i] = state
	b.statuses[i] = status
	if b.tty == nil {
		if state != statePending {
			fmt.Fprintf(b.out, "%s %s  %s\n", icon(state), b.labels[i], status)
		}
		return
	}
	b.drawLocked()
}

func icon(state prState) string {
	switch state {
	case stateDone:
		return "✔"
	case stateFailed:
		return "✖"
	case statePending:
		return "○"
	}
	return "⟳"
}

func coloredIcon(state prState) string {
	color := ansiCyan
	switch state {
	case stateDone:
		color = ansiGreen
	case stateFailed:
		color = ansiRed
	case statePending:
		color = ansiGrey
	}
	return color + icon(state) + ansiReset
}

func (b *board) drawLocked() {
	if b.tty == nil {
		return
	}
	if b.rendered > 0 {
		fmt.Fprintf(b.tty, "\x1b[%dA\x1b[J", b.rendered)
		b.rendered = 0
	}
	termWidth, _, err := term.GetSize(int(b.tty.Fd()))
	if err != nil || termWidth < 2 {
		termWidth = 0
	}

	var sb strings.Builder
	emit := func(line string) {
		sb.WriteString(line + "\n")
		b.rendered++
	}
	for i, l := range b.labels {
		line := []rune(l + "  " + b.statuses[i])
		if termWidth > 0 && len(line) > termWidth-3 {
			line = line[:termWidth-3]
		}
		emit(coloredIcon(b.states[i]) + " " + string(line))
		for _, boxed := range boxLines(b.notes[i], termWidth) {
			emit(ansiGrey + boxed + ansiReset)
		}
	}
	fmt.Fprint(b.tty, sb.String())
}

// boxLines frames lines in a box drawn under a pull request's line; it is empty without lines.
func boxLines(lines []string, termWidth int) []string {
	if len(lines) == 0 {
		return nil
	}
	maxText := boxMaxText
	if termWidth > 0 {
		maxText = min(maxText, termWidth-len(boxIndent)-4)
	}
	maxText = max(maxText, boxMinText)

	text := make([][]rune, len(lines))
	width := boxMinText
	for i, line := range lines {
		r := []rune(strings.ReplaceAll(ansiEscape.ReplaceAllString(line, ""), "\t", "    "))
		if len(r) > maxText {
			r = r[:maxText]
		}
		text[i] = r
		width = max(width, len(r))
	}

	const title = "─ details "
	result := []string{boxIndent + "┌" + title + strings.Repeat("─", width+2-len([]rune(title))) + "┐"}
	for _, r := range text {
		result = append(result, boxIndent+"│ "+string(r)+strings.Repeat(" ", width-len(r))+" │")
	}
	return append(result, boxIndent+"└"+strings.Repeat("─", width+2)+"┘")
}
