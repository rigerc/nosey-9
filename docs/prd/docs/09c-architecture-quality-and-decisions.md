# Architecture: security, reliability & ADRs

[← Specification index](../README.md) · [Previous](09b-architecture-data-and-flows.md) · [Next](10-errors.md)

### 12.13 Security, privacy and abuse controls

| Threat | Required control and validation evidence |
|---|---|
| Malicious Git/source text injection | Protocol allowlist; disallow option-like tokens, credentials in URI, `ext::`, `file:`, control chars; argv-only commands; fuzz parser and validate actual token arrays |
| Git hooks/filter/submodule execution | Disable hooks, submodules, LFS smudge and unsafe filters; `GIT_ALLOW_PROTOCOL` policy, noninteractive Git; test hostile remote fixtures |
| Traversal / symlink escape | Confine registry, template and local skill paths after realpath/canonicalization; reject symlinks escaping root; include recursive/destination path tests |
| Project config silently trusts registries | User-stored fingerprint approval; require renewed approval after registry-settings/path change; non-TTY incidental sources skipped |
| Untrusted detector code | No registry-defined executable code; custom adapters only user-configured, hash/version-pinned, explicitly trusted and constrained out of process |
| Secret exfiltration / terminal escape | Read `.env` key names only, passive MCP package tokens only; scrub env/secrets and strip ANSI/control characters in UIs, logs, JSON diagnostics |
| Scope confusion | Explicit project-only agents; no `--global`, wildcard agent or ambient agent auto-execution; source overrides and unpinned choices separately confirmed |
| False installed claims | Per-skill structured output and project inventory reconciliation; `unknown` for ambiguity; receipts not proof |
| Supply chain / ratings confusion | Attribute authored ratings and score source; officially curated scores require rubric/governance; never equate scores with detection confidence |

**Security tests:** Model unauthorized workspace-file modification, untrusted registry content, forked Git fetches and ambiguous upstream output. Review filesystem writes and subprocess env/argv with recorded sanitized test traces. No security certification, isolated sandbox or zero-telemetry claim follows from this planning work. Upstream inherited telemetry is disclosed and can be disabled via settings as specified in §10.

### 12.14 Reliability, error handling and cancellation

Each use case emits a structured result plus zero or more diagnostics with a stable code, severity, component, affected source/item and safe remediation. Registry errors are classified as **mandatory-source failures** (exit 2 or 3 as defined) or **incidental-source warnings** (continue with other valid registries). Snapshot promotion is all-or-nothing. A scan can be complete despite optional MCP exclusion; a required parser failure makes it incomplete, not a generic fatal error. JSON callers receive the same source-state semantics as TUI callers. Installer failure preserves per-skill observed states and never silently retries unknown outcomes or removes files. TUI SIGINT, resize and suspend cleanup restores terminal mode; child cancellation is deliberate, leaves an explicit incomplete status and does not misreport installed state. No daemon is required for retry orchestration.

### 12.15 Observability and diagnostics without product telemetry

**Proposed design.** Provide local structured events to stderr / `--verbose` and `nosey doctor`: version, platform, source IDs and validated revisions, cache staleness, registry schema or reference errors, provider capability matrix, rule coverage counts, install preflight policy decisions, per-skill verified outcomes, and timing of high-level stages. Never log raw dotenv values, MCP command strings, credentials, unescaped untrusted text or full subprocess environment. Persist no product analytics. Use diagnostics for user support and explicit exported artifacts only; upstream `DISABLE_TELEMETRY` behavior is separately controlled.

### 12.16 Performance, resource and accessibility budgets

The source PRD's provisional **warm-cache p95 under 2s** requires a declared fixture tree (file count, depth, sizes, project count), CPU/RAM, OS/terminal and refresh policy before it becomes a release gate. Proposed performance instrumentation isolates `config+cache`, `compile`, `walk+observe`, `recommend`, `inventory`, `render` and `Git refresh` durations so that offline/warm/cold metrics are not mixed. Bound maximum read sizes, regex inputs, recursion and child output; avoid expensive full-text indexing in MVP. Process file observations once per project and index shared manifest parses. The TUI must meet 80x24 full layout, degrade at 60–79 columns, keep a safe less-than-60 fallback, work keyboard-only, preserve dynamic footer intent, honor `NO_COLOR`, and never encode status only by color. Exact user accessibility testing and terminal support must be documented before launch.

### 12.17 Build and release topology (proposed)

Build `nosey` as a native Go executable using the ClackBT foundation and an approved Go toolchain compatible with the pinned starter. Preserve the starter's `gofmt`, `go vet`, `go test -race`, build and lint/check conventions; update `Makefile` references from `cmd/go-cli-template` to the initialized `cmd/nosey` path. Preserve existing GitHub Actions security, dependency review and vulnerability scans, and expand with Nosey JSON Schema fixtures, negative security cases, Bubble Tea model tests, OS/terminal smoke tests and disposable end-to-end Skills CLI verification. Do not claim that `make check` or Go tests have run as part of these documents. Capture binary release targets (OS/architecture, checksums and provenance) as explicit platform decisions before shipping.

Only approved versioned Nosey JSON Schemas may be embedded/distributed as static contracts. The **official default registry remains a Git-hosted external source**; it and its scaffold templates must not be compiled into the binary as runtime fallback. For `skills@1.7.6`, CI must actually exercise the exact chosen launcher (`npx` or installed executable), ref/argv grammar, agent translation, add/list JSON, per-skill reconciliation and installation scope in disposable projects. Node dependency checks apply to that chosen installer launcher, not to the native Go Nosey binary. Install mode is disabled until this compatibility gate passes.

### 12.18 Architecture decisions and alternatives

| ADR | Recommended choice | Rejected/alternate option | Revisit condition |
|---|---|---|---|
| ADR-01 | Local modular monolith | Background daemon / microservices | Multi-user remote workflows are in approved scope |
| ADR-02 | **Go + `rigerc/clackbt`** (user-selected; starter read-only inspected) | Original TypeScript/Node/Ink design | Revisit only by explicit owner change request |
| ADR-03 | Git snapshot + file-backed cache | Hosted database / custom registry API | Hosted collaboration/marketplace is approved |
| ADR-04 | Schema-first pure merge + immutable effective index | Mutable ad hoc registry object | Data set/incremental computation proves bottleneck |
| ADR-05 | Three-valued detection and mapping | Boolean-only inference | Never: boolean conflates unknown with absent |
| ADR-06 | Pinned upstream installer via argv | Reimplement agent installer or `npx` as shell | Upstream contract incompatible or unsupported long-term |
| ADR-07 | Per-skill verified install outcomes | Exit-code-only success | Never: violates source AC and false-success gate |
| ADR-08 | Explicit workspace trust fingerprints | First-use auto-trust | Never in MVP due to workspace takeover risk |
| ADR-09 | **Bubble Tea v2 full-screen TUI, ClackBT prompts for inline workflows** | Ink/React; nested prompt loops inside Bubble Tea | Revisit if verified terminal accessibility or API constraints fail |

**Architecture approval conditions:** Go and `rigerc/clackbt` are chosen by the user; engineering must freeze an actual starter commit, initialize the app in an authorized checkout, confirm package/API availability and test matrix, define supported platform and installer version requirements, and freeze approved schema references. Any change from this proposal is captured as a new ADR with impact on AC IDs and output contracts.
