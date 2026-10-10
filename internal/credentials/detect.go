package credentials

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"

	"github.com/EugenMayer/ghcli/pkg/keychain"
)

// candidates are tried during auto-detection of the default token. The entry names follow the conventions documented
// in README.configuration.md, which predate pkg/keychain's DefaultCandidates.
var candidates = []keychain.Candidate{
	{Binary: "secret-tool", Command: "secret-tool lookup service github-pat account ghcli"},
	{Binary: "kwallet-query", Command: "kwallet-query -f ghcli -r github-pat kdewallet"},
	{Binary: "security", Command: "security find-generic-password -s ghcli-github-pat -w",
		Ready: func(string, string, string) bool { return runtime.GOOS == "darwin" }},
	{Binary: "pass", Command: "pass show github/ghcli | head -n 1",
		Ready: func(home, _, _ string) bool {
			store := os.Getenv("PASSWORD_STORE_DIR")
			if store == "" {
				store = filepath.Join(home, ".password-store")
			}
			return exists(filepath.Join(store, "github", "ghcli.gpg"))
		}},
	{Binary: "gopass", Command: "gopass show -o github/ghcli"},
	{Binary: "rbw", Command: "rbw get ghcli"},
	{Binary: "bw", Command: "bw get password ghcli",
		Ready: func(string, string, string) bool { return os.Getenv("BW_SESSION") != "" }},
	{Binary: "op", Command: `op read "op://Private/ghcli/credential"`},
	{Binary: "gpg", Command: "gpg --quiet --decrypt ~/.ghcli/pat.gpg",
		Ready: func(home, _, _ string) bool { return exists(filepath.Join(home, ".ghcli", "pat.gpg")) }},
	{Binary: "age", Command: "age -d -i ~/.config/age/key.txt ~/.ghcli/pat.age",
		Ready: func(home, _, _ string) bool {
			return exists(filepath.Join(home, ".ghcli", "pat.age")) && exists(filepath.Join(home, ".config", "age", "key.txt"))
		}},
	{Binary: "sops", Command: `sops -d --extract '["github_pat"]' ~/.ghcli/secrets.enc.yml`,
		Ready: func(home, _, _ string) bool { return exists(filepath.Join(home, ".ghcli", "secrets.enc.yml")) }},
}

// tokenRe matches GitHub token formats, so unrelated command output is never taken as a token.
var tokenRe = regexp.MustCompile(`^(github_pat_|ghp_|gho_|ghu_|ghs_|ghr_)[A-Za-z0-9_]+$`)

func isGitHubToken(s string) bool { return tokenRe.MatchString(s) }

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
