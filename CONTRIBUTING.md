# Contributing

ClackBT is a reusable CLI/TUI starter. Prioritize working Go code, focused tests and a usable first-run application experience over documentation maintenance.

## Local checks

```sh
go test ./...
make format-check
make build
make check    # requires the pinned golangci-lint custom builder
```

`go.mod` declares the required Go version; if it cannot be installed, report that limitation. `make check` runs gofmt verification, `go vet`, custom lint, race tests, a coverage gate for runtime example packages, and the build. `make audit` optionally uses `govulncheck`.

## Changes

- Implement changes in `cmd/` and testable `internal/` packages. Keep Cobra help and flags truthful.
- Add meaningful success and error-path tests. For config changes, update loading/validation, defaults and sample values that actually change.
- Update README for onboarding or major user workflows only; do **not** update multiple Markdown files for every new command.
- Preserve `internal/clack/LICENSE`, `third_party/` notices and licenses/attribution in `.agents/` and `.claude/`.
- Do not use the opt-in flow-spec skill, `make skills`, `just sync`, tags, releases or publishing unless the change explicitly requires it.

After `scripts/setup`, `AGENTS.md`, `README.md`, and `CONTRIBUTING.md` describe the initialized application. See [README.md](README.md) for first-run instructions.
