package credentials

import (
	"context"
	"fmt"

	"github.com/EugenMayer/ghcli/pkg/keychain"
)

// keyringBackend is replaced in tests.
var keyringBackend = KeyringBackend

// Recommendation returns a hint to move the token into the OS keyring, the recommended storage,
// or "" if there is nothing to recommend. GHCLI_PAT is left alone, as it is meant for CI and
// one-off runs.
func Recommendation(ctx context.Context, t Token) string {
	if t.Source != SourceConfig && t.Source != SourceCommand {
		return ""
	}

	backend, err := keyringBackend(ctx)
	if err != nil {
		if t.Source == SourceConfig {
			return fmt.Sprintf("the token is stored in plain text in the config. As the OS keyring is unavailable (%s), "+
				"consider keeping it in a secret manager via os.keyring_command, see README.configuration.md.", keychain.UnavailableReason(err))
		}
		return ""
	}

	if t.Source == SourceConfig {
		return fmt.Sprintf("the token is stored in plain text in the config. Recommended: run '%s' "+
			"to store it in %s, then remove github.%s.pat from the config.", LoginCommand(t.Alias), backend, t.Alias)
	}
	return fmt.Sprintf("recommended: run '%s' to store the token in %s, "+
		"then remove os.keyring_command from the config.", LoginCommand(t.Alias), backend)
}
