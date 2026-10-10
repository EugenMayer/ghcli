package credentials

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/EugenMayer/ghcli/pkg/keychain"
)

// mockKeyring replaces the OS keyring with an in-memory one and isolates env and config.
func mockKeyring(t *testing.T) string {
	t.Helper()
	t.Cleanup(keychain.MockInit())
	old := keyringBackend
	keyringBackend = func(context.Context) (string, error) { return "Mock Keyring", nil }
	t.Cleanup(func() { keyringBackend = old })
	t.Setenv(EnvVar, "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

// writeConfig writes ~/.ghcli/config.yml and returns its path.
func writeConfig(t *testing.T, home, content string) string {
	t.Helper()
	path := filepath.Join(home, ".ghcli", "config.yml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func resolve(t *testing.T, req Request) (Token, string, error) {
	t.Helper()
	var out bytes.Buffer
	tok, err := Resolve(context.Background(), req, &out)
	return tok, out.String(), err
}

func TestSelectAlias(t *testing.T) {
	ask := func([]string) (string, error) { return "picked", nil }
	cases := []struct {
		name        string
		req         Request
		aliases     []string
		want, why   string
		wantErrPart string
	}{
		{"flag wins", Request{Alias: "x", Owner: "geek-labs"}, []string{"geek-labs"}, "x", reasonFlag, ""},
		{"owner match", Request{Owner: "geek-labs"}, []string{"default", "geek-labs"}, "geek-labs", reasonOwner, ""},
		{"owner match ignores case", Request{Owner: "Geek-Labs"}, []string{"default", "geek-labs"}, "geek-labs", reasonOwner, ""},
		{"only token", Request{Owner: "other"}, []string{"geek-labs"}, "geek-labs", reasonOnly, ""},
		{"default fallback", Request{Owner: "other"}, []string{"default", "geek-labs"}, "default", reasonDefault, ""},
		{"ask", Request{Owner: "other", Ask: ask}, []string{"a", "b"}, "picked", reasonAsked, ""},
		{"ambiguous", Request{Owner: "other"}, []string{"a", "b"}, "", "", "choose one with --alias"},
		{"nothing known", Request{Owner: "other"}, nil, DefaultAlias, "", ""},
	}
	for _, c := range cases {
		alias, why, err := selectAlias(c.req, c.aliases)
		if c.wantErrPart != "" {
			if err == nil || !strings.Contains(err.Error(), c.wantErrPart) {
				t.Errorf("%s: err = %v", c.name, err)
			}
			continue
		}
		if err != nil || alias != c.want || why != c.why {
			t.Errorf("%s: selectAlias() = %q, %q, %v; want %q, %q", c.name, alias, why, err, c.want, c.why)
		}
	}
}

func TestResolveByOwner(t *testing.T) {
	mockKeyring(t)
	if err := Store(DefaultAlias, "ghp_default"); err != nil {
		t.Fatal(err)
	}
	if err := Store("geek-labs", "ghp_geeklabs"); err != nil {
		t.Fatal(err)
	}
	// stored at the documented entries
	if got, err := keyring.Get("ghcli.github.geek-labs.pat", "ghcli"); err != nil || got != "ghp_geeklabs" {
		t.Errorf("keyring entry = %q, %v", got, err)
	}

	tok, out, err := resolve(t, Request{Owner: "geek-labs"})
	if err != nil || tok.Value != "ghp_geeklabs" || tok.Alias != "geek-labs" || tok.Source != SourceKeyring || out != "" {
		t.Errorf("owner geek-labs: %+v, %q, %v", tok, out, err)
	}

	tok, out, err = resolve(t, Request{Owner: "other-org"})
	if err != nil || tok.Value != "ghp_default" || !strings.Contains(out, `using the token with alias "default" for other-org`) {
		t.Errorf("owner other-org: %+v, %q, %v", tok, out, err)
	}

	tok, _, err = resolve(t, Request{Owner: "geek-labs", Alias: DefaultAlias})
	if err != nil || tok.Value != "ghp_default" || tok.Reason != reasonFlag {
		t.Errorf("--alias default: %+v, %v", tok, err)
	}

	if _, _, err := resolve(t, Request{Alias: "unknown"}); !errors.Is(err, ErrNoToken) ||
		!strings.Contains(err.Error(), "ghcli auth login --alias unknown") {
		t.Errorf("unknown alias: %v", err)
	}
	if _, _, err := resolve(t, Request{Alias: "bad alias"}); err == nil || !strings.Contains(err.Error(), "invalid alias") {
		t.Errorf("invalid alias: %v", err)
	}
}

func TestResolveEnvAlwaysWins(t *testing.T) {
	mockKeyring(t)
	_ = Store("geek-labs", "ghp_geeklabs")
	t.Setenv(EnvVar, "ghp_env")

	tok, _, err := resolve(t, Request{Owner: "geek-labs", Alias: "geek-labs"})
	if err != nil || tok.Value != "ghp_env" || tok.Source != SourceEnv || tok.Alias != "" {
		t.Errorf("Resolve() = %+v, %v; want GHCLI_PAT regardless of owner and alias", tok, err)
	}
}

func TestResolvePlainTextAlias(t *testing.T) {
	home := mockKeyring(t)
	cfgPath := writeConfig(t, home, "github:\n  geek-labs:\n    pat: ghp_plain\n")

	tok, out, err := resolve(t, Request{ConfigPath: cfgPath, Owner: "geek-labs"})
	if err != nil || tok.Value != "ghp_plain" || tok.Source != SourceConfig || tok.Alias != "geek-labs" {
		t.Fatalf("Resolve() = %+v, %v", tok, err)
	}
	if tok.From() != "github.geek-labs.pat in the config file" {
		t.Errorf("From() = %q", tok.From())
	}
	if !strings.Contains(out, "'ghcli auth login --alias geek-labs' to store it in Mock Keyring, then remove github.geek-labs.pat") {
		t.Errorf("recommendation = %q", out)
	}

	// the keyring wins over plain text of the same alias
	_ = Store("geek-labs", "ghp_keyring")
	if tok, _, _ := resolve(t, Request{ConfigPath: cfgPath, Owner: "geek-labs"}); tok.Value != "ghp_keyring" {
		t.Errorf("keyring should win, got %+v", tok)
	}
}

func TestResolveKeyringCommand(t *testing.T) {
	home := mockKeyring(t)

	cfgPath := writeConfig(t, home, "os:\n  keyring_command: echo ghp_for_every_repo\n")
	tok, _, err := resolve(t, Request{ConfigPath: cfgPath, Owner: "geek-labs"})
	if err != nil || tok.Value != "ghp_for_every_repo" || tok.Alias != "" || tok.Source != SourceCommand {
		t.Errorf("command without {alias}: %+v, %v", tok, err)
	}

	// {alias}: the owner's entry is tried first, then the default
	cfgPath = writeConfig(t, home, "os:\n  keyring_command: '[ {alias} != missing-org ] && echo ghp_{alias}'\n")
	tok, _, err = resolve(t, Request{ConfigPath: cfgPath, Owner: "geek-labs"})
	if err != nil || tok.Value != "ghp_geek-labs" || tok.Alias != "geek-labs" {
		t.Errorf("command with {alias}, owner: %+v, %v", tok, err)
	}
	tok, out, err := resolve(t, Request{ConfigPath: cfgPath, Owner: "missing-org"})
	if err != nil || tok.Value != "ghp_default" || !strings.Contains(out, `alias "default"`) {
		t.Errorf("command with {alias}, fallback: %+v, %q, %v", tok, out, err)
	}

	cfgPath = writeConfig(t, home, "os:\n  keyring_command: exit 3\n")
	if _, _, err := resolve(t, Request{ConfigPath: cfgPath}); err == nil || !strings.HasPrefix(err.Error(), "os.keyring_command:") {
		t.Errorf("failing command: %v", err)
	}
}

func TestStoreDeleteMaintainAliasList(t *testing.T) {
	mockKeyring(t)
	for _, a := range []string{"geek-labs", DefaultAlias, "other"} {
		if err := Store(a, "ghp_"+a); err != nil {
			t.Fatal(err)
		}
	}
	if aliases, _ := Aliases(""); !slices.Equal(aliases, []string{"default", "geek-labs", "other"}) {
		t.Errorf("Aliases() = %v", aliases)
	}

	if deleted, err := Delete("other"); err != nil || !deleted {
		t.Errorf("Delete() = %v, %v", deleted, err)
	}
	if deleted, err := Delete("other"); err != nil || deleted {
		t.Errorf("second Delete() = %v, %v", deleted, err)
	}
	if aliases, _ := Aliases(""); !slices.Equal(aliases, []string{"default", "geek-labs"}) {
		t.Errorf("Aliases() after Delete = %v", aliases)
	}

	deleted, err := DeleteAll()
	if err != nil || !slices.Equal(deleted, []string{"default", "geek-labs"}) {
		t.Errorf("DeleteAll() = %v, %v", deleted, err)
	}
	if aliases, _ := Aliases(""); len(aliases) != 0 {
		t.Errorf("Aliases() after DeleteAll = %v", aliases)
	}
	if _, err := keyring.Get("ghcli.github.aliases", "ghcli"); err == nil {
		t.Error("alias list should be deleted with the last alias")
	}
}

func TestResolveNoToken(t *testing.T) {
	mockKeyring(t)
	t.Setenv("PATH", t.TempDir()) // no secret manager to auto-detect

	_, _, err := resolve(t, Request{Owner: "geek-labs"})
	if !errors.Is(err, ErrNoToken) || !strings.Contains(err.Error(), "run 'ghcli auth login'") {
		t.Errorf("no token: %v", err)
	}
}

func TestResolveKeyringUnavailable(t *testing.T) {
	home := mockKeyring(t)
	t.Cleanup(keychain.MockUnavailable(errors.New("no D-Bus session bus")))
	t.Setenv("PATH", t.TempDir())

	cfgPath := writeConfig(t, home, "github:\n  default:\n    pat: ghp_plain\n")
	if tok, _, err := resolve(t, Request{ConfigPath: cfgPath}); err != nil || tok.Value != "ghp_plain" {
		t.Errorf("plain fallback: %+v, %v", tok, err)
	}

	writeConfig(t, home, "")
	if _, _, err := resolve(t, Request{}); !errors.Is(err, ErrNoToken) || !strings.Contains(err.Error(), "OS keyring unavailable: no D-Bus") {
		t.Errorf("expected 'no token' error naming the keyring problem, got %v", err)
	}
}

func TestUnresponsiveKeyringIsAccessedOnce(t *testing.T) {
	home := mockKeyring(t)
	t.Setenv("PATH", t.TempDir())

	// a keyring that times out or fails is reported as unavailable on every access
	calls := 0
	t.Cleanup(keychain.MockAvailability(func() error {
		calls++
		return errors.New("no answer within 30s")
	}))

	for name, cfg := range map[string]string{"no token": "", "plain text": "github:\n  geek-labs:\n    pat: ghp_plain\n"} {
		calls = 0
		_, _, _ = resolve(t, Request{ConfigPath: writeConfig(t, home, cfg), Owner: "geek-labs"})
		if calls != 1 {
			t.Errorf("%s: keyring accessed %d times, want 1", name, calls)
		}
	}
}

func TestKeyringOverride(t *testing.T) {
	home := mockKeyring(t)

	if notice := KeyringOverride(writeConfig(t, home, "github:\n  default:\n    pat: x\n")); notice != "" {
		t.Errorf("no command: got %q", notice)
	}
	cfgPath := writeConfig(t, home, "os:\n  keyring_command: secret-tool lookup service github-pat account ghcli\n")
	if notice := KeyringOverride(cfgPath); !strings.Contains(notice, "takes precedence over the OS keyring") ||
		!strings.Contains(notice, cfgPath) || !strings.Contains(notice, "secret-tool lookup") {
		t.Errorf("command: got %q", notice)
	}
}
