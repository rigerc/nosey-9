# Nosey

Discover agent skills for the technologies in your workspace, review their sources, and install selected skills for your coding agents.

Nosey provides Scan, Explore, Manage and Settings tabs, plus scriptable CLI commands. Detection stays inside the current directory or `--cwd`, discovers nested projects, honors Git ignores and configured exclusions, and reports incomplete observations explicitly.

## Build and run

Use the Go version declared in [go.mod](go.mod). Git is required for remote registries. Installation uses **Node >=22.20.0** and the pinned **Skills CLI 1.7.6**, from an installed `skills` when its version matches and otherwise through `npx`. Nosey stops with a clear error when neither is available.

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
nosey install SKILL_ID --print
nosey list --json
```

Nosey resolves required skills and blocks catalog conflicts and selections that would install under the same name (`--ignore-conflicts` opts out of conflicts only). It then runs one `skills add` per source, and the Skills CLI does the rest: it asks for scope, install method and agents when they are not set, warns about skills it would overwrite, and asks for confirmation. Git sources use the ref declared by the registry; official catalog sources use pinned commits.

- `--yes` needs target agents, installs into the project without prompts and replaces existing skills with the same name.
- `--print` shows the commands without running anything; `--copy` installs by copy.
- Cancelling a prompt skips that source. Nosey stops at the first source that fails. Upstream can enable noninteractive mode automatically inside AI agents.
- Installed state comes from `skills list --json` and `skills-lock.json`. Skills behind the catalog's pin show as outdated; install them again to update. `skills update` does not move a pinned skill.

To install from a source that is not in a registry, run `npx skills add` directly.

In the TUI, use Tab to change tabs, arrows to navigate, Space to select, `/` to search, `f`/`s` for filters/sorting and `i` to install all selections, including hidden selections. Escape cancels a dialog; `q` quits.

## Settings and registries

Settings use JSON: user configuration defaults to the OS configuration directory under `nosey/config.json`; project overrides live at `.nosey/config.json`. `--config PATH` selects a separate user configuration. Incompatible legacy settings are rejected without rewriting them.

```sh
nosey config set installation.agents '["codex","claude-code"]'
nosey config set installation.upstreamTelemetry '"disabled"'
nosey config get
nosey registry add /absolute/path/to/registry --id user:local
nosey registry list --json
nosey registry init /absolute/path/to/new-registry --ci github --yes
```

`registry init` creates an empty, regenerable registry from the template provider's `empty` template; `--ci github` adds a validation/regeneration workflow, and `--github OWNER/NAME [--public]` commits it and creates and pushes a GitHub repository with the `gh` CLI (private by default; needs a git identity and `gh auth login`). Run `nosey registry generate PATH` after editing its JSON files (`--check` verifies without writing; `nosey registry refresh PATH` updates files that declare an upstream).

Project-proposed registries require user-owned trust approval. `nosey trust` displays the workspace, proposals and fingerprint for interactive review; `nosey trust --yes` explicitly approves the current proposals noninteractively. Use `nosey trust --revoke` to revoke approval. Changes revoke that approval. MCP inspection is opt-in; dotenv evidence includes key names only.

Use each command's `--help` for options. Scan/search/init output remains v2; list/registry-list/doctor use v1. Contracts and approved decisions are in [docs/prd](docs/prd/).

## Development

```sh
go test ./...
make check
NOSEY_UPSTREAM_INTEGRATION=1 go test ./internal/installation -run '^TestUpstream' -v
```

`make check` runs formatting, vet/custom lint, race tests, coverage and build. The integration tests invoke the real pinned upstream CLI in disposable projects and need Node/npm plus network access. Ordinary tests use local fixtures and fake installers.

Official registry data lives in `registries/default/`. Edit the root JSON documents, then run `nosey registry generate registries/default` to canonicalize them and refresh `nosey.registry.json` and `nosey.registry.lock.json`. `make registry-check` verifies the committed output.

Platform runtime verification and the PRD's human usefulness studies remain release gates; cross-compilation alone does not establish terminal or installer support. See [CONTRIBUTING.md](CONTRIBUTING.md) and [AGENTS.md](AGENTS.md) for development guidance.

## License

Nosey's code is MIT-licensed ([LICENSE](LICENSE)). The registry data in `rigerc/nosey-registry` is attributed separately; this license covers the application code only.

## Attribution

Built from [go-cli-template](https://github.com/imdevan/go-cli-template) and [ClackBT](https://github.com/rigerc/clackbt). Preserve [LICENSE](LICENSE) and notices in `internal/clack/`, `third_party/` and `.agents/skills/`. Imported registry provenance and source attributions are retained in the official registry's data.
