// Package credentials resolves ghcli's GitHub token from GHCLI_PAT, os.keyring_command, the OS
// keyring or the config file, built on pkg/keychain.
package credentials

import (
	"context"
	"errors"
	"fmt"

	"github.com/EugenMayer/ghcli/internal/config"
	"github.com/EugenMayer/ghcli/pkg/keychain"
)

const (
	// EnvVar overrides every other token source.
	EnvVar = "GHCLI_PAT"

	// TokenPath is the token's path in the keychain: service "ghcli.github.pat", account "ghcli".
	TokenPath = "github.pat"

	// Entry used before ghcli.github.pat, migrated on first read.
	legacyService = "ghcli"
	legacyAccount = "github.pat"
)

// Keychain holds ghcli's secrets in the OS keyring.
var Keychain = keychain.New("ghcli")

// ErrNoToken is returned when no source provides a token.
var ErrNoToken = errors.New("no GitHub token found")

// Source describes where a token came from.
type Source string

const (
	SourceEnv     Source = "environment variable " + EnvVar
	SourceCommand Source = "os.keyring_command"
	SourceKeyring Source = "OS keyring"
	SourceConfig  Source = "github.pat in the config file"
)

// Token returns the GitHub token, trying in order: GHCLI_PAT, os.keyring_command, the OS keyring
// and github.pat from the config. It does not auto-detect, see Resolve.
func Token(cfg *config.Config) (string, Source, error) {
	s, err := resolve(context.Background(), cfg, false)
	if err != nil {
		return "", "", err
	}
	return s.Value, source(s), nil
}

func resolve(ctx context.Context, cfg *config.Config, detect bool) (keychain.Secret, error) {
	r := &keychain.Resolver{
		Keychain: Keychain,
		EnvVar:   func(string) string { return EnvVar },
		Command:  cfg.OS.KeyringCommand,
		Plain:    func(string) string { return cfg.GitHub.PAT },
	}

	s, err := r.Resolve(ctx, TokenPath)
	if isNotFound(err) {
		if moved, _ := Keychain.MigrateEntry(legacyService, legacyAccount, TokenPath); moved {
			s, err = r.Resolve(ctx, TokenPath)
		}
	}
	if isNotFound(err) && detect {
		r.Candidates, r.Validate = candidates, isGitHubToken
		s, err = r.Resolve(ctx, TokenPath)
	}

	var nf *keychain.NotFoundError
	switch {
	case errors.As(err, &nf):
		msg := "run 'ghcli auth login' to store one in the OS keyring (recommended), " +
			"or set " + EnvVar + " or os.keyring_command / github.pat in ~/.ghcli/config.yml"
		if errors.Is(nf.KeychainErr, keychain.ErrUnavailable) {
			msg += fmt.Sprintf(" (%v)", nf.KeychainErr)
		}
		return s, fmt.Errorf("%w: %s", ErrNoToken, msg)
	case err != nil && cfg.OS.KeyringCommand != "":
		return s, fmt.Errorf("os.keyring_command: %w", err)
	}
	return s, err
}

func isNotFound(err error) bool {
	var nf *keychain.NotFoundError
	return errors.As(err, &nf)
}

func source(s keychain.Secret) Source {
	switch s.Source {
	case keychain.SourceEnv:
		return SourceEnv
	case keychain.SourceKeychain:
		return SourceKeyring
	case keychain.SourcePlain:
		return SourceConfig
	default: // SourceCommand, SourceDetected
		return SourceCommand
	}
}

// Store saves the token in the OS keyring.
func Store(token string) error {
	if err := Keychain.Set(TokenPath, token); err != nil {
		return fmt.Errorf("storing token in the OS keyring: %w", err)
	}
	return nil
}

// Delete removes the token, including a legacy entry, from the OS keyring. It reports false if no
// token was stored.
func Delete() (bool, error) {
	deleted, err := Keychain.Delete(TokenPath)
	if err != nil {
		return false, fmt.Errorf("removing token from the OS keyring: %w", err)
	}
	if legacy, err := Keychain.DeleteEntry(legacyService, legacyAccount); err == nil && legacy {
		deleted = true
	}
	return deleted, nil
}

// KeyringBackend describes the OS keyring in use, e.g. "GNOME Keyring (Secret Service)".
func KeyringBackend(ctx context.Context) (string, error) {
	return Keychain.Backend(ctx)
}
