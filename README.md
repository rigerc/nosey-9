# ClackBT — Go CLI/TUI starter

A code-first starter for [Cobra](https://github.com/spf13/cobra) CLI applications, with [Bubble Tea](https://github.com/charmbracelet/bubbletea) components and bundled Clack-style interactive prompts. The example directory list, `greet`, and `prompt` commands are demonstrations, **not product features or requirements**.

Commands are documented through Cobra `--help`; there is no separate documentation site or per-command Markdown maintenance.

## Quick start

Requires the Go toolchain specified in `go.mod` (Go 1.26.10 in this checkout). `make` is needed for the recipes below.

```sh
go run ./cmd/go-cli-template --help
go run ./cmd/go-cli-template greet Ada
make build                  # dist/go-cli-template
make test                   # race-enabled Go tests
```

For interactive examples, run `go run ./cmd/go-cli-template` (read-only directory list) or `go run ./cmd/go-cli-template prompt` in a terminal. See `examples/forms/` for more prompt demos. `justfile` offers optional recipes with Just and zsh.

## Create an application

From a clean checkout, preview the changes:

```sh
go run ./scripts/setup \
  --name mycli \
  --module github.com/acme/mycli \
  --description "Tools for Acme projects" \
  --author "Acme" \
  --repository https://github.com/acme/mycli
```

Repeat with `--apply` to update files. The tool renames the command directory and Go module/imports, updates metadata/build/release settings, and **replaces the starter README and agent instructions with short, application-specific versions**. It also replaces starter-oriented contribution guidance.

Setup previews changes by default. It refuses destination conflicts and pre-existing changes to affected Git paths, supports identical reruns, and rolls back writes and renames on apply failures. An identical rerun **does not overwrite** subsequent application README or agent edits. Default validation includes formatting checks, Go tests, build, and CLI smoke checks; use `--verify=false` when the toolchain is unavailable. `--check` and `--skills` explicitly enable extra checks or command-skill generation. Git remotes and repository settings are not changed.

After setup, replace example commands, the directory list, configuration defaults and domain scaffolds as needed. Starter examples are not application requirements. Keep licenses and notices under `internal/clack/`, `third_party/`, and agent skills.

## Project map

| Location | Contents |
| --- | --- |
| `cmd/go-cli-template/` | Cobra commands, completion, root list and prompt examples |
| `internal/app/`, `internal/domain/`, `internal/workflow/` | Application composition and business logic extension points |
| `internal/config/`, `internal/runtimeconfig/`, `internal/utils/` | Typed config, environment handling and XDG paths |
| `internal/ui/` | Bubble Tea-based interface components |
| `internal/clack/` | Bundled interactive prompt runtime, with its license |
| `internal/adapters/` | Shell, clipboard, editor, and terminal integrations |
| `examples/forms/` | Standalone prompt gallery |
| `scripts/setup/` | First-run app initializer |

## Development and optional tooling

```sh
go test ./...
make format-check
make build
make check       # optional lint tooling required
make skills      # opt-in: generate this CLI's Agent Skill from Cobra metadata
```

`make check` includes vet, custom lint, race tests, a runtime-example coverage gate and build. `make skills` creates an app-specific CLI skill when explicitly invoked; no CI workflow writes skills back to your branch. CLI help is maintained with the commands themselves, not separately documented.

Configuration examples: [`example-config.toml`](example-config.toml) and [`sample.env`](sample.env). Contributor checks: [CONTRIBUTING.md](CONTRIBUTING.md). For opt-in prompt-flow **specification** work, use `.agents/skills/clackbt-flow-spec/`; its outputs are ignored in `.work/flow-spec/`.

## Attribution

This starter incorporates code originating in [imdevan/go-cli-template](https://github.com/imdevan/go-cli-template), and examples/quality tooling adapted from [rigerc/go-agentic-template](https://github.com/rigerc/go-agentic-template). Its BSD-2-Clause license is at `third_party/go-agentic-template/LICENSE`. The prompt runtime retains its MIT license at `internal/clack/LICENSE`; other bundled material has its own notices. Retain those notices when adapting or redistributing components.
