package keychaincmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/EugenMayer/ghcli/pkg/keychain"
)

func run(t *testing.T, kc *keychain.Keychain, stdin string, args ...string) string {
	t.Helper()
	cmd := New(kc)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out.String())
	}
	return out.String()
}

func TestCommands(t *testing.T) {
	t.Cleanup(keychain.MockInit())
	kc := keychain.New("app")

	if out := run(t, kc, "tok-123456789\n", "set", "jira.token"); !strings.Contains(out, "stored jira.token") {
		t.Errorf("set: %s", out)
	}
	if out := run(t, kc, "", "get", "jira.token"); out != "tok-123456789\n" {
		t.Errorf("get: %q", out)
	}
	out := run(t, kc, "", "status", "jira.token", "other")
	for _, want := range []string{"keyring: in-memory mock keyring", "✔ jira.token (tok-…6789)", "✘ other not stored"} {
		if !strings.Contains(out, want) {
			t.Errorf("status misses %q:\n%s", want, out)
		}
	}
	if out := run(t, kc, "", "delete", "jira.token"); !strings.Contains(out, "removed jira.token") {
		t.Errorf("delete: %s", out)
	}
	if out := run(t, kc, "", "delete", "jira.token"); !strings.Contains(out, "not stored") {
		t.Errorf("second delete: %s", out)
	}
}
