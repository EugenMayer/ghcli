package github

import (
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
)

// Repo identifies a GitHub repository.
type Repo struct {
	Owner string
	Name  string
}

func (r Repo) String() string { return r.Owner + "/" + r.Name }

// ParseRepo parses "owner/name".
func ParseRepo(s string) (Repo, error) {
	parts := strings.Split(strings.TrimSuffix(s, ".git"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return Repo{}, fmt.Errorf("invalid repository %q, expected owner/name", s)
	}
	return Repo{Owner: parts[0], Name: parts[1]}, nil
}

// scp-like syntax: [user@]host:owner/name(.git)
var scpRe = regexp.MustCompile(`^(?:[^@/]+@)?[^:/]+:(.+)$`)

// ParseRemoteURL extracts the repository from a git remote URL. The host is not checked, so
// SSH aliases like "github:owner/name" are taken as GitHub organization "owner", repo "name".
func ParseRemoteURL(remote string) (Repo, error) {
	remote = strings.TrimSpace(remote)

	var path string
	if strings.Contains(remote, "://") {
		u, err := url.Parse(remote)
		if err != nil {
			return Repo{}, fmt.Errorf("parsing remote %q: %w", remote, err)
		}
		path = u.Path
	} else if m := scpRe.FindStringSubmatch(remote); m != nil {
		path = m[1]
	} else {
		return Repo{}, fmt.Errorf("unsupported remote %q (use --repo owner/name)", remote)
	}

	repo, err := ParseRepo(strings.Trim(path, "/"))
	if err != nil {
		return Repo{}, fmt.Errorf("remote %q: %w", remote, err)
	}
	return repo, nil
}

// ResolveRepo returns the repo given by flag, or detects it from the 'origin' remote of the cwd.
func ResolveRepo(flag string) (Repo, error) {
	if flag != "" {
		return ParseRepo(flag)
	}

	out, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err != nil {
		return Repo{}, fmt.Errorf("could not detect repository from git remote 'origin' (use --repo owner/name): %w", err)
	}
	return ParseRemoteURL(string(out))
}
