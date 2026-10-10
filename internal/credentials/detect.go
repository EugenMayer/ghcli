package credentials

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"time"

	"github.com/EugenMayer/ghcli/internal/config"
)

// candidate is a keyring_command that is tried during auto-detection. The entry names follow the
// conventions documented in README.configuration.md.
type candidate struct {
	binary  string
	command string
	// ready reports whether the candidate is worth running, e.g. because its secret file exists.
	ready func(home string) bool
}

var candidates = []candidate{
	{binary: "secret-tool", command: "secret-tool lookup service github-pat account ghcli"},
	{binary: "kwallet-query", command: "kwallet-query -f ghcli -r github-pat kdewallet"},
	{binary: "security", command: "security find-generic-password -s ghcli-github-pat -w",
		ready: func(string) bool { return runtime.GOOS == "darwin" }},
	{binary: "pass", command: "pass show github/ghcli | head -n 1",
		ready: func(home string) bool {
			store := os.Getenv("PASSWORD_STORE_DIR")
			if store == "" {
				store = filepath.Join(home, ".password-store")
			}
			return exists(filepath.Join(store, "github", "ghcli.gpg"))
		}},
	{binary: "gopass", command: "gopass show -o github/ghcli"},
	{binary: "rbw", command: "rbw get ghcli"},
	{binary: "bw", command: "bw get password ghcli",
		ready: func(string) bool { return os.Getenv("BW_SESSION") != "" }},
	{binary: "op", command: `op read "op://Private/ghcli/credential"`},
	{binary: "gpg", command: "gpg --quiet --decrypt ~/.ghcli/pat.gpg",
		ready: func(home string) bool { return exists(filepath.Join(home, ".ghcli", "pat.gpg")) }},
	{binary: "age", command: "age -d -i ~/.config/age/key.txt ~/.ghcli/pat.age",
		ready: func(home string) bool {
			return exists(filepath.Join(home, ".ghcli", "pat.age")) && exists(filepath.Join(home, ".config", "age", "key.txt"))
		}},
	{binary: "sops", command: `sops -d --extract '["github_pat"]' ~/.ghcli/secrets.enc.yml`,
		ready: func(home string) bool { return exists(filepath.Join(home, ".ghcli", "secrets.enc.yml")) }},
}

// probeTimeout bounds each candidate, as some tools may wait for an unlock prompt.
var probeTimeout = 10 * time.Second

// tokenRe matches GitHub token formats, so unrelated command output is never taken as a token.
var tokenRe = regexp.MustCompile(`^(github_pat_|ghp_|gho_|ghu_|ghs_|ghr_)[A-Za-z0-9_]+$`)

// Detect tries the known secret manager commands whose binary is installed and returns the first
// one that prints a GitHub token.
func Detect(ctx context.Context) (command, token string, ok bool) {
	home, _ := os.UserHomeDir()
	for _, c := range candidates {
		if _, err := exec.LookPath(c.binary); err != nil {
			continue
		}
		if c.ready != nil && !c.ready(home) {
			continue
		}

		probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
		// No stdin: a probe must not block waiting for input in the terminal.
		t, err := runCommand(probeCtx, c.command, nil)
		cancel()
		if err == nil && tokenRe.MatchString(t) {
			return c.command, t, true
		}
	}
	return "", "", false
}

// Resolve loads the config and returns the token like Token. If no token is configured anywhere,
// it auto-detects a keyring_command, saves it to the config file and tells the user via out.
func Resolve(ctx context.Context, configPath string, out io.Writer) (string, Source, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return "", "", err
	}

	token, source, err := Token(cfg)
	if !errors.Is(err, ErrNoToken) {
		if err == nil && source == SourceConfig {
			fmt.Fprintln(out, "ℹ "+Recommendation(ctx, source))
		}
		return token, source, err
	}

	command, token, ok := Detect(ctx)
	if !ok {
		return "", "", err
	}

	path, writeErr := config.SetKeyringCommand(configPath, command)
	if writeErr != nil {
		fmt.Fprintf(out, "ℹ found a GitHub token via `%s`, but could not save it to the config: %v\n", command, writeErr)
		return token, SourceCommand, nil
	}
	fmt.Fprintf(out, "ℹ no token was configured, but one was found via `%s`.\n"+
		"  Saved it as os.keyring_command in %s, it will be used from now on.\n", command, path)
	return token, SourceCommand, nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
