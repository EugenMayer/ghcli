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
	keySpace = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
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

func (h *harness) checked() []int {
	var n []int
	for _, pr := range h.m.prs {
		if h.m.checked[pr.GetNumber()] {
			n = append(n, pr.GetNumber())
		}
	}
	return n
}

func TestLabelCheckThenApplySequentially(t *testing.T) {
	h := newHarness(t, nil, 1, 2, 3, 4)

	h.press(keySpace, keyDown, keyDown, keySpace) // check #1 and #3
	if got := h.checked(); !slices.Equal(got, []int{1, 3}) || len(h.pending) != 0 {
		t.Fatalf("checked = %v, pending %d; want [1 3], nothing applied yet", got, len(h.pending))
	}
	view := h.m.View().Content
	if !strings.Contains(view, "[x] #1") || !strings.Contains(view, "[ ] #2") {
		t.Errorf("view misses checkboxes:\n%s", view)
	}

	h.press(keySpace) // uncheck #3
	h.press(keySpace) // and check it again
	h.press(keyEnter)
	if len(h.pending) != 1 || len(h.m.queue) != 1 {
		t.Fatalf("pending %d, queued %d; want one at a time", len(h.pending), len(h.m.queue))
	}
	h.press(keyDown, keySpace) // ignored while labeling
	h.finish()
	if h.quit || !strings.Contains(h.m.View().Content, "✔ #1") {
		t.Errorf("after #1: quit %v, view:\n%s", h.quit, h.m.View().Content)
	}
	h.finish()
	if !slices.Equal(h.applied, []int{1, 3}) || !h.quit || !h.m.finished {
		t.Errorf("applied %v, quit %v, finished %v", h.applied, h.quit, h.m.finished)
	}
}

func TestLabelFailureReported(t *testing.T) {
	h := newHarness(t, map[int]bool{2: true}, 1, 2, 3)

	h.press(keyCtrlA, keyEnter)
	h.finish()
	h.finish()
	h.finish()
	if !slices.Equal(h.applied, []int{1, 2, 3}) || !h.quit {
		t.Fatalf("applied %v, quit %v", h.applied, h.quit)
	}
	var out bytes.Buffer
	if err := h.m.summary(&out); err == nil || !strings.Contains(err.Error(), "[2]") || strings.Count(out.String(), "✔") != 2 {
		t.Errorf("summary = %q, error = %v", out.String(), err)
	}
}

func TestLabelCtrlAToggles(t *testing.T) {
	h := newHarness(t, nil, 1, 2, 3)

	h.press(keySpace, keyCtrlA)
	if got := h.checked(); !slices.Equal(got, []int{1, 2, 3}) {
		t.Fatalf("ctrl+a with some checked: %v, want all", got)
	}
	h.press(keyCtrlA)
	if got := h.checked(); len(got) != 0 {
		t.Fatalf("ctrl+a with all checked: %v, want none", got)
	}
}

func TestLabelEnterWithNothingChecked(t *testing.T) {
	h := newHarness(t, nil, 1, 2)

	h.press(keyEnter)
	if !h.quit || !h.m.finished || len(h.applied) != 0 {
		t.Errorf("quit %v, finished %v, applied %v", h.quit, h.m.finished, h.applied)
	}
}

func TestLabelQuitLabelsNothing(t *testing.T) {
	h := newHarness(t, nil, 1, 2)

	h.press(keySpace, keyQ)
	if !h.quit || h.m.finished || len(h.applied) != 0 {
		t.Errorf("quit %v, finished %v, applied %v", h.quit, h.m.finished, h.applied)
	}
	var out bytes.Buffer
	if err := h.m.summary(&out); err != nil || !strings.Contains(out.String(), "no pull requests were labeled") {
		t.Errorf("summary = %q, %v", out.String(), err)
	}
}

func TestLabelCtrlCAborts(t *testing.T) {
	h := newHarness(t, nil, 1, 2, 3)

	h.press(keyCtrlA, keyEnter, keyCtrlC)
	if !h.quit || !h.m.aborted || h.m.finished {
		t.Fatalf("quit %v, aborted %v, finished %v", h.quit, h.m.aborted, h.m.finished)
	}
	var out bytes.Buffer
	_ = h.m.summary(&out)
	if !strings.Contains(out.String(), "aborted, 3 pull requests were not labeled") {
		t.Errorf("summary = %q", out.String())
	}
}

func TestLabelKeyNames(t *testing.T) {
	for k, want := range map[tea.KeyPressMsg]string{keyEnter: "enter", keySpace: "space", keyCtrlA: "ctrl+a", keyCtrlC: "ctrl+c", keyQ: "q", keyDown: "down"} {
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
		for _, k := range []string{" ", "\x1b[B", "\x1b[B", " ", "\r"} { // space, down, down, space, enter
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
		t.Fatal("program did not quit after the checked PRs were labeled")
	}
	if err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(applied, []int{1, 3}) {
		t.Errorf("applied = %v", applied)
	}
	if fm := final.(*labelModel); len(fm.labeled) != 2 || !fm.finished {
		t.Errorf("labeled = %d, finished %v", len(fm.labeled), fm.finished)
	}
	for _, want := range []string{`Label pull requests of o/r with "done"`, "[x] #1 pr 1", "labeled"} {
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
