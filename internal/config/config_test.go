package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	dir := t.TempDir()

	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte("github:\n  geek-labs:\n    pat: secret\nos:\n  keyring_command: pass show ghcli\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GitHub["geek-labs"].PAT != "secret" || cfg.OS.KeyringCommand != "pass show ghcli" {
		t.Errorf("unexpected config %+v", cfg)
	}

	if _, err := Load(filepath.Join(dir, "missing.yml")); err == nil {
		t.Error("expected error for missing explicitly given file")
	}

	t.Setenv("HOME", dir)
	cfg, err = Load("")
	if err != nil || len(cfg.GitHub) != 0 || cfg.OS != (OS{}) {
		t.Errorf("missing default config: got %+v, %v; want empty config", cfg, err)
	}
}
