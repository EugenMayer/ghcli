package credentials

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/EugenMayer/ghcli/internal/config"
)

// fakeEnv isolates HOME and PATH and installs fake binaries: name -> shell script body.
func fakeEnv(t *testing.T, binaries map[string]string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts")
	}
	mockKeyring(t)
	t.Setenv(EnvVar, "")

	home := t.TempDir()
	bin := filepath.Join(home, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"sh", "head"} {
		for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
			if _, err := os.Stat(filepath.Join(dir, tool)); err == nil {
				if err := os.Symlink(filepath.Join(dir, tool), filepath.Join(bin, tool)); err != nil {
					t.Fatal(err)
				}
				break
			}
		}
	}
	for name, body := range binaries {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", bin)
	t.Setenv("PASSWORD_STORE_DIR", "")
	t.Setenv("BW_SESSION", "")
	return home
}

func TestResolveDetectsAndSaves(t *testing.T) {
	home := fakeEnv(t, map[string]string{
		"gopass":      "echo 'not a token'", // rejected: no GitHub token format
		"secret-tool": "echo github_pat_fromKeyring_1",
	})
	configPath := filepath.Join(home, ".ghcli", "config.yml")

	var out bytes.Buffer
	token, source, err := Resolve(context.Background(), "", &out)
	if err != nil || token != "github_pat_fromKeyring_1" || source != SourceCommand {
		t.Fatalf("Resolve() = %q, %q, %v", token, source, err)
	}
	if !strings.Contains(out.String(), "secret-tool lookup service github-pat account ghcli") ||
		!strings.Contains(out.String(), configPath) {
		t.Errorf("unexpected message: %s", out.String())
	}

	cfg, err := config.Load(configPath)
	if err != nil || cfg.OS.KeyringCommand != "secret-tool lookup service github-pat account ghcli" {
		t.Fatalf("saved config = %+v, %v", cfg, err)
	}

	// second run uses the saved command and prints nothing
	out.Reset()
	if _, source, err := Resolve(context.Background(), "", &out); err != nil || source != SourceCommand || out.Len() != 0 {
		t.Errorf("second Resolve() = %q, %v, output %q", source, err, out.String())
	}
}

func TestResolveDetectsPassOnlyWithEntry(t *testing.T) {
	home := fakeEnv(t, map[string]string{"pass": "printf 'ghp_fromPass123\\nlogin: me\\n'"})

	if _, _, err := Resolve(context.Background(), "", &bytes.Buffer{}); err == nil {
		t.Fatal("pass without github/ghcli entry must not be detected")
	}

	if err := os.MkdirAll(filepath.Join(home, ".password-store", "github"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".password-store", "github", "ghcli.gpg"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	token, _, err := Resolve(context.Background(), "", &bytes.Buffer{})
	if err != nil || token != "ghp_fromPass123" {
		t.Errorf("Resolve() = %q, %v", token, err)
	}
}

func TestResolveNothingFound(t *testing.T) {
	home := fakeEnv(t, nil)

	if _, _, err := Resolve(context.Background(), "", &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "ghcli auth login") {
		t.Errorf("expected 'no token' error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".ghcli", "config.yml")); err == nil {
		t.Error("config must not be written when nothing was detected")
	}
}

func TestResolvePlainTextRecommendsKeyring(t *testing.T) {
	home := fakeEnv(t, map[string]string{"secret-tool": "echo github_pat_detected_1"})
	configPath := filepath.Join(home, "config.yml")
	if err := os.WriteFile(configPath, []byte("github:\n  pat: ghp_plain\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	token, source, err := Resolve(context.Background(), configPath, &out)
	if err != nil || token != "ghp_plain" || source != SourceConfig {
		t.Errorf("Resolve() = %q, %q, %v", token, source, err)
	}
	if got := out.String(); strings.Contains(got, "found via") ||
		!strings.Contains(got, "'ghcli auth login' to store it in Mock Keyring") {
		t.Errorf("unexpected output %q", got)
	}
}
