// Package keychaincmd provides cobra commands to manage secrets of a keychain.Keychain:
//
//	<app> secret set <path>      store a secret, input is hidden (--stdin to pipe it in)
//	<app> secret get <path>      print a secret, e.g. for scripts
//	<app> secret delete <path>   remove a secret
//	<app> secret status [path…]  show the keyring backend and which paths are stored
//
// Usage:
//
//	kc := keychain.New("mytool")
//	rootCmd.AddCommand(keychaincmd.New(kc))
package keychaincmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/EugenMayer/ghcli/pkg/keychain"
)

// New returns the "secret" command with set, get, delete and status subcommands.
func New(kc *keychain.Keychain) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secret",
		Short: "Manage secrets in the OS keyring",
		Long: fmt.Sprintf(`Manage secrets in the OS keyring (GNOME Keyring, KWallet, macOS Keychain, Windows
Credential Manager). A secret at path P is stored under service "%s.P", account "%s".`,
			kc.Namespace(), kc.Namespace()),
	}
	cmd.AddCommand(newSet(kc), newGet(kc), newDelete(kc), newStatus(kc))
	return cmd
}

func newSet(kc *keychain.Keychain) *cobra.Command {
	var fromStdin bool
	cmd := &cobra.Command{
		Use:   "set <path>",
		Short: "Store a secret in the OS keyring",
		Example: `  secret set jira.token
  pass show work/jira | secret set jira.token --stdin`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			if err := keychain.ValidatePath(path); err != nil {
				return err
			}
			secret, err := ReadSecret(cmd.InOrStdin(), cmd.ErrOrStderr(), path+": ", fromStdin)
			if err != nil {
				return err
			}
			if err := kc.Set(path, secret); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "✔ stored %s in the OS keyring\n", path)
			return nil
		},
	}
	cmd.Flags().BoolVar(&fromStdin, "stdin", false, "read the secret from standard input")
	return cmd
}

func newGet(kc *keychain.Keychain) *cobra.Command {
	return &cobra.Command{
		Use:   "get <path>",
		Short: "Print a secret from the OS keyring",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			secret, err := kc.Get(args[0])
			if err != nil {
				return fmt.Errorf("%s: %w", args[0], err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), secret)
			return nil
		},
	}
}

func newDelete(kc *keychain.Keychain) *cobra.Command {
	return &cobra.Command{
		Use:     "delete <path>",
		Aliases: []string{"rm"},
		Short:   "Remove a secret from the OS keyring",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			deleted, err := kc.Delete(args[0])
			if err != nil {
				return err
			}
			if deleted {
				fmt.Fprintf(cmd.OutOrStdout(), "✔ removed %s from the OS keyring\n", args[0])
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "%s is not stored in the OS keyring\n", args[0])
			}
			return nil
		},
	}
}

func newStatus(kc *keychain.Keychain) *cobra.Command {
	return &cobra.Command{
		Use:   "status [path…]",
		Short: "Show the keyring backend and whether the given paths are stored",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			backend, err := kc.Backend(cmd.Context())
			if err != nil {
				fmt.Fprintf(out, "keyring: unavailable: %s\n", keychain.UnavailableReason(err))
				return nil
			}
			fmt.Fprintf(out, "keyring: %s\n", backend)

			for _, path := range args {
				secret, err := kc.Get(path)
				switch {
				case err == nil:
					fmt.Fprintf(out, "  ✔ %s (%s)\n", path, Mask(secret))
				case errors.Is(err, keychain.ErrNotFound):
					fmt.Fprintf(out, "  ✘ %s not stored\n", path)
				default:
					fmt.Fprintf(out, "  ✘ %s: %v\n", path, err)
				}
			}
			return nil
		},
	}
}

// ReadSecret reads a secret from in. If in is a terminal and fromStdin is false, prompt is shown
// on promptOut and the input is hidden; otherwise the first line of in is read.
func ReadSecret(in io.Reader, promptOut io.Writer, prompt string, fromStdin bool) (string, error) {
	var secret string
	if f, ok := in.(*os.File); ok && !fromStdin && term.IsTerminal(int(f.Fd())) {
		fmt.Fprint(promptOut, prompt)
		b, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(promptOut)
		if err != nil {
			return "", fmt.Errorf("reading secret: %w", err)
		}
		secret = string(b)
	} else {
		line, err := bufio.NewReader(in).ReadString('\n')
		if err != nil && line == "" {
			return "", fmt.Errorf("reading secret from standard input: %w", err)
		}
		secret = line
	}

	secret = strings.TrimSpace(secret)
	if secret == "" {
		return "", errors.New("no secret given")
	}
	return secret, nil
}

// Mask shows only the first and last 4 characters of a secret.
func Mask(secret string) string {
	if len(secret) <= 8 {
		return "****"
	}
	return secret[:4] + "…" + secret[len(secret)-4:]
}
