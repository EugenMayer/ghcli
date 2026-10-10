package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

// Config mirrors ~/.ghcli/config.yml.
type Config struct {
	GitHub GitHub `yaml:"github"`
	OS     OS     `yaml:"os"`
}

type GitHub struct {
	// PAT is a (fine-grained / scoped) personal access token stored in plain text.
	PAT string `yaml:"pat"`
}

type OS struct {
	// KeyringCommand is a shell command printing the token, e.g. "pass show github/ghcli".
	// It is used instead of the built-in OS keyring access.
	KeyringCommand string `yaml:"keyring_command"`
}

// DefaultPath returns ~/.ghcli/config.yml.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, ".ghcli", "config.yml"), nil
}

// Load reads the config from path, or from DefaultPath when path is empty. A missing file at the
// default location yields an empty config, as the token may come from elsewhere (env, keyring).
func Load(path string) (*Config, error) {
	explicit := path != ""
	if !explicit {
		var err error
		if path, err = DefaultPath(); err != nil {
			return nil, err
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && !explicit {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}
	return &cfg, nil
}
