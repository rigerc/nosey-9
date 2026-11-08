# Nonfunctional & verification

[← Specification index](../README.md) · [Previous](13-requirement-traceability.md) · [Next](15-delivery-and-risks.md)

## 19. Cross-cutting nonfunctional requirements and verification plan

| ID | Requirement / measurable rule | Status | Verification |
|---|---|---|---|
| NFR-01 | No executable installation without deliberate confirmation (`--yes` for non-TTY); project-scope-only agent selection | Source normative | Fake-runner negative tests and upstream project install e2e |
| NFR-02 | All seven advertised JSON commands validate against their active v1/v2/v3 schemas in approved D4; documented semantic invariants pass | Source normative | Golden serialization tests and cross-field property checks |
| NFR-03 | No false technology absence when a relevant rule was not evaluated; `complete` derived from required rules only | Source normative | Exhaustive three-state test matrix plus budget/provider failures |
| NFR-04 | No false `installed` claim without per-skill and project inventory evidence | Source normative | Real pinned-CLI integration tests including partial/malformed output |
| NFR-05 | A failed/invalid registry refresh cannot replace the last validated snapshot | Source normative | Fault-injection tests (network, schema, interrupted fetch, concurrent access) |
| NFR-06 | Untrusted project registries cannot be activated by project config alone | Source normative | Changed-workspace/fingerprint and noninteractive flow tests |
| NFR-07 | Preserve source/query/selection state across TUI section switches; maintain one-column accessible keyboard behavior | Source normative | TUI e2e state transitions and terminal matrix |
| NFR-08 | Warm-cache scan target p95 under 2 seconds on a **defined** representative fixture/CI runner | Provisional source target | Benchmark definition and published distributions needed |
| NFR-09 | No product telemetry in MVP; upstream telemetry default inherited, disabled mode passes documented env vars | Source normative | Inspect outgoing logging controls and sanitized child env |
| NFR-10 | Config, receipt and scaffold writes must not leave partial committed state after failure | Proposed engineering acceptance | File fault injection and crash recovery tests |
| NFR-11 | No raw secret values or hostile terminal control sequences in diagnostics, TUI or stdout JSON | Source normative | Snapshot/ANSI sanitization tests with malicious fixtures |
| NFR-12 | All bundled detection observation provider capabilities explicitly reported; unavailable required ones become gaps | Source normative | Capability matrix vs runtime provider implementation tests |

## 20. Detailed engineering verification and release gates

### 20.1 Test pyramid and artifacts to create

**Unit tests** for exact ID matching including scoped IDs; v1/v2 decoder; rule scope and evaluation; coverage accounting; three-state technology truth tables; three-valued mapping condition clauses; field merges with partial/tombstone/atomic behavior; source-provenance lineage; score null vs zero; stable sort comparators; dependency/cycle and symmetric conflict detection; escaping/normalization; and install-plan group derivation.

**Contract tests** validate all 16 original and four approved additional JSON specification schemas plus source-vs-effective overlay semantics, closed objects and enum values, duplicate-ID semantic checks, the command-specific output version (v1/v2/v3), full default filters, cross-record counts, grouping uniqueness and aligned command item arrays. Test data must be newly authored; the supplied package contains no executable fixture/test harness. Do not rewrite the reference 894-technology catalog to make tests pass.

**Integration tests** use Go package-local test fixtures (`testing`, `httptest` where applicable), fake source providers/process runners, failing cache/store and bounded filesystem fixtures for offline/stale behavior, cancellation, immutable-snapshot promotion, user/workspace trust gate, registry-init collision/symlink cases, source compatibility, partial add output and unknown installed identity. Then run real pinned `skills@1.7.6` checks in disposable projects with several agent identities, Git refs, local cached skill directories, and both successful and failed installs.

**Approved-decision tests** cover M1R closure without M6; persistent skill readability after deleting refresh cache; digest stability and invalidation on source/ref/agent/method changes with zero invocation on mismatch; exact nested cwd/ignore/symlink policy; invalid overlay exclusion without lower-layer resurrection; and full catalog/crosswalk disposition. See [D1–D7](17-approved-decisions.md).

**System tests** run under the initialized ClackBT Go CLI and test Bubble Tea v2 model transitions (and Clack native-flow cancellation) for launch-to-Scan, Explore grouping/selection, hidden selections, Install confirmation/Cancel/Dry-run/Print/Execute, Settings registry trust approval, New Registry wizard, registry CLI, doctor, strict vs non-strict incomplete scan, noninteractive exit behavior and terminal cleanup under SIGINT. Reconcile expected results with AC-01–AC-72 individually.

**Security tests** include malicious Git URI variations, argv injection, ref traversal, symlink escape, unbounded file/regex/MCP read, untrusted ANSI strings, secret redaction, project self-approval attempt, adapter hash mismatch and bounded child output. Negative tests must assert **no installer invocation** on preflight failure.

### 20.2 Release gates (must have owners and recorded evidence)

| Gate | Exit evidence | Blocker if absent |
|---|---|---|
| G1 — Source and public contract freeze | Preserved 16 baseline plus four approved extension schemas, D1–D7 decisions, v1.7.1 AC coverage, **Go/ClackBT starter commit and module identity**, version table, full source/effective semantic test suite | Engineering cannot assume inputs/outputs or template dependencies are final |
| G2 — Trusted registry publication | P1R separate official Git repository/URL and maintainer; signed-off templates/source SHA; unchanged v2 catalog; recorded development submodule URL/gitlink SHA; fresh remote/cache init proof and app build without submodule; recovery plan | No safe default data source/init provider |
| G3 — Detection and recommendation correctness | Seven rule kinds and ecosystem provider capabilities demonstrated; completeness semantics and unknown-safe evaluation; independently reviewed corpus protocol | Cannot promise positive/negative recommendations |
| G4 — Upstream CLI compatibility | Actual v1.7.6 argv/ref tests, add/list JSON outcomes, display-name ↔ ID mapping, source/lockfile/cache-origin reconciliation in disposable project | Install cannot be signed off on parser inspection alone |
| G5 — Installation safety and recovery | Project-only safeguards, per-skill verification, partial failures, cancellation, no false success | Release safety failure |
| G6 — UX, platforms and performance | Supported OS/terminal matrix; keyboard and narrow-layout acceptance; named p95 fixture/runner results; safe exit/NO_COLOR | Product and operational incompleteness |
| G7 — Editorial and launch usefulness | Curated-score rubric/owner/correction policy; labeled recommendation precision@3 and applicability coverage studies, usability task completion | Curated official ratings and usefulness claims must not be published |

### 20.3 Baseline metrics protocol (proposed)

Treat ≥95% technology ID correctness, ≥80% recommendation precision@3, ≥80% of applicable workspaces receiving actionable recommendations, ≥90% unaided study completion and warm-cache p95 <2s as **provisional targets**, not demonstrated outcomes. Before measuring, approve the sampling frame, corpus version/SHA, independence of human labels, definition of an applicable workspace and actionable skill, top-3 ranking tie rule, sample sizes, confidence intervals and exclusions. Report technology-state false positives **and** unknown coverage separately; do not calculate success by dropping unknown cases invisibly. Record terminal width and OS for usability, and hardware/file-tree characteristics for performance. No runtime product telemetry is required to conduct a controlled pre-release evaluation.
