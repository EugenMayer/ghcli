package credentials

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRecommendation(t *testing.T) {
	mockKeyring(t)
	ctx := context.Background()

	for _, tok := range []Token{{Source: SourceKeyring, Alias: "x"}, {Source: SourceEnv}} {
		if tip := Recommendation(ctx, tok); tip != "" {
			t.Errorf("%+v: expected no recommendation, got %q", tok, tip)
		}
	}
	if tip := Recommendation(ctx, Token{Source: SourceConfig, Alias: DefaultAlias}); !strings.Contains(tip, "run 'ghcli auth login' to store it in Mock Keyring, then remove github.default.pat") {
		t.Errorf("config default: got %q", tip)
	}
	if tip := Recommendation(ctx, Token{Source: SourceCommand, Alias: "geek-labs"}); !strings.Contains(tip, "'ghcli auth login --alias geek-labs'") ||
		!strings.Contains(tip, "remove os.keyring_command") {
		t.Errorf("command: got %q", tip)
	}

	keyringBackend = func(context.Context) (string, error) { return "", errors.New("no D-Bus session bus") }
	if tip := Recommendation(ctx, Token{Source: SourceConfig, Alias: DefaultAlias}); !strings.Contains(tip, "os.keyring_command") || strings.Contains(tip, "auth login") {
		t.Errorf("config without keyring: got %q", tip)
	}
	if tip := Recommendation(ctx, Token{Source: SourceCommand}); tip != "" {
		t.Errorf("command without keyring: expected none, got %q", tip)
	}
}
