# ghcli

`ghcli` is a command line tool for automating recurring GitHub chores, the kind that take many
clicks in the web UI, like merging a batch of finished pull requests.

Compared to the existing `gh` cli offered by github, which is a great tool, it offers more high-level workflows,
interactive selections and thus. So you might want to use is a complimentary tool, not a replacement.

One good example is batch-merging PR's without using the "merge-queue" feature. You cli will poll and finish the job
the PR becomes mergeable.


## Installation

**Prebuilt binary** from the [latest release](https://github.com/EugenMayer/ghcli/releases/latest),
for Linux and macOS on amd64 and arm64:

```sh
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/')
sudo curl -fsSL -o /usr/local/bin/ghcli \
  "https://github.com/EugenMayer/ghcli/releases/latest/download/ghcli-${OS}-${ARCH}"
sudo chmod +x /usr/local/bin/ghcli
ghcli --version
```

Or use go

```bash
go install github.com/EugenMayer/ghcli@latest   # or a specific version, e.g. @v0.0.1
```

For Windows, download `ghcli-windows-amd64.exe` or `ghcli-windows-arm64.exe` from the release.
Each release also has a `checksums.txt` with SHA-256 sums of all binaries.

**With Go 1.27** (an older Go downloads 1.27 automatically). The binary is installed to
`$(go env GOPATH)/bin`, usually `~/go/bin`, which must be on your `PATH`:

## Commands

| Command             | Description                                                          |
|---------------------|----------------------------------------------------------------------|
| `ghcli pr list`     | List open pull requests as a table                                   |
| `ghcli pr label`    | Pick a label (`done` preselected), then label PRs one by one or all with ctrl+a |
| `ghcli pr merge`    | Pick an open pull request and merge it                               |
| `ghcli pr merge --all-done` | Merge all PRs labeled `done` one by one, waiting until each is mergeable |
| `ghcli auth login [--alias org]`  | Store a GitHub token in the OS keyring, one per organization |
| `ghcli auth logout [--alias org \| --all]` | Remove a token (or all) from the OS keyring  |
| `ghcli auth status` | Show all tokens, their accounts and API rate limits, and which one the current repository uses |

```sh
ghcli pr list
ghcli pr merge --all-done --strategy merge -R geek-labs/terraform-modules
```

Run `ghcli --help` or `ghcli <command> --help` to see all flags.

Without `--repo`, ghcli uses the `origin` remote of the current directory. SSH aliases work too:
`github:org/repo` is read as organization `org`, repository `repo`.

`--all-done` ignores review status. Before each merge it waits while GitHub computes whether the PR
can be merged or required checks run, and updates branches that are behind their base. PRs with
conflicts or that time out are skipped. `pr merge` squashes by default; `--strategy merge`
creates a merge commit instead, and `pr.merge_strategy` in the config changes the default (see
[configuration](README.configuration.md#merge-strategy)). If the repository does not allow merge commits,
the merge strategy falls back to squash. Branch protection still applies: if reviews are required,
the merge only succeeds when your user may bypass the protection.

## Configuration

ghcli needs a fine-grained GitHub token with **Pull requests** and **Contents** set to read and
write. Pull requests must be **write** for `pr label` and `pr merge`, read is not enough (see the
[permissions per command](README.configuration.md#creating-the-token)). A fine-grained token belongs to one organization (or user), so you store one token per
organization, named after it. Store them in your OS keyring, which is the recommended way (GNOME
Keyring or KWallet on Linux, Keychain on macOS, Credential Manager on Windows):

```sh
ghcli auth login --alias geek-labs   # prints a pre-filled link to create the token, then asks for it
ghcli auth login                     # alias "default": for every other repository
ghcli auth status                    # all tokens, and which one the current repository uses
```

ghcli picks the token for a repository by its owner: in `geek-labs/terraform-modules` the token
`geek-labs` is used. If there is no token with that name, the only token or `default` is used,
otherwise ghcli asks. `--alias` picks a token explicitly, and the `GHCLI_PAT` environment variable
overrides everything and is used for every repository, e.g. in CI.

No keyring (SSH, servers, WSL), or your tokens are already in a password manager such as pass,
gopass, 1Password or sops? Use [`os.keyring_command`](README.configuration.md#advanced-keyring_command)
instead. See [README.configuration.md](README.configuration.md) for creating the token, all
supported tools and troubleshooting.

### Build and customize

```sh
make build    # ./dist/ghcli for the current platform
make install  # build and copy to /usr/local/bin/ghcli (uses sudo)
make dist     # all release platforms + checksums.txt into ./dist
make test     # run the tests
```

### CI and releases

- `.github/workflows/build.yml` runs on pushes to `main` and on pull requests: it checks formatting
  and runs vet, the tests and a full cross-platform build.
- `.github/workflows/release.yml` runs when a `v*` tag is pushed. It builds all platforms and
  creates a GitHub release with the binaries and checksums.

```sh
git tag v0.0.1 && git push origin v0.0.1
```

## Reusing the keychain in other projects

The OS keyring handling is a standalone Go package, [`pkg/keychain`](pkg/keychain/README.md): store
and fetch secrets by path, resolve them from env/commands/keyring, reference them from config
values (`token: keychain:jira.token`) and add `secret set|get|delete|status` commands to any cobra
CLI.
