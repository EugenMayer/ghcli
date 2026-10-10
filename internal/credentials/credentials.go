// Package credentials resolves the GitHub token from the environment, a command, the OS keyring
// or the config file, and manages the token stored in the keyring.
package credentials

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/EugenMayer/ghcli/internal/config"
)

const (
	// EnvVar overrides every other token source.
	EnvVar = "GHCLI_PAT"

	keyringService = "ghcli"
	keyringUser    = "github.pat"
)

// ErrNoToken is returned by Token when no source provides a token.
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
// and github.pat from the config.
func Token(cfg *config.Config) (string, Source, error) {
	if t := strings.TrimSpace(os.Getenv(EnvVar)); t != "" {
		return t, SourceEnv, nil
	}

	if cfg.OS.KeyringCommand != "" {
		// stdin is passed through so the command may prompt, e.g. for a gpg passphrase.
		t, err := runCommand(context.Background(), cfg.OS.KeyringCommand, os.Stdin)
		if err != nil {
			return "", SourceCommand, fmt.Errorf("os.keyring_command: %w", err)
		}
		return t, SourceCommand, nil
	}

	t, keyringErr := keyringGet()
	if keyringErr == nil {
		return t, SourceKeyring, nil
	}

	if t := strings.TrimSpace(cfg.GitHub.PAT); t != "" {
		return t, SourceConfig, nil
	}

	msg := "run 'ghcli auth login' to store one in the OS keyring (recommended), " +
		"or set " + EnvVar + " or os.keyring_command / github.pat in ~/.ghcli/config.yml"
	if !errors.Is(keyringErr, keyring.ErrNotFound) {
		msg += fmt.Sprintf(" (OS keyring unavailable: %v)", keyringErr)
	}
	return "", "", fmt.Errorf("%w: %s", ErrNoToken, msg)
}

// Store saves the token in the OS keyring.
func Store(token string) error {
	if err := keyringAvailable(); err != nil {
		return fmt.Errorf("storing token in the OS keyring: %w", err)
	}
	if err := keyring.Set(keyringService, keyringUser, token); err != nil {
		return fmt.Errorf("storing token in the OS keyring: %w", err)
	}
	return nil
}

// Delete removes the token from the OS keyring. It reports false if no token was stored.
func Delete() (bool, error) {
	if err := keyringAvailable(); err != nil {
		return false, fmt.Errorf("removing token from the OS keyring: %w", err)
	}
	err := keyring.Delete(keyringService, keyringUser)
	if errors.Is(err, keyring.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("removing token from the OS keyring: %w", err)
	}
	return true, nil
}

// keyringTimeout bounds a keyring lookup. It is generous, as the desktop may show an unlock prompt.
const keyringTimeout = 30 * time.Second

func keyringGet() (string, error) {
	if err := keyringAvailable(); err != nil {
		return "", err
	}

	type result struct {
		token string
		err   error
	}
	done := make(chan result, 1)
	go func() {
		t, err := keyring.Get(keyringService, keyringUser)
		done <- result{t, err}
	}()

	var t string
	select {
	case r := <-done:
		if r.err != nil {
			return "", r.err
		}
		t = r.token
	case <-time.After(keyringTimeout):
		return "", fmt.Errorf("no answer within %s", keyringTimeout)
	}
	if t = strings.TrimSpace(t); t == "" {
		return "", keyring.ErrNotFound
	}
	return t, nil
}

// runCommand runs command in the shell and returns its trimmed output.
// keyringAvailable reports why the OS keyring can't be used, or nil. On Linux the Secret Service
// is reached via the D-Bus session bus. Without one, the D-Bus library would launch a new bus whose
// keyring is locked and can't show an unlock prompt, which blocks forever (SSH, servers, WSL).
// Replaced in tests.
var keyringAvailable = func() error {
	if runtime.GOOS != "linux" {
		return nil
	}
	if os.Getenv("DBUS_SESSION_BUS_ADDRESS") != "" {
		return nil
	}
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		if _, err := os.Stat(filepath.Join(dir, "bus")); err == nil {
			return nil
		}
	}
	return errors.New("no D-Bus session bus (DBUS_SESSION_BUS_ADDRESS is not set)")
}

func runCommand(ctx context.Context, command string, stdin io.Reader) (string, error) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}
	// Children of the shell may keep stdout open after a timeout kill; don't wait for them forever.
	cmd.WaitDelay = time.Second

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdin = stdin
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("%q failed: %w: %s", command, err, msg)
		}
		return "", fmt.Errorf("%q failed: %w", command, err)
	}

	t := strings.TrimSpace(string(out))
	if t == "" {
		return "", fmt.Errorf("%q printed no token", command)
	}
	return t, nil
}
