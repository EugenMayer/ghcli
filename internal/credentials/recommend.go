package credentials

import (
	"context"
	"fmt"
)

// Recommendation returns a hint to move the token into the OS keyring, the recommended storage,
// or "" if there is nothing to recommend for source. GHCLI_PAT is left alone, as it is meant for
// CI and one-off runs.
func Recommendation(ctx context.Context, source Source) string {
	if source != SourceConfig && source != SourceCommand {
		return ""
	}

	backend, err := KeyringBackend(ctx)
	if err != nil {
		if source == SourceConfig {
			return fmt.Sprintf("the token is stored in plain text in the config. As the OS keyring is unavailable (%v), "+
				"consider keeping it in a secret manager via os.keyring_command, see README.configuration.md.", err)
		}
		return ""
	}

	switch source {
	case SourceConfig:
		return fmt.Sprintf("the token is stored in plain text in the config. Recommended: run 'ghcli auth login' "+
			"to store it in %s, then remove github.pat from the config.", backend)
	default:
		return fmt.Sprintf("recommended: run 'ghcli auth login' to store the token in %s, "+
			"then remove os.keyring_command from the config.", backend)
	}
}
