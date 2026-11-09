# Nosey

Discover agent skills for the technologies in your workspace, review their sources, and install selected skills for your coding agents.

Nosey provides Scan, Explore, Manage and Settings tabs, plus scriptable CLI commands. Detection stays inside the current directory or `--cwd`, discovers nested projects, honors Git ignores and configured exclusions, and reports incomplete observations explicitly.

## Build and run

Use the Go version declared in [go.mod](go.mod). Git and `gh` are not required at runtime: Nosey talks to Git and GitHub natively, so remote registries and skills from Git sources need neither executable. Git is only needed to develop Nosey (fixtures and hooks). Nosey installs skills itself: it needs no Node.js, npm or `skills` CLI at runtime. Node is only used by the registry studio's UI tests.

```sh
git clone --recurse-submodules https://github.com/rigerc/nosey-9.git
cd nosey-9
make build
./dist/nosey --help
./dist/nosey --cwd /path/to/project
```

For an existing checkout, run `git submodule update --init registries/default`. The submodule pins the official authoring data for development; the application fetches the external [official registry](https://github.com/rigerc/nosey-registry) and retains validated snapshots for offline use. First use needs a reachable registry or an explicitly configured local source.

## Discover and install

```sh
nosey --cwd /path/to/project scan --json
nosey --cwd /path/to/project search nextjs --json
nosey --cwd /path/to/project doctor --json
```

Install skills by the ID returned from search:

```sh
nosey install SKILL_ID
nosey install SKILL_ID --agent codex --agent claude-code
nosey install SKILL_ID --agent codex --yes
nosey list --json
```

Nosey resolves required skills and blocks catalog conflicts and selections that would install under the same name (`--ignore-conflicts` opts out of conflicts only). Nosey asks for target agents once when none are configured, then shows the whole selection and asks for one confirmation. After approval it fetches each source once and installs without further prompts, in dependency order. All installs target the project, replace existing skills with the same names and are recorded in `skills-lock.json`, which stays compatible with `npx skills`. Git sources use the ref declared by the registry; official catalog sources use pinned commits.

- `--yes` needs target agents, installs into the project without prompts and replaces existing skills with the same name.
- `--copy` installs by copy.
- Cancelling the Nosey confirmation installs nothing. Nosey stops at the first source that fails, keeps and records what already installed, and reports the sources it did not start (exit code 5).
- In symlink mode, agents whose project directory does not exist (for example `.windsurf`) are skipped; Nosey lists every skipped agent. `--copy` installs into all selected agents.
- Only one Nosey install runs per project at a time; a second one stops with a message instead of waiting indefinitely.
- Installed state comes from the agent skill directories and `skills-lock.json`. Skills behind the catalog's pin show as outdated. `nosey list --check-updates` fetches each installed skill's source and compares content.

## Update and uninstall

```sh
nosey update                      # every outdated catalog-managed skill
nosey update SKILL_ID --yes
nosey uninstall NAME_OR_ID...
nosey uninstall NAME_OR_ID --ignore-requirements --yes
```

`nosey update [SKILL_ID...] [--ignore-conflicts] [--yes]` updates skills that follow the catalog. A skill is outdated when the catalog pin differs from the ref in `skills-lock.json` or when the upstream content differs from what is installed; the confirmation and the results state which. Each skill is refreshed at every location where it is installed in the project, keeping each location's symlink or copy mode. Requirements the catalog added are installed where the skills requiring them live. Skills outside the catalog are listed as skipped and never updated. The check needs network access, and a failed check stops the command without changing anything. Sources update in dependency order and stop at the first failure; completed updates stay recorded.

`nosey uninstall NAME_OR_ID... [--ignore-requirements] [--yes]` removes skills tracked in `skills-lock.json` from every agent directory and deletes their lock entries. Targets are tracked names or catalog IDs; untracked or unknown targets are refused (exit code 2). The whole selection is checked together: removal is blocked while a skill that stays installed requires a target unless `--ignore-requirements` is passed, and a skill selected together with what it requires is removed first. Removal stops at the first failure, keeps lock entries of skills that were not fully removed and reports what was not started.

Both commands show the whole selection, confirm once, need `--yes` without a terminal, hold the same per-project operation lock as `install`, and have no JSON output. Exit codes: 2 policy or validation (including a busy workspace), 5 operation failure or partial success, 6 missing prerequisite.

Projects installed with Nosey and with `npx skills` interoperate: both read and write the same agent directories and `skills-lock.json`. To install from a source that is not in a registry, run `npx skills add` directly.

In the TUI, use Tab to change tabs, arrows to navigate, Space to select, `/` to search, `f`/`s` for filters/sorting and `i` to install all selections, including hidden selections. On the Manage tab, `u` checks installed skills for updates, `U` updates the outdated catalog skill under the cursor and `d` removes the tracked skill under the cursor, each with one confirmation. Escape cancels a dialog; `q` quits.

## Settings and registries

Settings use JSON: user configuration defaults to the OS configuration directory under `nosey/config.json`; project overrides live at `.nosey/config.json`. `--config PATH` selects a separate user configuration. Incompatible legacy settings are rejected without rewriting them.

```sh
nosey config set installation.agents '["codex","claude-code"]'
nosey config set installation.method '"copy"'
nosey config get
nosey registry add /absolute/path/to/registry --id user:local
nosey registry list --json
nosey registry init /absolute/path/to/new-registry --ci github --yes
```

`registry init` creates an empty, regenerable registry from the template provider's `empty` template; `--ci github` adds a validation/regeneration workflow, and `--github OWNER/NAME [--public]` commits it and creates and pushes a GitHub repository (private by default; needs a Git identity and a token from `GITHUB_TOKEN`, `GH_TOKEN` or `gh auth login`, with the `workflow` scope when combined with `--ci github`). Run `nosey registry generate PATH` after editing its JSON files (`--check` verifies without writing; `nosey registry refresh PATH` updates files that declare an upstream).

Project-proposed registries require user-owned trust approval. `nosey trust` displays the workspace, proposals and fingerprint for interactive review; `nosey trust --yes` explicitly approves the current proposals noninteractively. Use `nosey trust --revoke` to revoke approval. Changes revoke that approval. MCP inspection is opt-in; dotenv evidence includes key names only.

Use each command's `--help` for options. Scan/search/init output remains v2; list/registry-list/doctor use v1. Contracts and approved decisions are in [docs/prd](docs/prd/).

## Development

```sh
go test ./...
make check
NOSEY_SKILLKIT_INTEGRATION=1 go test ./internal/installation -run '^TestPinned' -v
```

`make check` runs formatting, vet/custom lint, race tests, coverage and build. The gated integration test installs the current official pin from GitHub into a disposable project and needs network access. Ordinary tests need neither Node nor network: they use local repositories and fake fetchers, and need Git only for those fixtures. `make native-runtime-check` runs the built binary with neither `git` nor `gh` on `PATH`.

Official registry data lives in `registries/default/`. Edit the root JSON documents, then run `nosey registry generate registries/default` to canonicalize them and refresh `nosey.registry.json` and `nosey.registry.lock.json`. `make registry-check` verifies the committed output.

Platform runtime verification and the PRD's human usefulness studies remain release gates; cross-compilation alone does not establish terminal or installer support. See [CONTRIBUTING.md](CONTRIBUTING.md) and [AGENTS.md](AGENTS.md) for development guidance.

## Troubleshooting

`nosey doctor` reports the native transports and which credential sources are available, without printing tokens. Missing credentials are fine for public repositories.

- **GitHub credentials over HTTPS and the API:** `GITHUB_TOKEN`, then `GH_TOKEN`, then the token `gh auth login` stored (read from `hosts.yml` or the OS keyring, without running `gh`). Enterprise hosts use `GH_ENTERPRISE_TOKEN`, then `GITHUB_ENTERPRISE_TOKEN`, then the stored token for that host; the host must be `GH_HOST` or listed in `gh`'s `hosts.yml`. A token is only sent to the host it belongs to.
- **SSH:** the SSH agent, then the unencrypted default keys `~/.ssh/id_ed25519`, `id_ecdsa` and `id_rsa`, with `known_hosts` verification.
- **Git configuration:** `insteadOf` URL rewrites and credential helpers are not used. Use the real URL and one of the credentials above.
- **Unsupported repositories and origins:** SHA-256 repositories, reftable repositories and local-path or `file://` origins return an error.

## License

Nosey's code is MIT-licensed ([LICENSE](LICENSE)). The registry data in `rigerc/nosey-registry` is attributed separately; this license covers the application code only.

## Attribution

Built from [go-cli-template](https://github.com/imdevan/go-cli-template) and [ClackBT](https://github.com/rigerc/clackbt). Preserve [LICENSE](LICENSE) and notices in `internal/clack/`, `third_party/` and `.agents/skills/`. Imported registry provenance and source attributions are retained in the official registry's data.
