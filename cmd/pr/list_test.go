package pr

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/google/go-github/v92/github"
	"github.com/mattn/go-runewidth"

	gh "github.com/EugenMayer/ghcli/internal/github"
)

func TestRenderPRTable(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	prs := []*github.PullRequest{
		{
			Number: github.Ptr(7), Title: github.Ptr("Add VPC module"), Draft: github.Ptr(false),
			User:      &github.User{Login: github.Ptr("alice")},
			Head:      &github.PullRequestBranch{Ref: github.Ptr("feat/vpc")},
			Base:      &github.PullRequestBranch{Ref: github.Ptr("main")},
			Labels:    []*github.Label{{Name: "done"}, {Name: "infra"}},
			CreatedAt: &github.Timestamp{Time: now.Add(-3 * 24 * time.Hour)},
		},
		{
			Number: github.Ptr(12), Title: github.Ptr("WIP: bump provider"), Draft: github.Ptr(true),
			User:      &github.User{Login: github.Ptr("bob")},
			Head:      &github.PullRequestBranch{Ref: github.Ptr("chore/provider")},
			Base:      &github.PullRequestBranch{Ref: github.Ptr("main")},
			CreatedAt: &github.Timestamp{Time: now.Add(-90 * time.Minute)},
		},
	}

	var buf bytes.Buffer
	if err := renderPRTable(&buf, gh.Repo{Owner: "o", Name: "r"}, prs, now, 0); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	t.Log("\n" + out)

	for _, want := range []string{"Add VPC module", "alice", "feat/vpc → main", "done, infra", "3d",
		"[draft] WIP: bump provider", "1h", "2 open pull requests in o/r"} {
		if !strings.Contains(out, want) {
			t.Errorf("output misses %q", want)
		}
	}
}

func TestRenderPRTableMaxWidth(t *testing.T) {
	now := time.Now()
	prs := []*github.PullRequest{{
		Number:    github.Ptr(1),
		Title:     github.Ptr(strings.Repeat("very long title ", 10)),
		User:      &github.User{Login: github.Ptr("alice")},
		Head:      &github.PullRequestBranch{Ref: github.Ptr("feature/some-quite-long-branch-name")},
		Base:      &github.PullRequestBranch{Ref: github.Ptr("main")},
		CreatedAt: &github.Timestamp{Time: now},
	}}

	var buf bytes.Buffer
	if err := renderPRTable(&buf, gh.Repo{Owner: "o", Name: "r"}, prs, now, 80); err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + buf.String())

	if !strings.Contains(buf.String(), "…") {
		t.Error("expected the title to be truncated, not squeezed")
	}
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if w := runewidth.StringWidth(line); w > 80 {
			t.Errorf("line is %d characters wide, want <= 80: %s", w, line)
		}
	}
}

func TestAge(t *testing.T) {
	cases := map[time.Duration]string{
		5 * time.Minute:          "5m",
		5 * time.Hour:            "5h",
		3 * 24 * time.Hour:       "3d",
		30 * 24 * time.Hour:      "4w",
		2 * 365 * 24 * time.Hour: "2y",
	}
	for d, want := range cases {
		if got := age(d); got != want {
			t.Errorf("age(%s) = %s, want %s", d, got, want)
		}
	}
}
