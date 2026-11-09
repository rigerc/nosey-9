# Go / `rigerc/clackbt` implementation integration

[← Specification index](../README.md) · [Architecture foundation](09-architecture-foundation.md) · [Delivery gates](14-nonfunctional-and-validation.md)

**Decision:** Native Go implementation from `rigerc/clackbt`, explicitly chosen by the product owner. **Evidence:** read-only GitHub connector inspection of accessible private repository `main` on 2026-11-07: `README.md`, `go.mod`, `AGENTS.md`, `Makefile`, `.github/workflows/ci.yml` and repository-indexed paths. **Not done:** clone, setup, code changes, builds, execution, tests, CI, installer verification or write access to the repository. Freeze a commit before coding because `main` is mutable.

**Local integration note (draft.3):** This workspace already contains the starter; record its actual baseline (`0c41bf4a3927ae0d590f17424546d1f7f5ddd468`, current local HEAD when this update was prepared) and upstream provenance rather than making another checkout solely for this guide. The historical read-only inspection statement above belongs to the supplied archive. Earlier local `go test ./...` and `make format-check build` passed for this unchanged starter, not for Nosey functionality. Setup and registry/submodule creation remain planned.

## Starter components and Nosey integration

| Starter component / verified location | What exists in the template | Proposed Nosey use |
|---|---|---|
| [`cmd/go-cli-template/`](https://github.com/rigerc/clackbt/tree/main/cmd/go-cli-template) | Cobra root, commands, completion, read-only directory-list demonstration | After setup becomes `cmd/nosey/`; replace demos with `scan`, `search`, `install`, `registry`, `list`, `doctor`; bare `nosey` starts real Scan TUI |
| `internal/app/`, `internal/domain/`, `internal/workflow/` | Application and business-logic extension points | Compose scan/search/install use cases; pure detection/registry/mapping/planner logic |
| [`internal/ui/`](https://github.com/rigerc/clackbt/tree/main/internal/ui) | Full-screen Bubble Tea v2 UI example | Four-section Scan / Explore / Manage / Settings with single-column layout and explicit focus/state ownership |
| [`internal/clack/`](https://github.com/rigerc/clackbt/tree/main/internal/clack) | Bundled, internally licensed Clack-style prompt runtime | Inline registries setup, trust approval, install confirmation, progress; use separate workflows, not nested `.Run()` inside an active Bubble Tea loop |
| `internal/config/`, `internal/runtimeconfig/`, `internal/utils/` | Typed config/environment/XDG extension points | User/workspace settings, paths and source trust state; preserve effective settings merge semantics |
| `internal/adapters/` | Shell/clipboard/editor/terminal extension points | Go `exec.CommandContext` Git execution and the in-tree `internal/skillkit` installer; file caches, bounded observations and adapters |
| [`scripts/setup/`](https://github.com/rigerc/clackbt/tree/main/scripts/setup) | Preview-by-default starter initializer with `--apply`, rename and validation | Initialize fresh Nosey checkout exactly once and inspect diff before proceeding |
| [`Makefile`](https://github.com/rigerc/clackbt/blob/main/Makefile), CI | Format, race tests, coverage, build, security/dependency workflows | Rename binary targets; add Nosey schema/negative tests; retain source quality/security gates |

**Observed dependency baseline** (`go.mod`, read-only): `go 1.26.10`; `github.com/spf13/cobra v1.10.2`; `charm.land/bubbletea/v2 v2.1.0`; `charm.land/bubbles/v2 v2.2.1`; `charm.land/lipgloss/v2 v2.0.6`. Treat them as starter facts, not proof of successful Nosey compilation.

## Bootstrap sequence (commands are reference only; not executed)

1. Record the existing application checkout baseline and its private `rigerc/clackbt` provenance/commit where available. Preserve licenses; do not restart this project in a second checkout merely to follow bootstrap instructions.
2. Run the starter initializer in **preview** mode using a confirmed future module path; do not invent the user's destination repository:

   ```sh
   go run ./scripts/setup --name nosey --module github.com/OWNER/nosey \
     --description "Agent skill discovery and installation planning" \
     --author "AUTHOR" --repository https://github.com/OWNER/nosey
   ```

3. Review files and rename conflicts. Re-run with `--apply` on an authorized clean checkout, substituting actual owner/module values. The starter says it replaces its README/AGENTS/CONTRIBUTING with app-specific versions, but does not change Git remotes or repository settings.
4. Remove example `greet`, `prompt` and directory-list behavior from the **product routing**, retaining any reusable UI/Clack components and their license notices. Replace root help and file paths with Nosey commands; avoid duplicate standalone command-reference Markdown because Cobra help is authoritative.
5. Create the default registry repository and exact `registries/default/` development submodule through [P1R / M1R](../IMPLEMENTATION_PLAN.md#m1r--create-the-official-registry-repository-and-development-submodule) once identity and local validation are available. Keep app builds independent of that checkout, exclude its payloads from releases, and test runtime remote/cache fetch separately. No recursive initialization of fetched registry submodules.
6. First vertical slice: `nosey scan --json` using a trusted local example catalog → JSON Schema output v2 → initial fullscreen Scan projection. Use offline fake Git/provider/process runners before real remote and installation integration.
7. Install mode only after `internal/skillkit` goldens (captured from `skills@1.7.6`), agent identity mapping, and project-only per-skill verification are demonstrated in disposable workspaces.

**License handling:** preserve `internal/clack/LICENSE`, `third_party/` and all other starter/upstream notices. Do not copy proprietary private source into the PRD package; this guide describes read-only observations only.

Approved draft.4 [D1–D7](17-approved-decisions.md) additionally require all seven versioned JSON outputs, config get/set, exact-root scanning, effective-invalidation isolation, persistent installation sources and SHA-256 reviewed-plan consent. Implement the pinned catalog conversion/crosswalk before official registry publication.

## Runtime and API boundaries

- **Cobra**: argument validation, flag binding, documented `--help`, noninteractive exit codes, deterministic JSON stdout. `--json` never launches Bubble Tea or inline prompts; human diagnostics are stderr only.
- **Bubble Tea v2**: exclusively owns the fullscreen session, selection, dynamic footer, focus and screen state. `Update` handlers issue effects as commands/messages; no blocking Git fetch or install subprocess in rendering logic.
- **Clack inline prompts**: used outside an active fullscreen event loop for explicit confirmation, registry init and optional scripted-wizard input. A cancellation is not an affirmative selection. Non-TTY execution must never prompt; it uses plan/print/dry-run or explicit `--yes` with preflight.
- **Pure domain**: registry field-level merge; immutable effective snapshots; coverage state machine; three-valued conditions; ranking, sort/filter, dependency/conflict graph and install plan. No terminal imports, no process execution.
- **Adapters**: `context.Context`, `exec.CommandContext` with explicit argv, bounded output, scrubbed env, project root `Dir`; Git hardening, path confinement, atomic file writes; controlled JSON Schema validator for Draft 2020-12 (select and test compatible Go library before freezing).
- **Persistence**: XDG user config/cache and workspace `.nosey` files per source contract; atomic snapshots and trust approvals. Native single binary does not depend on Node; the external Skills CLI launcher may.

## Quality and release checkpoints

| Check | Requirement |
|---|---|
| Starter bootstrap | Confirm pinned SHA, `scripts/setup` preview/apply behavior, module imports and licenses; verify no accidental mutations to `rigerc/clackbt` |
| Go quality | Run `gofmt`, `go vet ./...`, `go test ./...`, `go test -race ./...`, build and applicable starter `make check` on initialized Nosey app; report exact results |
| CLI contracts | Golden `--help`/exit tests, stdout JSON v2 schemas, `--cwd`, non-TTY behavior, Cobra parsing of all PRD subcommands |
| TUI contracts | Bubble Tea v2 model/update/focus and 60-/80-column tests; keyboard-only and `NO_COLOR`; Clack confirmation/cancel tests without nested loops |
| Security | Fuzz/ref/path escaping, fake process runner verifying argv and env, workspace registry trust, adapter pin checks and atomic writes |
| Installer | **Real** `skills@1.7.6` disposable-project runs for ref grammar, add/list JSON, agent mapping, logical-origin reconciliation and false-installed rejection |
| Release | Native build targets, checksums, licenses, dependency review, `govulncheck`, CI security action status, explicit G1–G7 signoffs |

## Open technical decisions

- Exact immutable `rigerc/clackbt` commit to base the app on, resulting public/private Nosey repository/module path and permitted checkout for initialization.
- Version-pinned Go schema validator supporting the supplied Draft 2020-12 features and separately tested semantic constraints.
- Installer distribution: resolved by [D9](17-approved-decisions.md#d9--native-installation-engine); Nosey installs natively and needs no launcher.
- Supported operating systems, terminal matrix, binaries/releases and benchmark runner.
- Whether to retain the starter's optional command-skill generator; it is not necessary for Nosey's MVP and must remain opt-in.
