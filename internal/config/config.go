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
	// GitHub holds one account per alias, e.g. "default" or an organization name:
	// github.<alias>.pat.
	GitHub map[string]GitHubAccount `yaml:"github"`
	OS     OS                       `yaml:"os"`
	PR     PR                       `yaml:"pr"`
}

type GitHubAccount struct {
	// PAT is a (fine-grained / scoped) personal access token stored in plain text.
	PAT string `yaml:"pat"`
}

type OS struct {
	// KeyringCommand is a shell command printing the token, used instead of the built-in OS
	// keyring access. {alias} is replaced by the alias, e.g. "pass show github/{alias}"; without
	// it, the command's token is used for every repository.
	KeyringCommand string `yaml:"keyring_command"`
}

type PR struct {
	// MergeStrategy is the default strategy of pr merge, "squash" or "merge"; empty means squash.
	MergeStrategy string `yaml:"merge_strategy"`
	// ReadyToMergeLabel marks pull requests that are ready to merge: preselected by pr label,
	// merged by pr merge --all-ready. Empty means "done".
	ReadyToMergeLabel string `yaml:"ready_to_merge_label"`
	// MergeBlockLabels are labels that keep a pull request from being merged by pr merge and
	// pr ship, in addition to the built-in "hold", which always blocks.
	MergeBlockLabels []string `yaml:"merge_block_labels"`
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
