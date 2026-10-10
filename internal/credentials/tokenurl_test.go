package credentials

import (
	"net/url"
	"strings"
	"testing"
)

func TestTokenURL(t *testing.T) {
	u, err := url.Parse(TokenURL("geek-labs", "90", false))
	if err != nil {
		t.Fatal(err)
	}
	if u.Host != "github.com" || u.Path != "/settings/personal-access-tokens/new" {
		t.Errorf("url = %s", u)
	}
	want := map[string]string{
		"name": "ghcli geek-labs", "target_name": "geek-labs", "expires_in": "90",
		"pull_requests": "write", "contents": "write", "issues": "write",
	}
	q := u.Query()
	for k, v := range want {
		if got := q.Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if q.Has("workflows") {
		t.Error("workflows must only be requested with --workflows")
	}

	q, _ = url.ParseQuery(strings.SplitN(TokenURL(DefaultAlias, "none", true), "?", 2)[1])
	if q.Has("target_name") || q.Get("workflows") != "write" || q.Get("expires_in") != "none" {
		t.Errorf("default alias with workflows: %v", q)
	}

	if q, _ := url.ParseQuery(strings.SplitN(TokenURL(strings.Repeat("x", 60), "90", false), "?", 2)[1]); len(q.Get("name")) > 40 {
		t.Errorf("name longer than 40 characters: %q", q.Get("name"))
	}
}

func TestValidateExpiry(t *testing.T) {
	for _, ok := range []string{"1", "90", "366", "none"} {
		if err := ValidateExpiry(ok); err != nil {
			t.Errorf("ValidateExpiry(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"0", "367", "-1", "never", ""} {
		if err := ValidateExpiry(bad); err == nil {
			t.Errorf("ValidateExpiry(%q): expected error", bad)
		}
	}
}
