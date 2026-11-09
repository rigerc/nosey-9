# Architecture: foundation, trust & Go packages

[← Specification index](../README.md) · [Previous](08-settings.md) · [Next](09b-architecture-data-and-flows.md)

## 12. Technical architecture

**Approved implementation direction:** Go, starting from private [`rigerc/clackbt`](https://github.com/rigerc/clackbt), with Cobra for commands, Bubble Tea v2 for the full-screen four-tab TUI, and bundled Clack-style prompts for focused inline approvals and wizard flows. The app is a local-only compiled executable. Keep domain/use-case code independent of terminal rendering and make every I/O adapter injectable. The starter is a **template**, not an existing Nosey implementation.

```text
Cobra root/subcommands      Bubble Tea v2 full-screen TUI
        |                             |
        +------> Go application use cases <------- Clack inline prompt flows
                           |
                pure domain engines + ports
           (registry, detection, mapping, plan)
                           |
       +-------------------+---------------------+
       |                   |                     |
 Go Git/cache/config   bounded workspace   exec.CommandContext
   adapters             observation         skillkit installer
       |                   |                     |
        +------ local snapshots, receipts, diagnostics
```

**Go ownership mapping:** `cmd/nosey/` for thin Cobra construction; existing `internal/app/`, `internal/domain/`, `internal/workflow/` for orchestration and policies; `internal/ui/` for the Bubble Tea product TUI; `internal/clack/` for the starter's retained prompt runtime; `internal/config/` and `internal/runtimeconfig/` for typed settings and XDG conventions; `internal/adapters/` for process, Git and terminal boundaries. New cohesive packages may be added under `internal/` without creating a second framework. The starter's `greet`, `prompt` and root directory-list examples must be replaced, not treated as product requirements. See [Go template integration](go-clackbt-integration.md) for the source-verified adoption plan.

**Internal entities, proposed Go types (not additional public wire fields):** `Technology`, `Detection`, `RuleEvaluation`, `DetectorCapabilities`, `DetectionEvidence`, `Skill`, `Repository`, `SearchGroup`, `Recommendation`, `ScanCoverage`, `InstallPlan`, `RegistrySnapshot`. Use named string enum types for `detected|absent|unknown`, `matched|not-matched|not-evaluated` and install verification states. Keep JSON shape/version authoritative in the 16 supplied schemas. Preserve exact case-sensitive technology IDs and the distinction between catalog skill ID and upstream source skill name.

### 12.1 Architecture goals, principles and deployment model

**Go + ClackBT chosen; integration design proposed.** Initialize a new Nosey application from `rigerc/clackbt` using its documented `go run ./scripts/setup` (preview first, `--apply` only in the intended application checkout), then replace demo commands/UI with Nosey workflows. As inspected on 2026-11-07, starter `go.mod` declares **Go 1.26.10**, `charm.land/bubbletea/v2 v2.1.0`, `charm.land/bubbles/v2 v2.2.1`, `charm.land/lipgloss/v2 v2.0.6`, and `github.com/spf13/cobra v1.10.2`. These are **observed starter dependencies**, not a claim of verified compatibility with Nosey's planned features; freeze an approved starter commit and module version matrix before implementation. Future dependency upgrades require integration tests.

No daemon, browser service, REST server, account server, SQL database or hosted backend is needed. The release is a native Go binary per supported OS/architecture with a local user/workspace config, validated filesystem cache, and explicitly launched external Git/Skills CLI processes. Use `context.Context` for cancellation and bounded operations, standard-library `os/exec` argv execution, embedded/read-only *schemas* if convenient, and **no embedded official registry data or scaffold templates**. Unlike the old source recommendation, Node.js is **not** required to run Nosey scan/search; Node/npm may be required by the external Skills CLI launcher chosen for installation.

Architecture priorities: **(1) deliberate install intent/source trust, (2) deterministic and explainable outputs, (3) explicit unknown/coverage states, (4) stable JSON contracts, (5) fast local operation, (6) portable native packaging**. Go package structure should follow existing starter extension points and avoid over-abstraction. Accessibility and terminal behavior belong in UI adapters, never the core detection logic.

### 12.2 Component and trust-boundary diagram

```text
                  UNTRUSTED EXTERNAL DATA / PROCESSES
    Git registry remotes         Skill Git/local sources      Git (fetch only)
           |                              |                        ^  |
           v                              v                        |  v
    +-----------------+       +----------------------+       +------------------+
    | Hardened Git    |       | Preflight + source   |------>| argv-only child  |
    | fetch adapter   |       |/ref validation       |       | process adapter  |
    +--------+--------+       +-----------+----------+       +--------+---------+
             |                            |                           |
       TRUST/VALIDATION BOUNDARY          |                   project agent dirs
             v                            |                   + origin receipt
   +---------------------+                |
   | immutable validated |                |
   | registry snapshots  |                |
   +----------+----------+                |
              v                           |
  +-------------------------------+       |
  | source decode -> overlay merge|       |
  | -> effective schema + semantic|       |
  | validation -> provenance index|       |
  +---------------+---------------+       |
                  |                       |
      +-----------+-------------+         |
      v                         v         |
 +------------------+  +------------------------+
 | bounded detection|  | skill index / mapping  |
 | + coverage graph |->| and 3-state evaluation | 
 +------------------+  +------------+-----------+
                                   |            |
                          +--------+------------+------+
                          | application use cases     |
                          | scan/search/install/init  |
                          +--------+------------+------+
                                   |            |
                              CLI JSON       TUI state
                              stderr         one-column UI

   TRUSTED LOCAL CONTROL: user config, approved workspace fingerprint,
   explicitly trusted user adapters, versioned schemas and source-policy code.
```

**Boundary A — data ingress:** Every Git/local registry and its template/skill metadata is untrusted until path-constrained, bounded, decoded, schema-validated and semantically reconciled. Git transport credentials remain owned by the user's Git helper. **Boundary B — execution:** No catalog metadata can run code. Only an explicit user-confirmed install invokes the pinned external installer, and only a user-installed, hash-pinned adapter can run detector code. **Boundary C — user workspace:** Read scanning is bounded; project skill files and Nosey attribution receipts are written only by explicit installation or initialization actions. **Boundary D — presentation:** Evidence, filenames, registry descriptions, subprocess output and terminal control characters are escaped and secret-minimized before display/logging.

### 12.3 Module decomposition and dependency direction

| Layer | Module or port | Responsibilities | Key outputs |
|---|---|---|---|
| Presentation | `cli`, `tui`, view-state adapters | Parse args, keymap, accessibility, prompts, dialogs, JSON + stderr; no business rules | Render models or machine outputs |
| Application | `scan-service`, `search-service`, `install-service`, `registry-service`, `settings-service`, `doctor-service` | Order use-case steps, policies, cancellation and diagnostics | Result envelopes and plans |
| Pure domain | `registry-merger`, `technology-normalizer`, `condition-evaluator`, `recommendation-engine`, `install-planner`, `comparators` | Deterministic transforms; no process spawning | Effective entities, detection state, plans |
| Domain policy | `coverage-aggregator`, `trust-policy`, `source-policy`, `provenance-resolver` | Invariants, uncertainty, identity and authorization | Explicit states and blockers |
| Infrastructure | `git-provider`, `local-provider`, `snapshot-cache`, `schema-validator`, `filesystem-walker`, ecosystem observers | External and filesystem I/O behind typed ports | Validated snapshots and bounded observations |
| External boundary | `skills-cli-adapter`, `installed-skill-reader`, `process-runner`, `adapter-runner` | Exact argv, version pin, source reconciliation, per-skill evidence | Observed outcomes; no optimistic success |

**Go dependency rule:** Cobra/Clack/Bubble Tea presentation → application workflows → pure domain; infrastructure implements narrow consumer-defined ports. Use `context.Context` and stable DTOs through interface boundaries. Keep `internal/clack` prompt models out of a running fullscreen Bubble Tea event loop; launch prompts as separate terminal flows and hand outcomes back to use cases. **Dependency rule:** presentation → application → domain ports; infrastructure implements domain ports. Avoid importing TUI code into detectors or decision engines. Inject `Clock`, `Filesystem`, `ProcessRunner`, `RegistryProvider`, `InstalledSkillReader`, `Environment` and `Logger` for deterministic unit/integration tests. Keep technology data schemas and CLI schemas as separately versioned public contracts; never reuse renderer-only types as wire formats.

### 12.4 Proposed repository and package organization

This tree expresses architectural boundaries, not a second mandatory package hierarchy. For this checkout, use the concrete [repository implementation map](../IMPLEMENTATION_PLAN.md#3-proposed-go-boundaries) and [phase comparison](../IMPLEMENTATION_PLAN.md#8-comparison-with-the-supplied-go-prd-implementation-plan); do not implement duplicate nested/top-level engines or contract packages. The default registry is a separate repository with a pinned development submodule under [§6](04-registry-contracts.md#default-registry-repository-and-development-submodule), outside the runtime package tree.

The following maps Nosey packages onto **observed ClackBT template directories**, with names below `internal/` for proposed Nosey code. It is a future target, **not evidence these files already exist** in `rigerc/clackbt`:

```text
cmd/nosey/                         # after setup: Cobra root and command wiring
  main.go                          # generated/renamed entry point
  root.go                          # bare nosey -> Bubble Tea; --help -> Cobra
  scan.go, search.go, install.go, registry.go, doctor.go
internal/app/                      # composition root and application services
internal/domain/                   # pure registry/detection/mapping/plan types, invariants
  registry/, detection/, recommendation/, installation/
internal/workflow/                 # scan, search, refresh, init, install orchestration
internal/config/                  # typed user/workspace configuration
internal/runtimeconfig/           # XDG paths, env-derived defaults, runtime options
internal/ui/                      # Bubble Tea v2 Scan / Explore / Manage / Settings
internal/clack/                   # retained bundled Clack runtime; license preserved
internal/adapters/                # git, cache, filesystem, schema, process
internal/skillkit/                # native skill fetch, install, lock and operation lock
  git/, registry/, cache/, fs/, schema/, skills/, trustedadapter/
internal/contract/                # optional public JSON DTO marshaling/validation
schemas/                         # exact approved schema contracts; output versions per D4
internal/testkit/ or testdata/    # fake runners, hostile fixtures, golden snapshots
scripts/setup/                    # retain starter initializer (normally run once)
```

Reuse the starter's `Makefile`, `.github/workflows/ci.yml`, build checks, component tests, configuration patterns and license notices; adjust executable targets and coverage package lists to actual Nosey packages. Do not assume the original example commands are products. No separate TypeScript `src/`, `node_modules`, package manager, React/Ink renderer or Node deployment layer. Provide thin command constructors in `cmd/nosey/`; prefer focused interfaces at consumer boundaries, not a service interface for every struct.
