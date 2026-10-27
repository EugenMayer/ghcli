# ghcli configuration

Back to the [README](README.md).

## Creating the token

The easiest way: run `ghcli auth login --alias <org>`. Before asking for the token, it prints a
link to GitHub's token form, pre-filled with the organization as resource owner, the permissions
below and an expiry of 90 days:

```
Click here to create a PAT with the right permissions - it will let you confirm and adjust:
  https://github.com/settings/personal-access-tokens/new?contents=write&…&target_name=geek-labs
  Select the repositories there, then paste the new token below.
```

GitHub shows the form with everything filled in for you to confirm or adjust; only the repository
selection can't be pre-filled. `--web` opens the link in the browser, `--expires 30` (or `none`)
changes the expiry, `--workflows` adds Workflows: write.

To create the token by hand, use
[Settings → Developer settings → Personal access tokens → Fine-grained tokens](https://github.com/settings/personal-access-tokens/new):

- **Resource owner:** the organization that owns the repositories, e.g. `geek-labs`. A token owned
  by your personal account cannot reach organization repositories.
- **Repository access:** the repositories you want to automate, or all of them.
- **Repository permissions:**

  | Permission    | Access         | Command | Needed for |
  |---------------|----------------|---------|------------|
  | Pull requests | Read and write | `pr list` (read), `pr label`, `pr merge` (write) | Listing open PRs (read); adding labels to PRs and updating branches that are behind before a merge (write) |
  | Contents      | Read and write | `pr merge` | Merging |
  | Metadata      | Read-only      | all | Added automatically |
  | Workflows     | Read and write | `pr merge` | Optional: only for PRs that change `.github/workflows/` |

  **Pull requests must be write for `pr label`:** labels on pull requests need it. Issues: write is
  not needed and does not replace it. If you only use some commands, the permissions in their rows
  are enough, e.g. Pull requests (read) for `pr list`.

If the organization requires approval for fine-grained tokens, an org owner has to approve the
token before it works.

A fine-grained token can only access the repositories of its resource owner. For several
organizations, create one token per organization and store each under the organization's name,
see below.

## Storing the token in the OS keyring (recommended)

The recommended way is to store the token in your OS keyring, where it is encrypted and unlocked
with your login. As a fine-grained token belongs to a single organization (or user), store one
token per organization and use the organization name as alias:

```sh
ghcli auth login --alias geek-labs   # paste the token, input is hidden
ghcli auth login --alias my-other-org
ghcli auth login                     # alias "default", e.g. for your personal repositories
ghcli auth status
```

```
keyring: GNOME Keyring (Secret Service)
tokens:
  default      from OS keyring (gith…1234)
               user you (ID 123456), 4998 of 5000 API requests left, resets 14:05:12
› geek-labs    from OS keyring (gith…5678)
               user you (ID 123456), 4998 of 5000 API requests left, resets 14:05:12
repository geek-labs/terraform-modules: uses alias "geek-labs" (matches the repository owner)
```

### Which token a repository uses

1. `GHCLI_PAT`, if set: always, for every repository, without looking at aliases
2. `--alias <name>`, if given
3. the token whose alias is the repository owner (`geek-labs` for `geek-labs/terraform-modules`)
4. the only token, if there is just one
5. `default`, if it exists
6. otherwise ghcli asks which one to use

When ghcli falls back to `default` or asks, it says so. A 404 for a repository usually means the
token can't see it; the error names the token used and the `ghcli auth login --alias …` to fix it.

### Where tokens are stored

The token of alias `<alias>` is kept under service `ghcli.github.<alias>.pat` (account `ghcli`),
e.g. `ghcli.github.geek-labs.pat`, in:

- **Linux:** the Secret Service, i.e. GNOME Keyring, KWallet (KDE Frameworks 5.97+) or KeePassXC
  with Secret Service integration enabled
- **macOS:** the login Keychain
- **Windows:** the Credential Manager

Keyrings can't list their entries, so ghcli also keeps the list of aliases under
`ghcli.github.aliases` (no secret in it).

To replace a token, run `ghcli auth login --alias <alias>` again. `ghcli auth logout --alias <alias>`
removes one, `ghcli auth logout --all` removes all.

That's all most setups need. The rest of this page is only for special cases.

## Advanced: `keyring_command`

Prefer the OS keyring above. Use `keyring_command` only if you can't use it, e.g.:

- there is no keyring: SSH sessions, servers and WSL usually have no Secret Service running
- your token already lives in a password manager such as pass or 1Password

`os.keyring_command` is a shell command that prints the token. It goes into `~/.ghcli/config.yml` (or the
file given with `--config`) and takes precedence over the OS keyring. `{alias}` is replaced by the
alias, so one command serves a token per organization:

```yaml
os:
  keyring_command: pass show github/{alias} | head -n 1
```

For `geek-labs/terraform-modules`, ghcli runs `pass show github/geek-labs | head -n 1`; if that
fails, it tries `pass show github/default | head -n 1`. `--alias` picks the alias explicitly.
Without `{alias}`, the command's token is used for every repository.

ghcli runs it with `sh -c` (`cmd /C` on Windows) on every call and trims the output. If the command
fails or prints nothing, ghcli stops with that error instead of trying other sources.

### Supported tools

Store the token once with the command in the middle column, then use the `keyring_command` on the
right. The table shows a single token; for one token per organization, put `{alias}` where the
entry name is, e.g. `pass show github/{alias}` or `op read "op://Private/ghcli-{alias}/credential"`.
Any other command that prints the token works too.

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

If no token is configured at all, ghcli looks for a `default` token in the tools above, except
KeePassXC and the GitHub CLI.
It only tries tools that are installed and, for pass, gpg, age and sops, only if the entry or file
exists. The first command that prints a GitHub token (`github_pat_…`, `ghp_…`, …) is saved as
`keyring_command`, and ghcli tells you:

```
ℹ no token was configured, but one was found via `pass show github/ghcli | head -n 1`.
  Saved it as os.keyring_command in /home/you/.ghcli/config.yml, it will be used from now on.
```

Commands that fail, print something else or don't answer within 10 seconds are skipped. Nothing is
written if no token is found. The saved command has no `{alias}`, so its token is used for every
repository; add `{alias}` to the command for one token per organization.

## Other token sources

ghcli uses the first token it finds, in this order:

1. the `GHCLI_PAT` environment variable, e.g. for CI or a one-off run. It is used for every
   repository, aliases are ignored
2. `os.keyring_command` in the config
3. the OS keyring (`ghcli auth login --alias <alias>`)
4. `github.<alias>.pat` in the config: the token in plain text. Not recommended: ghcli reminds you
   on every run to move it to the OS keyring. If you use it anyway, protect the file with
   `chmod 600 ~/.ghcli/config.yml`:

   ```yaml
   github:
     geek-labs:
       pat: github_pat_…
     default:
       pat: github_pat_…
   ```

## Pull request settings

Defaults of `pr label` and `pr merge` go into `~/.ghcli/config.yml`:

```yaml
pr:
  merge_strategy: merge         # squash (default) or merge
  ready_to_merge_label: ready   # default: done
  merge_block_labels: [wip, do not merge]   # in addition to "hold"
```

- `merge_strategy`: how `pr merge` merges. `--strategy squash|merge` overrides it for a single run.
  If a repository does not allow merge commits, `merge` falls back to squash for that pull request.
- `ready_to_merge_label`: the label marking pull requests as ready to merge. `pr label` preselects
  it, `pr merge --all-ready` and `pr ship` merge every pull request carrying it.
  `--ready-to-merge-label` overrides it for a single `pr merge` or `pr ship` run.
- `merge_block_labels`: labels that keep a pull request from being merged by `pr merge` and `pr ship`
  (case-insensitive). `hold` always blocks and cannot be turned off; the list only adds more labels.
  `pr merge --all-ready` and `pr ship` skip such pull requests, selecting one in `pr merge` is refused.

## Troubleshooting

| Error                                      | Cause                                                                           |
|--------------------------------------------|---------------------------------------------------------------------------------|
| `404 Not Found` when listing PRs           | The token can't see the repository: wrong resource owner, repository not selected, or token not yet approved. Store a token for the organization with `ghcli auth login --alias <org>` |
| `several tokens exist (…), none with an alias matching …` | No token is named after the repository owner and there is no `default`: pass `--alias`, or run interactively to choose |
| `403 Resource not accessible by personal access token` | A permission from [Creating the token](#creating-the-token) is missing |
| `403 Resource not accessible by personal access token (the token needs …)` in `pr label` | Set **Pull requests** to **Read and write** on the token; read is not enough to add labels |
| `403 … (the token needs …)` in other commands | Add the permission GitHub names to the token. If the token already has it, ghcli uses a different token (check `ghcli auth status` and `GHCLI_PAT`), or the changed permissions still wait for the organization's approval |
| `403 API rate limit exceeded for user ID …` | Your account is out of API requests. All your tokens and tools share this limit; wait for the reset |
| `401 Bad credentials`                      | The token is wrong or expired. Check with `ghcli auth status` which source is used |
| `OS keyring unavailable: …dbus…`           | No Secret Service running (SSH, server, WSL): use `GHCLI_PAT`, `os.keyring_command` or `github.<alias>.pat` |
