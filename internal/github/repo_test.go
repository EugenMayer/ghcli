package github

import "testing"

func TestParseRemoteURL(t *testing.T) {
	cases := map[string]Repo{
		"https://github.com/EugenMayer/ghcli":               {"EugenMayer", "ghcli"},
		"https://github.com/EugenMayer/ghcli.git\n":         {"EugenMayer", "ghcli"},
		"git@github.com:EugenMayer/ghcli.git":               {"EugenMayer", "ghcli"},
		"ssh://git@github.com/EugenMayer/ghcli.git":         {"EugenMayer", "ghcli"},
		"ssh://git@ssh.github.com:443/EugenMayer/ghcli":     {"EugenMayer", "ghcli"},
		"https://x:token@github.com/EugenMayer/ghcli":       {"EugenMayer", "ghcli"},
		"github:geek-labs/terraform-modules\n":              {"geek-labs", "terraform-modules"},
		"git@gh-work:geek-labs/terraform-modules.git":       {"geek-labs", "terraform-modules"},
		"ssh://git@gh-work/geek-labs/terraform-modules.git": {"geek-labs", "terraform-modules"},
	}
	for in, want := range cases {
		got, err := ParseRemoteURL(in)
		if err != nil || got != want {
			t.Errorf("ParseRemoteURL(%q) = %v, %v; want %v", in, got, err, want)
		}
	}

	for _, in := range []string{"github:foo", "github:a/b/c", "/some/local/path"} {
		if _, err := ParseRemoteURL(in); err == nil {
			t.Errorf("ParseRemoteURL(%q): expected error", in)
		}
	}
}

func TestParseRepo(t *testing.T) {
	if got, err := ParseRepo("EugenMayer/ghcli"); err != nil || got != (Repo{"EugenMayer", "ghcli"}) {
		t.Errorf("unexpected result %v, %v", got, err)
	}
	for _, in := range []string{"", "foo", "foo/", "/bar", "a/b/c"} {
		if _, err := ParseRepo(in); err == nil {
			t.Errorf("ParseRepo(%q): expected error", in)
		}
	}
}
