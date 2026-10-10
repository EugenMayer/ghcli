package auth

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/EugenMayer/ghcli/internal/credentials"
)

func newLoginCmd() *cobra.Command {
	var withToken bool

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Store a GitHub token in the OS keyring",
		Example: `  ghcli auth login
  pass show github/ghcli | ghcli auth login --with-token`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			token, err := readToken(withToken)
			if err != nil {
				return err
			}
			if err := credentials.Store(token); err != nil {
				return err
			}
			fmt.Println("✔ token stored in the OS keyring")
			return nil
		},
	}

	cmd.Flags().BoolVar(&withToken, "with-token", false, "read the token from standard input")

	return cmd
}

func readToken(fromStdin bool) (string, error) {
	var token string
	if fromStdin || !term.IsTerminal(int(os.Stdin.Fd())) {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return "", fmt.Errorf("reading token from standard input: %w", err)
		}
		token = line
	} else {
		fmt.Fprint(os.Stderr, "GitHub token: ")
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", fmt.Errorf("reading token: %w", err)
		}
		token = string(b)
	}

	token = strings.TrimSpace(token)
	if token == "" {
		return "", errors.New("no token given")
	}
	return token, nil
}
