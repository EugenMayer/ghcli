// Package credentials resolves ghcli's GitHub tokens, built on pkg/keychain.
//
// Fine-grained tokens belong to a single owner, so ghcli keeps one token per alias. By convention
// an alias is the organization (or user) name, so the token for a repository is found by its
// owner; "default" is the token for everything else.
package credentials

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/EugenMayer/ghcli/internal/config"
	"github.com/EugenMayer/ghcli/pkg/keychain"
)

const (
	// EnvVar holds a token that is used for every repository, overriding all other sources.
	EnvVar = "GHCLI_PAT"

	// DefaultAlias is the alias used when none is given.
	DefaultAlias = "default"

	// aliasesPath lists the aliases with a token in the keychain, as keyrings can't enumerate.
	aliasesPath = "github.aliases"

	// aliasPlaceholder is replaced by the alias in os.keyring_command.
	aliasPlaceholder = "{alias}"
)

// Keychain holds ghcli's secrets in the OS keyring.
var Keychain = keychain.New("ghcli")

// ErrNoToken is returned when no source provides a token.
var ErrNoToken = errors.New("no GitHub token found")

// TokenPath is the keychain path of an alias' token: service "ghcli.github.<alias>.pat".
func TokenPath(alias string) string { return "github." + alias + ".pat" }

var aliasRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ValidateAlias checks that alias is usable, e.g. an organization name.
func ValidateAlias(alias string) error {
	if !aliasRe.MatchString(alias) {
		return fmt.Errorf("invalid alias %q: use letters, digits and . _ -, e.g. the organization name", alias)
	}
	return nil
}

// Source describes where a token came from.
type Source int

const (
	SourceEnv Source = iota + 1
	SourceCommand
	SourceKeyring
	SourceConfig
)

// Token is a resolved token.
type Token struct {
	Value  string
	Source Source
	// Alias is the alias the token belongs to; empty for GHCLI_PAT and for an os.keyring_command
	// without {alias}, which serve every repository.
	Alias string
	// Reason tells why Alias was chosen, e.g. "matches the repository owner".
	Reason string
	// Detected is the command found by auto-detection, which Resolve saved to the config.
	Detected string
}

// From describes the token's source including its alias, e.g. "OS keyring (alias geek-labs)".
func (t Token) From() string {
	if t.Alias != "" && (t.Source == SourceKeyring || t.Source == SourceCommand) {
		return fmt.Sprintf("%s (alias %s)", t.Where(), t.Alias)
	}
	return t.Where()
}

// Where describes the token's source, e.g. "OS keyring".
func (t Token) Where() string {
	switch t.Source {
	case SourceEnv:
		return "environment variable " + EnvVar
	case SourceCommand:
		return "os.keyring_command"
	case SourceKeyring:
		return "OS keyring"
	default:
		return fmt.Sprintf("github.%s.pat in the config file", t.Alias)
	}
}

// Request describes which token to resolve.
type Request struct {
	ConfigPath string
	// Owner is the repository owner, used to pick the token whose alias matches. May be empty.
	Owner string
	// Alias forces a token, e.g. from --alias. May be empty.
	Alias string
	// Ask lets the user pick an alias if several tokens exist and none matches. nil means fail.
	Ask func(aliases []string) (string, error)
	// NoAliasNotice suppresses the notice about a fallback alias, for callers that show it.
	NoAliasNotice bool
}

// Resolve returns the token for req:
//
//  1. GHCLI_PAT, if set, for every repository
//  2. os.keyring_command without {alias}, for every repository
//  3. otherwise the token of an alias: req.Alias, or the alias matching req.Owner, or the only
//     token, or "default", or one the user picks via req.Ask. The alias' token comes from
//     os.keyring_command with {alias}, the OS keyring or github.<alias>.pat in the config.
//
// If nothing is configured at all, the default token is auto-detected and its command saved to
// the config. Notices (auto-detection, plain-text tokens, a fallback alias) are written to out.
func Resolve(ctx context.Context, req Request, out io.Writer) (Token, error) {
	if v := strings.TrimSpace(os.Getenv(EnvVar)); v != "" {
		return Token{Value: v, Source: SourceEnv}, nil
	}
	if req.Alias != "" {
		if err := ValidateAlias(req.Alias); err != nil {
			return Token{}, err
		}
	}

	cfg, err := config.Load(req.ConfigPath)
	if err != nil {
		return Token{}, err
	}

	if command := cfg.OS.KeyringCommand; command != "" {
		if !strings.Contains(command, aliasPlaceholder) {
			v, err := keychain.RunCommand(ctx, command, os.Stdin)
			if err != nil {
				return Token{}, fmt.Errorf("os.keyring_command: %w", err)
			}
			return Token{Value: v, Source: SourceCommand}, nil
		}
		return resolveCommandAlias(ctx, cfg, req, out)
	}

	aliases, keychainErr := knownAliases(cfg)
	alias, reason, err := selectAlias(req, aliases)
	if err != nil {
		return Token{}, err
	}

	detect := len(aliases) == 0 && alias == DefaultAlias
	t, err := resolveAlias(ctx, cfg, alias, keychainErr, detect)
	if err != nil {
		return Token{}, err
	}
	t.Reason = reason

	switch {
	case t.Detected != "":
		path, writeErr := config.SetKeyringCommand(req.ConfigPath, t.Detected)
		if writeErr != nil {
			fmt.Fprintf(out, "ℹ found a GitHub token via `%s`, but could not save it to the config: %v\n", t.Detected, writeErr)
		} else {
			fmt.Fprintf(out, "ℹ no token was configured, but one was found via `%s`.\n"+
				"  Saved it as os.keyring_command in %s, it will be used from now on.\n", t.Detected, path)
		}
		t.Alias = "" // the saved command has no {alias}, so it serves every repository
	case t.Source == SourceConfig:
		fmt.Fprintln(out, "ℹ "+Recommendation(ctx, t))
	}
	if req.Owner != "" && !req.NoAliasNotice && (reason == reasonDefault || reason == reasonAsked) {
		fmt.Fprintf(out, "ℹ using the token with alias %q for %s: %s\n", alias, req.Owner, reason)
	}
	return t, nil
}

const (
	reasonFlag    = "set with --alias"
	reasonOwner   = "matches the repository owner"
	reasonOnly    = "the only token"
	reasonDefault = "no token with an alias matching the repository owner, using the default"
	reasonAsked   = "selected"
)

// selectAlias picks the alias for req among the known ones.
func selectAlias(req Request, aliases []string) (alias, reason string, err error) {
	if req.Alias != "" {
		return req.Alias, reasonFlag, nil
	}
	if req.Owner != "" {
		if i := slices.IndexFunc(aliases, func(a string) bool { return strings.EqualFold(a, req.Owner) }); i >= 0 {
			return aliases[i], reasonOwner, nil
		}
	}
	switch {
	case len(aliases) == 0:
		return DefaultAlias, "", nil
	case len(aliases) == 1:
		return aliases[0], reasonOnly, nil
	case slices.Contains(aliases, DefaultAlias):
		return DefaultAlias, reasonDefault, nil
	case req.Ask != nil:
		alias, err := req.Ask(aliases)
		return alias, reasonAsked, err
	default:
		return "", "", fmt.Errorf("several tokens exist (%s), none with an alias matching %q: choose one with --alias",
			strings.Join(aliases, ", "), req.Owner)
	}
}

// resolveCommandAlias resolves via os.keyring_command with {alias}. As the command's store can't
// be listed, it tries the alias given, else the repository owner, then the default alias.
func resolveCommandAlias(ctx context.Context, cfg *config.Config, req Request, out io.Writer) (Token, error) {
	type attempt struct{ alias, reason string }
	var attempts []attempt
	switch {
	case req.Alias != "":
		attempts = []attempt{{req.Alias, reasonFlag}}
	case req.Owner != "":
		attempts = []attempt{{req.Owner, reasonOwner}, {DefaultAlias, reasonDefault}}
	default:
		attempts = []attempt{{DefaultAlias, ""}}
	}

	var errs []error
	for _, a := range attempts {
		command := strings.ReplaceAll(cfg.OS.KeyringCommand, aliasPlaceholder, a.alias)
		v, err := keychain.RunCommand(ctx, command, os.Stdin)
		if err == nil {
			if a.reason == reasonDefault && !req.NoAliasNotice {
				fmt.Fprintf(out, "ℹ using the token with alias %q for %s: %s\n", a.alias, req.Owner, a.reason)
			}
			return Token{Value: v, Source: SourceCommand, Alias: a.alias, Reason: a.reason}, nil
		}
		errs = append(errs, err)
	}
	return Token{}, fmt.Errorf("os.keyring_command: %w", errors.Join(errs...))
}

// resolveAlias returns the token of alias from the OS keyring or github.<alias>.pat in the
// config, or auto-detects it if detect is set. keychainErr is a known keyring failure.
func resolveAlias(ctx context.Context, cfg *config.Config, alias string, keychainErr error, detect bool) (Token, error) {
	r := &keychain.Resolver{
		Keychain:    Keychain,
		EnvVar:      func(string) string { return "" }, // GHCLI_PAT is handled by Resolve
		Plain:       func(string) string { return cfg.GitHub[alias].PAT },
		KeychainErr: keychainErr,
	}
	if detect {
		r.Candidates, r.Validate = candidates, isGitHubToken
	}

	s, err := r.Resolve(ctx, TokenPath(alias))
	var nf *keychain.NotFoundError
	if errors.As(err, &nf) {
		msg := fmt.Sprintf("run '%s' to store one in the OS keyring (recommended), "+
			"or set %s, os.keyring_command or github.%s.pat in ~/.ghcli/config.yml", LoginCommand(alias), EnvVar, alias)
		if errors.Is(nf.KeychainErr, keychain.ErrUnavailable) {
			msg += fmt.Sprintf(" (%v)", nf.KeychainErr)
		}
		if alias != DefaultAlias {
			return Token{}, fmt.Errorf("%w for alias %q: %s", ErrNoToken, alias, msg)
		}
		return Token{}, fmt.Errorf("%w: %s", ErrNoToken, msg)
	}
	if err != nil {
		return Token{}, err
	}

	t := Token{Value: s.Value, Alias: alias}
	switch s.Source {
	case keychain.SourceKeychain:
		t.Source = SourceKeyring
	case keychain.SourcePlain:
		t.Source = SourceConfig
	default: // SourceDetected
		t.Source, t.Detected = SourceCommand, s.Command
	}
	return t, nil
}

// knownAliases returns the aliases with a token in the keychain or the config, sorted. A keyring
// failure is returned as keychainErr, so it isn't retried (and waited for) per alias.
func knownAliases(cfg *config.Config) (aliases []string, keychainErr error) {
	aliases, keychainErr = Keychain.List(aliasesPath)
	for alias, account := range cfg.GitHub {
		if strings.TrimSpace(account.PAT) != "" {
			aliases = append(aliases, alias)
		}
	}
	slices.Sort(aliases)
	return slices.Compact(aliases), keychainErr
}

// Aliases returns the aliases with a token in the OS keyring or the config file, sorted.
func Aliases(configPath string) ([]string, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}
	aliases, _ := knownAliases(cfg)
	return aliases, nil
}

// KeyringOverride returns a notice if os.keyring_command is set, as it takes precedence over the
// OS keyring: tokens stored with 'ghcli auth login' are then not used. It returns "" otherwise.
func KeyringOverride(configPath string) string {
	cfg, err := config.Load(configPath)
	if err != nil || cfg.OS.KeyringCommand == "" {
		return ""
	}
	path := configPath
	if path == "" {
		path, _ = config.DefaultPath()
	}
	return fmt.Sprintf("os.keyring_command (%q) is set in %s and takes precedence over the OS keyring, "+
		"so tokens stored with 'ghcli auth login' are not used. Remove os.keyring_command to use them.",
		cfg.OS.KeyringCommand, path)
}

// ResolveAlias returns the token of one alias, without auto-detection or GHCLI_PAT.
func ResolveAlias(ctx context.Context, configPath, alias string) (Token, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return Token{}, err
	}
	if cfg.OS.KeyringCommand != "" {
		return resolveCommandAlias(ctx, cfg, Request{Alias: alias}, io.Discard)
	}
	return resolveAlias(ctx, cfg, alias, nil, false)
}

// LoginCommand is the command storing the token of alias.
func LoginCommand(alias string) string {
	if alias == DefaultAlias || alias == "" {
		return "ghcli auth login"
	}
	return "ghcli auth login --alias " + alias
}

// Store saves the token of alias in the OS keyring.
func Store(alias, token string) error {
	if err := ValidateAlias(alias); err != nil {
		return err
	}
	if err := Keychain.Set(TokenPath(alias), token); err != nil {
		return fmt.Errorf("storing token in the OS keyring: %w", err)
	}
	if err := Keychain.AddToList(aliasesPath, alias); err != nil {
		return fmt.Errorf("storing token in the OS keyring: %w", err)
	}
	return nil
}

// Delete removes the token of alias from the OS keyring. It reports false if none was stored.
func Delete(alias string) (bool, error) {
	if err := ValidateAlias(alias); err != nil {
		return false, err
	}
	deleted, err := Keychain.Delete(TokenPath(alias))
	if err != nil {
		return false, fmt.Errorf("removing token from the OS keyring: %w", err)
	}
	if err := Keychain.RemoveFromList(aliasesPath, alias); err != nil {
		return deleted, fmt.Errorf("removing token from the OS keyring: %w", err)
	}
	return deleted, nil
}

// DeleteAll removes the tokens of all aliases from the OS keyring and returns their aliases.
func DeleteAll() ([]string, error) {
	aliases, err := Keychain.List(aliasesPath)
	if err != nil {
		return nil, fmt.Errorf("removing tokens from the OS keyring: %w", err)
	}
	var deleted []string
	for _, alias := range aliases {
		ok, err := Delete(alias)
		if err != nil {
			return deleted, err
		}
		if ok {
			deleted = append(deleted, alias)
		}
	}
	return deleted, nil
}

// KeyringBackend describes the OS keyring in use, e.g. "GNOME Keyring (Secret Service)".
func KeyringBackend(ctx context.Context) (string, error) {
	return Keychain.Backend(ctx)
}
