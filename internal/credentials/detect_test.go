package credentials

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/EugenMayer/ghcli/internal/config"
)

// fakeEnv isolates HOME and PATH and installs fake binaries: name -> shell script body.
// sh, head and sleep stay usable, as candidate commands are run through the shell.
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
	for _, tool := range []string{"sh", "head", "sleep"} {
		path, err := lookPathIn(tool, os.Getenv("PATH"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(path, filepath.Join(bin, tool)); err != nil {
			t.Fatal(err)
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

func lookPathIn(name, path string) (string, error) {
	for _, dir := range filepath.SplitList(path) {
		p := filepath.Join(dir, name)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p, nil
		}
	}
	return "", os.ErrNotExist
}

func TestDetect(t *testing.T) {
	home := fakeEnv(t, map[string]string{
		"secret-tool": "exit 1",                                  // installed, but no entry
		"gopass":      "echo 'not a token'",                      // prints something that is no token
		"pass":        "printf 'ghp_fromPass123\\nlogin: me\\n'", // not ready: no store entry yet
		"rbw":         "echo github_pat_fromRbw_123",
	})

	command, token, ok := Detect(context.Background())
	if !ok || command != "rbw get ghcli" || token != "github_pat_fromRbw_123" {
		t.Fatalf("Detect() = %q, %q, %v", command, token, ok)
	}

	// once the pass entry exists, pass comes first
	if err := os.MkdirAll(filepath.Join(home, ".password-store", "github"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".password-store", "github", "ghcli.gpg"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	command, token, ok = Detect(context.Background())
	if !ok || command != "pass show github/ghcli | head -n 1" || token != "ghp_fromPass123" {
		t.Fatalf("Detect() = %q, %q, %v", command, token, ok)
	}
}

func TestDetectTimeout(t *testing.T) {
	fakeEnv(t, map[string]string{"secret-tool": "sleep 5; echo ghp_late"})
	old := probeTimeout
	probeTimeout = 100 * time.Millisecond
	t.Cleanup(func() { probeTimeout = old })

	start := time.Now()
	if _, _, ok := Detect(context.Background()); ok {
		t.Error("expected no detection for a hanging command")
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("Detect took %s, timeout not applied", d)
	}
}

func TestResolveDetectsAndSaves(t *testing.T) {
	home := fakeEnv(t, map[string]string{"secret-tool": "echo github_pat_fromKeyring_1"})
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

func TestResolveNothingFound(t *testing.T) {
	home := fakeEnv(t, nil)

	if _, _, err := Resolve(context.Background(), "", &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "ghcli auth login") {
		t.Errorf("expected 'no token' error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".ghcli", "config.yml")); err == nil {
		t.Error("config must not be written when nothing was detected")
	}
}

func TestResolveConfiguredSourceSkipsDetection(t *testing.T) {
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
	// plain text: no detection, but the keyring is recommended
	if got := out.String(); strings.Contains(got, "found via") ||
		!strings.Contains(got, "'ghcli auth login' to store it in Mock Keyring") {
		t.Errorf("unexpected output %q", got)
	}
}
