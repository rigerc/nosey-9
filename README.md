# Nosey

Discover agent skills for the technologies in your workspace, review their sources, and install selected skills for your coding agents.

Nosey provides Scan, Explore, Manage and Settings tabs, plus scriptable CLI commands. Detection stays inside the current directory or `--cwd`, discovers nested projects, honors Git ignores and configured exclusions, and reports incomplete observations explicitly.

## Build and run

Use the Go version declared in [go.mod](go.mod). Git is required for remote registries. Installation uses **Node >=22.20.0**, npm/npx and the pinned **Skills CLI 1.7.6**.

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

Use a skill ID returned by search to review a project install:

```sh
nosey install SKILL_ID --agent codex --dry-run --json
nosey install SKILL_ID --agent codex --yes --approve-plan REVIEWED_SHA256
nosey list --json
```

Review the plan before supplying its `approval.digest`. Additional source approval requires both `--approve-plan` and `--yes`; changed content, revisions, agents or method invalidates the digest. `--copy` selects installation by copy. Agents must be selected explicitly or configured; every TUI skill starts unchecked.

Previews never invoke the installer or prepare persistent local installation sources. For local skills, raw `--print` and TUI command copying remain unavailable until a confirmed execution prepares the immutable source. These sources live in Nosey's application data independently of refresh caches and have no automatic garbage collection. Installation receipts help identify origins; inventory also verifies upstream records, lock hashes and readable payloads.

In the TUI, use Tab to change tabs, arrows to navigate, Space to select, `/` to search, `f`/`s` for filters/sorting and `i` to review all selections, including hidden selections. Escape cancels a dialog; `q` quits.

## Settings and registries

Settings use JSON: user configuration defaults to the OS configuration directory under `nosey/config.json`; project overrides live at `.nosey/config.json`. `--config PATH` selects a separate user configuration. Incompatible legacy settings are rejected without rewriting them.

```sh
nosey config set installation.agents '["codex","claude-code"]'
nosey config set installation.upstreamTelemetry '"disabled"'
nosey config get
nosey registry add /absolute/path/to/registry --id user:local
nosey registry list --json
nosey registry init /absolute/path/to/new-registry --template empty --yes
```

Project-proposed registries require user-owned trust approval. `nosey trust` displays the workspace, proposals and fingerprint for interactive review; `nosey trust --yes` explicitly approves the current proposals noninteractively. Use `nosey trust --revoke` to revoke approval. Changes revoke that approval. Registry trust and installation-plan approval are separate decisions. MCP inspection is opt-in; dotenv evidence includes key names only.

Use each command's `--help` for options. Scan/search/init output remains v2; install plans use v3; list/registry-list/doctor use v1. Contracts and approved decisions are in [docs/prd](docs/prd/).

## Development

```sh
go test ./...
make check
NOSEY_UPSTREAM_INTEGRATION=1 go test ./internal/installation -run TestPinnedUpstreamLocalInstallAndCacheRotation -v
```

`make check` runs formatting, vet/custom lint, race tests, coverage and build. The integration test invokes the real pinned upstream CLI in a disposable project and needs Node/npm and package access. Ordinary tests use local fixtures and fake installers.

Registry import inputs and explicit ID conversions are in `registry-import/`; regenerate with `go run ./scripts/catalog-import --help`. Keep the reviewed external registry commit and `registries/default` gitlink aligned when publishing data updates.

Platform runtime verification and the PRD's human usefulness studies remain release gates; cross-compilation alone does not establish terminal or installer support. See [CONTRIBUTING.md](CONTRIBUTING.md) and [AGENTS.md](AGENTS.md) for development guidance.

## Attribution

Built from [go-cli-template](https://github.com/imdevan/go-cli-template) and [ClackBT](https://github.com/rigerc/clackbt). Preserve [LICENSE](LICENSE) and notices in `internal/clack/`, `third_party/` and `.agents/skills/`. Imported registry provenance and source attributions are retained in the official registry's authoring data.
