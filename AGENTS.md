# Instructions for coding agents

## Scope

ClackBT is a **starter** for Go CLI/TUI applications, not a completed file manager. `greet`, `prompt`, and the directory list are replaceable demonstrations. When working in an **initialized application**, its own `AGENTS.md` takes precedence over these starter instructions.

## Implement first

- Change application code and focused tests to meet the request. Keep Cobra parsing thin, with logic in testable `internal/` packages.
- Keep command descriptions, flags and `--help` accurate. Do not create separate command-reference pages.
- Run `gofmt`, `go test ./...` and the relevant build/checks when available; report unavailable tools honestly.
- For settings changes, update defaults, loading/validation, and affected examples. Don't synchronize multiple Markdown files by default.
- Update README only for onboarding, installation, or substantial user-facing workflow changes, or when specifically requested.
- **Do not generate flow specs, documentation, plans, storyboards or reports** for routine implementation tasks. The `clackbt-flow-spec` skill is opt-in for explicitly requested specification work; its output is ignored in `.work/flow-spec/`.
- Preserve existing licenses, upstream notices, and third-party skill attributions.
- Don't run external sync, publishing, or release automation unless explicitly asked.

## Key locations

- `cmd/go-cli-template/` — Cobra entry point and demonstrations.
- `internal/app/`, `internal/domain/`, `internal/workflow/` — application extension points.
- `internal/config/`, `internal/runtimeconfig/` — settings.
- `internal/ui/` — Bubble Tea UI; `internal/clack/` — bundled prompt runtime.
- `examples/forms/` — optional prompt demonstrations.
- `scripts/setup/` — initialize an application (generates app-specific README/AGENTS).

The Go version is declared in `go.mod`. `make check` needs optional lint tooling. `make skills` is an **explicit** task, not an automatic CI job.
