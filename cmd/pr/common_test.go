package pr

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"github.com/EugenMayer/ghcli/internal/config"
)

func TestLoadPRConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(path, []byte("pr:\n  merge_strategy: merge\n  ready_to_merge_label: ready\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := &cobra.Command{}
	cmd.Flags().String("config", path, "")

	cfg, err := loadPRConfig(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if want := (config.PR{MergeStrategy: "merge", ReadyToMergeLabel: "ready"}); cfg != want {
		t.Errorf("got %+v, want %+v", cfg, want)
	}
}

func TestResolveStrategy(t *testing.T) {
	tests := []struct {
		name, cfg, flag, want string
		wantErr               bool
	}{
		{name: "default", want: "squash"},
		{name: "config", cfg: "merge", want: "merge"},
		{name: "flag over config", cfg: "merge", flag: "squash", want: "squash"},
		{name: "invalid flag", flag: "rebase", wantErr: true},
		{name: "invalid config", cfg: "rebase", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveStrategy(tt.flag, config.PR{MergeStrategy: tt.cfg})
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Errorf("got %q, %v; want %q, error %v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestResolveReadyLabel(t *testing.T) {
	tests := []struct{ name, cfg, flag, want string }{
		{name: "default", want: "done"},
		{name: "config", cfg: "ready", want: "ready"},
		{name: "flag over config", cfg: "ready", flag: "merge-me", want: "merge-me"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveReadyLabel(tt.flag, config.PR{ReadyToMergeLabel: tt.cfg}); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
