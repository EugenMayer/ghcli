package credentials

import (
	"context"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/EugenMayer/ghcli/internal/config"
)

// mockKeyring replaces the OS keyring with an in-memory one.
func mockKeyring(t *testing.T) {
	t.Helper()
	keyring.MockInit()
	oldAvailable, oldBackend := keyringAvailable, keyringBackend
	keyringAvailable = func() error { return nil }
	keyringBackend = func(context.Context) (string, error) { return "Mock Keyring", nil }
	t.Cleanup(func() { keyringAvailable, keyringBackend = oldAvailable, oldBackend })
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

	if _, _, err := Token(cfg("", "")); err == nil || !strings.Contains(err.Error(), "ghcli auth login") {
		t.Errorf("expected 'no token' error, got %v", err)
	}

	check(cfg("from-config", ""), "from-config", SourceConfig)

	if err := Store("from-keyring"); err != nil {
		t.Fatal(err)
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

func TestTokenCommandErrors(t *testing.T) {
	mockKeyring(t)
	t.Setenv(EnvVar, "")

	for _, command := range []string{"true", "echo nope >&2; exit 3"} {
		if _, _, err := Token(cfg("from-config", command)); err == nil {
			t.Errorf("keyring_command %q: expected error", command)
		}
	}
}
