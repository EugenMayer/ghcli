package github

import (
	"fmt"

	"github.com/google/go-github/v92/github"
)

// NewClient returns a GitHub API client authenticated with the given PAT.
func NewClient(pat string) (*github.Client, error) {
	client, err := github.NewClient(github.WithAuthToken(pat), github.WithUserAgent("ghcli"))
	if err != nil {
		return nil, fmt.Errorf("creating GitHub client: %w", err)
	}
	return client, nil
}
