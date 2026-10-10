package pr

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/go-github/v92/github"

	gh "github.com/EugenMayer/ghcli/internal/github"
)

var (
	keyEnter = tea.KeyPressMsg{Code: tea.KeyEnter}
	keyDown  = tea.KeyPressMsg{Code: tea.KeyDown}
	keyCtrlA = tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl}
	keyCtrlC = tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	keyQ     = tea.KeyPressMsg{Code: 'q', Text: "q"}
)

func testPRs(numbers ...int) []*github.PullRequest {
	prs := make([]*github.PullRequest, 0, len(numbers))
	for _, n := range numbers {
		prs = append(prs, &github.PullRequest{Number: github.Ptr(n), Title: github.Ptr(fmt.Sprintf("pr %d", n))})
	}
	return prs
}

// harness runs the model's commands by hand, so tests control when a label request finishes.
type harness struct {
	t       *testing.T
	m       *labelModel
	pending []tea.Cmd // label requests started but not finished
	quit    bool
	applied []int
}

func newHarness(t *testing.T, failing map[int]bool, numbers ...int) *harness {
	h := &harness{t: t}
	apply := func(_ context.Context, n int) error {
		h.applied = append(h.applied, n)
		if failing[n] {
			return errors.New("403 forbidden")
		}
		return nil
	}
	h.m = newLabelModel(context.Background(), gh.Repo{Owner: "o", Name: "r"}, "done", testPRs(numbers...), apply)
	return h
}

func (h *harness) handle(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	// tea.Quit is the only command that is not a label request
	if msg := cmd(); msg != nil {
		if _, ok := msg.(tea.QuitMsg); ok {
			h.quit = true
			return
		}
		h.pending = append(h.pending, func() tea.Msg { return msg })
	}
}

func (h *harness) press(keys ...tea.KeyPressMsg) {
	for _, k := range keys {
		_, cmd := h.m.Update(k)
		h.handle(cmd)
	}
}

// finish delivers the result of the oldest pending label request.
func (h *harness) finish() {
	h.t.Helper()
	if len(h.pending) == 0 {
		h.t.Fatal("no pending label request")
	}
	cmd := h.pending[0]
	h.pending = h.pending[1:]
	_, next := h.m.Update(cmd())
	h.handle(next)
}

func (h *harness) remaining() []int {
	var n []int
	for _, pr := range h.m.remaining {
		n = append(n, pr.GetNumber())
	}
	return n
}

func TestLabelOptimisticAndSequential(t *testing.T) {
	h := newHarness(t, nil, 1, 2, 3, 4)

	h.press(keyEnter) // #1: removed at once, request started
	if got := h.remaining(); !slices.Equal(got, []int{2, 3, 4}) {
		t.Fatalf("remaining = %v", got)
	}
	h.press(keyDown, keyEnter) // #3 while #1 is still in flight: queued, not started
	if got := h.remaining(); !slices.Equal(got, []int{2, 4}) || len(h.pending) != 1 || len(h.m.queue) != 1 {
		t.Fatalf("remaining = %v, pending %d, queued %d", got, len(h.pending), len(h.m.queue))
	}

	h.finish() // #1 done, #3 starts
	if len(h.pending) != 1 || !strings.Contains(h.m.View().Content, "✔ #1") {
		t.Errorf("after #1: pending %d, view:\n%s", len(h.pending), h.m.View().Content)
	}
	h.finish()
	if !slices.Equal(h.applied, []int{1, 3}) || h.quit {
		t.Errorf("applied %v, quit %v", h.applied, h.quit)
	}
}

func TestLabelFailurePutsBack(t *testing.T) {
	h := newHarness(t, map[int]bool{2: true}, 1, 2, 3)

	h.press(keyDown, keyEnter) // #2
	h.finish()
	if got := h.remaining(); !slices.Equal(got, []int{1, 2, 3}) {
		t.Fatalf("failed PR not put back in place: %v", got)
	}
	view := h.m.View().Content
	if !strings.Contains(view, "✘ #2") || !strings.Contains(view, "failed: 403 forbidden") {
		t.Errorf("view misses failure:\n%s", view)
	}

	var out bytes.Buffer
	if err := h.m.summary(&out); err == nil || !strings.Contains(err.Error(), "[2]") {
		t.Errorf("summary error = %v", err)
	}
}

func TestLabelCtrlAAndAutoQuit(t *testing.T) {
	h := newHarness(t, nil, 1, 2, 3)

	h.press(keyCtrlA)
	if len(h.m.remaining) != 0 || len(h.pending) != 1 {
		t.Fatalf("remaining %d, pending %d", len(h.m.remaining), len(h.pending))
	}
	h.finish()
	h.finish()
	h.finish()
	if !slices.Equal(h.applied, []int{1, 2, 3}) || !h.quit {
		t.Errorf("applied %v, quit %v; want all labeled and quit", h.applied, h.quit)
	}

	var out bytes.Buffer
	if err := h.m.summary(&out); err != nil || strings.Count(out.String(), "✔") != 3 {
		t.Errorf("summary = %q, %v", out.String(), err)
	}
}

func TestLabelQuitWaitsForPending(t *testing.T) {
	h := newHarness(t, nil, 1, 2, 3)

	h.press(keyEnter, keyEnter, keyQ) // #1 in flight, #2 queued, then quit
	if h.quit {
		t.Fatal("quit before pending labels were applied")
	}
	h.press(keyEnter) // ignored while quitting
	h.finish()
	h.finish()
	if !slices.Equal(h.applied, []int{1, 2}) || !h.quit {
		t.Errorf("applied %v, quit %v", h.applied, h.quit)
	}
}

func TestLabelDoubleCtrlCAborts(t *testing.T) {
	h := newHarness(t, nil, 1, 2, 3)

	h.press(keyEnter, keyEnter, keyCtrlC)
	if h.quit {
		t.Fatal("first ctrl+c must wait for pending labels")
	}
	h.press(keyCtrlC)
	if !h.quit || !h.m.aborted {
		t.Fatal("second ctrl+c must abort")
	}
	var out bytes.Buffer
	_ = h.m.summary(&out)
	if !strings.Contains(out.String(), "aborted, 2 pull requests were not labeled") {
		t.Errorf("summary = %q", out.String())
	}
}

func TestLabelKeyNames(t *testing.T) {
	for k, want := range map[tea.KeyPressMsg]string{keyEnter: "enter", keyCtrlA: "ctrl+a", keyCtrlC: "ctrl+c", keyQ: "q", keyDown: "down"} {
		if got := k.String(); got != want {
			t.Errorf("key %+v = %q, want %q", k, got, want)
		}
	}
}

func TestLabelProgram(t *testing.T) {
	var mu sync.Mutex
	var applied []int
	apply := func(_ context.Context, n int) error {
		time.Sleep(20 * time.Millisecond) // like an API call
		mu.Lock()
		defer mu.Unlock()
		applied = append(applied, n)
		return nil
	}
	m := newLabelModel(context.Background(), gh.Repo{Owner: "o", Name: "r"}, "done", testPRs(1, 2, 3, 4), apply)

	in, keys := io.Pipe()
	var out bytes.Buffer
	p := tea.NewProgram(m, tea.WithInput(in), tea.WithOutput(&out), tea.WithWindowSize(100, 30))
	go func() {
		for _, k := range []string{"\r", "\x1b[B", "\r", "\x01"} { // enter, down, enter, ctrl+a
			time.Sleep(50 * time.Millisecond)
			_, _ = keys.Write([]byte(k))
		}
	}()

	done := make(chan struct{})
	var final tea.Model
	var err error
	go func() { final, err = p.Run(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		p.Kill()
		t.Fatal("program did not quit after all PRs were labeled")
	}
	if err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	// enter on #1, then down skips #2 and enter labels #3, ctrl+a labels #2 and #4
	if !slices.Equal(applied, []int{1, 3, 2, 4}) {
		t.Errorf("applied = %v", applied)
	}
	if len(final.(*labelModel).labeled) != 4 {
		t.Errorf("labeled = %d", len(final.(*labelModel).labeled))
	}
	for _, want := range []string{`Label pull requests of o/r with "done"`, "#2 pr 2", "labeled"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output misses %q", want)
		}
	}
}

func TestLabelsAPIError(t *testing.T) {
	forbidden := func(accepted string) error {
		resp := &http.Response{StatusCode: http.StatusForbidden, Header: http.Header{}, Request: &http.Request{Method: "POST", URL: &url.URL{Path: "/x"}}}
		if accepted != "" {
			resp.Header.Set("X-Accepted-GitHub-Permissions", accepted)
		}
		return &github.ErrorResponse{Response: resp, Message: "Resource not accessible by personal access token"}
	}

	if got := labelsAPIError(forbidden("pull_requests=write")).Error(); got != "403 Resource not accessible by personal access token (the token needs pull_requests=write; set \"Pull requests\" to \"Read and write\")" {
		t.Errorf("with header: %q", got)
	}
	if got := labelsAPIError(forbidden("")).Error(); !strings.Contains(got, "403 Resource not accessible") || !strings.Contains(got, "the token needs") {
		t.Errorf("without header: %q", got)
	}
	if labelsAPIError(nil) != nil {
		t.Error("nil error must stay nil")
	}
}

func TestLabelViewFitsWidth(t *testing.T) {
	prs := testPRs(1)
	m := newLabelModel(context.Background(), gh.Repo{Owner: "o", Name: "r"}, "done", prs, nil)
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	m.failed[1] = errors.New(strings.Repeat("long error ", 10))
	m.message(strings.Repeat("word ", 30))

	for _, line := range strings.Split(m.View().Content, "\n") {
		if w := ansi.StringWidth(line); w > 40 {
			t.Errorf("line is %d wide, want <= 40: %q", w, line)
		}
	}
	if !strings.Contains(m.View().Content, "word word") {
		t.Error("message lost instead of wrapped")
	}
}

func TestDescribeAcceptedPermissions(t *testing.T) {
	cases := map[string]string{
		"contents=read":                                               "contents=read",
		"pull_requests=write,contents=read":                           "pull_requests=write and contents=read",
		"issues=write; pull_requests=write":                           "one of: issues=write, or pull_requests=write",
		"pull_requests=read,contents=read; issues=read,contents=read": "one of: pull_requests=read and contents=read, or issues=read and contents=read",
	}
	for in, want := range cases {
		if got := describeAcceptedPermissions(in); got != want {
			t.Errorf("describeAcceptedPermissions(%q) = %q, want %q", in, got, want)
		}
	}
}
