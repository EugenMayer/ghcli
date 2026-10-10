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

	for _, source := range []Source{SourceKeyring, SourceEnv} {
		if tip := Recommendation(ctx, source); tip != "" {
			t.Errorf("%s: expected no recommendation, got %q", source, tip)
		}
	}
	if tip := Recommendation(ctx, SourceConfig); !strings.Contains(tip, "plain text") || !strings.Contains(tip, "Mock Keyring") {
		t.Errorf("config: got %q", tip)
	}
	if tip := Recommendation(ctx, SourceCommand); !strings.Contains(tip, "remove os.keyring_command") {
		t.Errorf("command: got %q", tip)
	}

	keyringBackend = func(context.Context) (string, error) { return "", errors.New("no D-Bus session bus") }
	if tip := Recommendation(ctx, SourceConfig); !strings.Contains(tip, "os.keyring_command") || strings.Contains(tip, "auth login") {
		t.Errorf("config without keyring: got %q", tip)
	}
	if tip := Recommendation(ctx, SourceCommand); tip != "" {
		t.Errorf("command without keyring: expected none, got %q", tip)
	}
}
