package credentials

import (
	"fmt"
	"net/url"
	"strconv"
)

// Permission is a fine-grained token permission, named like GitHub's template URL parameters.
type Permission struct {
	Name   string // e.g. "pull_requests"
	Access string // "read" or "write"; write includes read
	For    string // the commands needing it
}

// Permissions are the repository permissions ghcli's commands need. Metadata (read) is added by
// GitHub automatically.
var Permissions = []Permission{
	{Name: "pull_requests", Access: "write", For: "pr list (read), pr label, pr merge"},
	{Name: "contents", Access: "write", For: "pr merge"},
}

// WorkflowsPermission lets pr merge merge pull requests that change .github/workflows/.
var WorkflowsPermission = Permission{Name: "workflows", Access: "write", For: "pr merge of PRs changing .github/workflows/"}

// DefaultExpiry is the default token lifetime in days for TokenURL.
const DefaultExpiry = "90"

// ValidateExpiry checks a token lifetime for TokenURL: days between 1 and 366, or "none".
func ValidateExpiry(expires string) error {
	if expires == "none" {
		return nil
	}
	if days, err := strconv.Atoi(expires); err != nil || days < 1 || days > 366 {
		return fmt.Errorf("invalid expiry %q: use days between 1 and 366, or none", expires)
	}
	return nil
}

// TokenURL returns a link to GitHub's form for a new fine-grained token, pre-filled with name,
// resource owner (the alias, unless it is the default alias), expiry and ghcli's permissions.
// GitHub shows the form for confirmation; the repository selection has to be made there.
// See https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens
func TokenURL(alias, expires string, workflows bool) string {
	name := "ghcli " + alias
	if len(name) > 40 { // GitHub's limit for token names
		name = name[:40]
	}

	q := url.Values{}
	q.Set("name", name)
	q.Set("expires_in", expires)
	if alias != DefaultAlias {
		q.Set("target_name", alias)
	}

	q.Set("description", "ghcli: pr list, pr label, pr merge")
	for _, p := range Permissions {
		q.Set(p.Name, p.Access)
	}
	if workflows {
		q.Set(WorkflowsPermission.Name, WorkflowsPermission.Access)
	}

	return "https://github.com/settings/personal-access-tokens/new?" + q.Encode()
}
