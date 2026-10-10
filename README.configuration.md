# ghcli configuration

Back to the [README](README.md).

## Creating the token

Create a fine-grained token under
[Settings → Developer settings → Personal access tokens → Fine-grained tokens](https://github.com/settings/personal-access-tokens/new):

- **Resource owner:** the organization that owns the repositories, e.g. `geek-labs`. A token owned
  by your personal account cannot reach organization repositories.
- **Repository access:** the repositories you want to automate, or all of them.
- **Repository permissions:**

  | Permission    | Access         | Needed for                                              |
  |---------------|----------------|---------------------------------------------------------|
  | Pull requests | Read and write | Listing open PRs and their labels, updating branches that are behind |
  | Contents      | Read and write | Merging                                                 |
  | Metadata      | Read-only      | Added automatically                                     |
  | Workflows     | Read and write | Optional: only for PRs that change `.github/workflows/` |

If the organization requires approval for fine-grained tokens, an org owner has to approve the
token before it works.

## Storing the token

Store the token in your OS keyring:

```sh
ghcli auth login    # paste the token, input is hidden
ghcli auth status   # token source, keyring, account and API rate limit
```

```
token:   from OS keyring (gith…1234)
keyring: GNOME Keyring (Secret Service)
limit:   4998 of 5000 API requests left, resets 14:05:12
user:    you (ID 123456)
```

The token is kept under service `ghcli`, user `github.pat` in:

- **Linux:** the Secret Service, i.e. GNOME Keyring, KWallet (KDE Frameworks 5.97+) or KeePassXC
  with Secret Service integration enabled
- **macOS:** the login Keychain
- **Windows:** the Credential Manager

To replace the token, run `ghcli auth login` again. `ghcli auth logout` removes it.

That's all most setups need. The rest of this page is only for special cases.

## Advanced: `keyring_command`

Use `keyring_command` if you can't or don't want to use the OS keyring, e.g.:

- there is no keyring: SSH sessions, servers and WSL usually have no Secret Service running
- your token already lives in a password manager such as pass or 1Password

`os.keyring_command` is a shell command that prints the token. It goes into `~/.ghcli/config.yml` (or the
file given with `--config`) and takes precedence over the OS keyring:

```yaml
os:
  keyring_command: pass show github/ghcli | head -n 1
```

ghcli runs it with `sh -c` (`cmd /C` on Windows) on every call and trims the output. If the command
fails or prints nothing, ghcli stops with that error instead of trying other sources.

### Supported tools

Store the token once with the command in the middle column, then use the `keyring_command` on the
right. Any other command that prints the token works too.

| Tool | Store the token | `keyring_command` |
|------|-----------------|---------------|
| GNOME Keyring / KWallet (`secret-tool`) | `secret-tool store --label="GitHub PAT for ghcli" service github-pat account ghcli` | `secret-tool lookup service github-pat account ghcli` |
| KWallet (`kwallet-query`) | `echo -n "$TOKEN" \| kwallet-query -f ghcli -w github-pat kdewallet` | `kwallet-query -f ghcli -r github-pat kdewallet` |
| macOS Keychain (`security`) | `security add-generic-password -s ghcli-github-pat -a "$USER" -w` | `security find-generic-password -s ghcli-github-pat -w` |
| pass | `pass insert github/ghcli` | `pass show github/ghcli \| head -n 1` |
| gopass | `gopass insert github/ghcli` | `gopass show -o github/ghcli` |
| rbw | `rbw add ghcli` | `rbw get ghcli` |
| Bitwarden (`bw`, needs `BW_SESSION`) | item named `ghcli` | `bw get password ghcli` |
| 1Password (`op`) | item `ghcli` in vault `Private` | `op read "op://Private/ghcli/credential"` |
| gpg | `echo -n "$TOKEN" \| gpg -e -r you@example.com -o ~/.ghcli/pat.gpg` | `gpg --quiet --decrypt ~/.ghcli/pat.gpg` |
| age | `echo -n "$TOKEN" \| age -r "$(age-keygen -y ~/.config/age/key.txt)" -o ~/.ghcli/pat.age` | `age -d -i ~/.config/age/key.txt ~/.ghcli/pat.age` |
| sops | `~/.ghcli/secrets.enc.yml` containing `github_pat: …` | `sops -d --extract '["github_pat"]' ~/.ghcli/secrets.enc.yml` |
| KeePassXC (`keepassxc-cli`) | entry `GitHub/ghcli` | `keepassxc-cli show -s -a Password ~/Passwords.kdbx GitHub/ghcli` |
| GitHub CLI | `gh auth login` | `gh auth token` |

The `secret-tool` row uses the same keyring as `ghcli auth login`, so it's only useful if you want
the token under your own entry. `gh auth token` reuses the GitHub CLI login, which usually has much
broader access than a fine-grained PAT.

### Auto-detection

If no token is configured at all, ghcli tries the tools above, except KeePassXC and the GitHub CLI.
It only tries tools that are installed and, for pass, gpg, age and sops, only if the entry or file
exists. The first command that prints a GitHub token (`github_pat_…`, `ghp_…`, …) is saved as
`keyring_command`, and ghcli tells you:

```
ℹ no token was configured, but one was found via `pass show github/ghcli | head -n 1`.
  Saved it as os.keyring_command in /home/you/.ghcli/config.yml, it will be used from now on.
```

Commands that fail, print something else or don't answer within 10 seconds are skipped. Nothing is
written if no token is found. So with one of these tools, storing the token under the entry name
from the table and running any ghcli command is all the setup needed.

## Other token sources

ghcli uses the first token it finds, in this order:

1. the `GHCLI_PAT` environment variable, e.g. for CI or a one-off run
2. `os.keyring_command` in the config
3. the OS keyring (`ghcli auth login`)
4. `github.pat` in the config: the token in plain text. If you use it, protect the file with
   `chmod 600 ~/.ghcli/config.yml`

## Troubleshooting

| Error                                      | Cause                                                                           |
|--------------------------------------------|---------------------------------------------------------------------------------|
| `404 Not Found` when listing PRs           | The token can't see the repository: wrong resource owner, repository not selected, or token not yet approved |
| `403 Resource not accessible by personal access token` | A permission from [Creating the token](#creating-the-token) is missing |
| `403 API rate limit exceeded for user ID …` | Your account is out of API requests. All your tokens and tools share this limit; wait for the reset |
| `401 Bad credentials`                      | The token is wrong or expired. Check with `ghcli auth status` which source is used |
| `OS keyring unavailable: …dbus…`           | No Secret Service running (SSH, server, WSL): use `GHCLI_PAT`, `os.keyring_command` or `github.pat` |
