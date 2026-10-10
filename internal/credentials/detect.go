package credentials

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"

	"github.com/EugenMayer/ghcli/internal/config"
	"github.com/EugenMayer/ghcli/pkg/keychain"
)

// candidates are tried during auto-detection. The entry names follow the conventions documented
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

// Resolve loads the config and returns the token like Token. If no token is configured anywhere,
// it auto-detects a keyring_command, saves it to the config file and tells the user via out.
// A plain-text token in the config gets a recommendation to move it to the OS keyring.
func Resolve(ctx context.Context, configPath string, out io.Writer) (string, Source, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return "", "", err
	}

	s, err := resolve(ctx, cfg, true)
	if err != nil {
		return "", "", err
	}

	switch s.Source {
	case keychain.SourcePlain:
		fmt.Fprintln(out, "ℹ "+Recommendation(ctx, SourceConfig))
	case keychain.SourceDetected:
		path, writeErr := config.SetKeyringCommand(configPath, s.Command)
		if writeErr != nil {
			fmt.Fprintf(out, "ℹ found a GitHub token via `%s`, but could not save it to the config: %v\n", s.Command, writeErr)
		} else {
			fmt.Fprintf(out, "ℹ no token was configured, but one was found via `%s`.\n"+
				"  Saved it as os.keyring_command in %s, it will be used from now on.\n", s.Command, path)
		}
	}
	return s.Value, source(s), nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
