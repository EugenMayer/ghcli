package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetKeyringCommand(t *testing.T) {
	const command = `sops -d --extract '["github_pat"]' ~/.ghcli/secrets.enc.yml`

	cases := map[string]string{
		"new file":         "",
		"empty os":         "os:\n",
		"existing keys":    "# my config\ngithub:\n  default:\n    # old token\n    pat: old\nother: 1\n",
		"replaces command": "os:\n  keyring_command: echo old\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "sub", "config.yml")
			if content != "" {
				_ = os.MkdirAll(filepath.Dir(path), 0o700)
				if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			if _, err := SetKeyringCommand(path, command); err != nil {
				t.Fatal(err)
			}

			cfg, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.OS.KeyringCommand != command {
				t.Errorf("keyring_command = %q, want %q", cfg.OS.KeyringCommand, command)
			}

			data, _ := os.ReadFile(path)
			t.Log("\n" + string(data))
			if strings.Contains(content, "pat: old") {
				if cfg.GitHub["default"].PAT != "old" || !strings.Contains(string(data), "# old token") || !strings.Contains(string(data), "other: 1") {
					t.Error("existing keys or comments were lost")
				}
			}
			if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
				t.Errorf("mode = %v, want 0600", info.Mode().Perm())
			}
		})
	}
}
