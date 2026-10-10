package credentials

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/EugenMayer/ghcli/internal/config"
)

// fakeEnv isolates HOME, PATH and the keyring and installs fake binaries: name -> shell script.
func fakeEnv(t *testing.T, binaries map[string]string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts")
	}
	home := mockKeyring(t)

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

	tok, out, err := resolve(t, Request{Owner: "geek-labs"})
	if err != nil || tok.Value != "github_pat_fromKeyring_1" || tok.Source != SourceCommand || tok.Alias != "" {
		t.Fatalf("Resolve() = %+v, %v", tok, err)
	}
	if !strings.Contains(out, "secret-tool lookup service github-pat account ghcli") || !strings.Contains(out, configPath) {
		t.Errorf("unexpected message: %s", out)
	}

	cfg, err := config.Load(configPath)
	if err != nil || cfg.OS.KeyringCommand != "secret-tool lookup service github-pat account ghcli" {
		t.Fatalf("saved config = %+v, %v", cfg, err)
	}

	// second run uses the saved command and prints nothing
	if tok, out, err := resolve(t, Request{Owner: "geek-labs"}); err != nil || tok.Source != SourceCommand || out != "" {
		t.Errorf("second Resolve() = %+v, %q, %v", tok, out, err)
	}
}

func TestResolveDetectsPassOnlyWithEntry(t *testing.T) {
	home := fakeEnv(t, map[string]string{"pass": "printf 'ghp_fromPass123\\nlogin: me\\n'"})

	if _, _, err := resolve(t, Request{}); err == nil {
		t.Fatal("pass without github/ghcli entry must not be detected")
	}

	if err := os.MkdirAll(filepath.Join(home, ".password-store", "github"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".password-store", "github", "ghcli.gpg"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if tok, _, err := resolve(t, Request{}); err != nil || tok.Value != "ghp_fromPass123" {
		t.Errorf("Resolve() = %+v, %v", tok, err)
	}
}

func TestNoDetectionOnceATokenIsStored(t *testing.T) {
	home := fakeEnv(t, map[string]string{"secret-tool": "echo github_pat_detected_1"})
	_ = Store("geek-labs", "ghp_geeklabs")

	// "geek-labs" is the only token, so it's used; nothing is detected or written
	tok, out, err := resolve(t, Request{Owner: "other-org"})
	if err != nil || tok.Value != "ghp_geeklabs" || strings.Contains(out, "found via") {
		t.Errorf("Resolve() = %+v, %q, %v", tok, out, err)
	}
	if _, err := os.Stat(filepath.Join(home, ".ghcli", "config.yml")); err == nil {
		t.Error("config must not be written")
	}
}
