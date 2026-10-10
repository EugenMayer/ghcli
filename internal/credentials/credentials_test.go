package credentials

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/EugenMayer/ghcli/internal/config"
	"github.com/EugenMayer/ghcli/pkg/keychain"
)

// mockKeyring replaces the OS keyring with an in-memory one.
func mockKeyring(t *testing.T) {
	t.Helper()
	t.Cleanup(keychain.MockInit())
	old := keyringBackend
	keyringBackend = func(context.Context) (string, error) { return "Mock Keyring", nil }
	t.Cleanup(func() { keyringBackend = old })
}

func cfg(pat, command string) *config.Config {
	return &config.Config{GitHub: config.GitHub{PAT: pat}, OS: config.OS{KeyringCommand: command}}
}

func TestTokenPrecedence(t *testing.T) {
	mockKeyring(t)
	t.Setenv(EnvVar, "")

	check := func(c *config.Config, wantToken string, wantSource Source) {
		t.Helper()
		token, source, err := Token(c)
		if err != nil || token != wantToken || source != wantSource {
			t.Errorf("Token() = %q, %q, %v; want %q, %q", token, source, err, wantToken, wantSource)
		}
	}

	_, _, err := Token(cfg("", ""))
	if !errors.Is(err, ErrNoToken) || !strings.Contains(err.Error(), "ghcli auth login") {
		t.Errorf("expected 'no token' error, got %v", err)
	}

	check(cfg("from-config", ""), "from-config", SourceConfig)

	if err := Store("from-keyring"); err != nil {
		t.Fatal(err)
	}
	// stored at the documented entry
	if got, err := keyring.Get("ghcli.github.pat", "ghcli"); err != nil || got != "from-keyring" {
		t.Errorf("keyring entry = %q, %v", got, err)
	}
	check(cfg("from-config", ""), "from-keyring", SourceKeyring)

	check(cfg("from-config", "echo '  from-command  '"), "from-command", SourceCommand)

	t.Setenv(EnvVar, "from-env")
	check(cfg("from-config", "echo from-command"), "from-env", SourceEnv)
	t.Setenv(EnvVar, "")

	if deleted, err := Delete(); err != nil || !deleted {
		t.Errorf("Delete() = %v, %v", deleted, err)
	}
	if deleted, err := Delete(); err != nil || deleted {
		t.Errorf("second Delete() = %v, %v; want false, nil", deleted, err)
	}
	check(cfg("from-config", ""), "from-config", SourceConfig)
}

func TestTokenKeyringUnavailable(t *testing.T) {
	mockKeyring(t)
	t.Cleanup(keychain.MockUnavailable(errors.New("no D-Bus session bus")))
	t.Setenv(EnvVar, "")

	check := func(c *config.Config, wantToken string, wantSource Source) {
		t.Helper()
		token, source, err := Token(c)
		if err != nil || token != wantToken || source != wantSource {
			t.Errorf("Token() = %q, %q, %v; want %q, %q", token, source, err, wantToken, wantSource)
		}
	}
	check(cfg("from-config", ""), "from-config", SourceConfig)

	if _, _, err := Token(cfg("", "")); !errors.Is(err, ErrNoToken) || !strings.Contains(err.Error(), "OS keyring unavailable: no D-Bus") {
		t.Errorf("expected 'no token' error naming the keyring problem, got %v", err)
	}
}

func TestUnresponsiveKeyringIsAccessedOnce(t *testing.T) {
	fakeEnv(t, nil) // isolated PATH: auto-detection finds no tools

	// a keyring that times out or fails is reported as unavailable on every access
	calls := 0
	t.Cleanup(keychain.MockAvailability(func() error {
		calls++
		return errors.New("no answer within 30s")
	}))

	for name, c := range map[string]*config.Config{"no token": cfg("", ""), "plain text": cfg("from-config", "")} {
		calls = 0
		_, _, _ = resolveForTest(c)
		if calls != 1 {
			t.Errorf("%s: keyring accessed %d times, want 1", name, calls)
		}
	}
}

// resolveForTest resolves with auto-detection, like every ghcli command does.
func resolveForTest(c *config.Config) (keychain.Secret, Source, error) {
	s, err := resolve(context.Background(), c, true)
	return s, source(s), err
}

func TestTokenCommandErrors(t *testing.T) {
	mockKeyring(t)
	t.Setenv(EnvVar, "")

	for _, command := range []string{"true", "echo nope >&2; exit 3"} {
		_, _, err := Token(cfg("from-config", command))
		if err == nil || !strings.HasPrefix(err.Error(), "os.keyring_command:") {
			t.Errorf("keyring_command %q: expected os.keyring_command error, got %v", command, err)
		}
	}
}
