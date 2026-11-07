> clackbt specialization: inspect `internal/clack/prompts` and `core`, Cobra commands, TTY checks and Go teatest before extracting observed contracts. Use `references/clackbt-source.md` for the source map.

# Existing-application inspection and contract extraction

## Goal and scope

Reconstruct **what the TUI currently implements** in a readable and machine-readable contract. This workflow is for brownfield applications, documentation, audits, migration planning, and integrations. It does not implement the app, change behavior, or propose changes as if they already exist. If the user wants new behavior, document it as a separately marked proposed design.

Default output scope is **contracts-only**. After inspecting implementation evidence, show an initial **provisional extraction plan** (surface, scope, provenance, known gaps), then interview the user, revise the plan after each round, and obtain explicit approval before authoring artifacts. Ask whether to reconstruct full screen layout trees and complete state-machine flows only when that additional work is materially relevant or requested. All output belongs at `<repo-root>/.work/flow-spec/<feature-slug>/`.

## 1. Establish trusted context and inspect source (read-only first)

1. Identify the repository root (`git rev-parse --show-toplevel`, else explicit root). Identify the executable/package and user-selected feature boundary before authoring.
2. Discover TUI entry points, app initializers, screen/router registries, component/widget builders, key handlers, focus managers, render/update loops, async effect managers, persistence/network edges, model definitions, validation functions, and test snapshots. Use `rg`/`grep`, file inspection, and existing documentation only as navigation. Avoid vendor/generated/lockfile directories where possible.
3. Build a source inventory: repository-relative path, relevant symbol or handler, exact 1-based source line range, and which contract item it backs. Record whether code is conditional on environment, feature flags, roles, terminal capabilities, or external plugins.
4. Follow control flow from user interaction → event → handler → state change/command → success/failure event → view update. Follow data flow from inputs, stores/services and persistence back to UI. Record cancellation, retries, errors, and unknown branches.
5. Prefer code and tests over README claims; recognize tests as evidence of asserted/covered behavior, not automatic proof of all runtime paths. Static inspection may not resolve late-bound imports or runtime-generated screens.
6. Never execute arbitrary application code, tests, install scripts, or external commands with side effects without user approval. Do not read or copy `.env`, keys, session tokens, or other secrets into artifacts. Do not embed sensitive sample data, unredacted logs, or proprietary code snippets in generated Markdown. Source locations and brief descriptions are usually enough.

## 2. Ask the user to resolve what source cannot

Ask only material questions after inspection. In particular: 

- Does the contract cover the *observed implementation* or the *desired next behavior*? Split if both.
- Which executable/workspace/feature, roles, configuration, and platforms are in scope?
- Are plugin-provided, dynamic, remote, or environment-dependent TUI elements in scope? Can those be observed through approved traces/tests?
- If code and tests disagree, which revision/behavior is authoritative?
- Where unknown semantics would change data shapes, permissions, validation, or side effects, can the user confirm them?

Before asking, present the evidence-grounded v0 plan with **observed** facts separated from **inferred/proposed** areas. Do not ask already-answered repository facts. Ask with a native question tool if present; otherwise use direct chat questions. After each answer, show an updated plan and delta, and interview again about remaining material gaps. Require explicit user approval of the consolidated plan before authoring files; record the plan revisions, question IDs, answers and approval in `DECISIONS.md`.

## 3. Derive contracts in source-to-contract order

1. **UI:** enumerate real visible screen identifiers and components, props/bindings, focus/keys if inspectable, emitted events and actual handler names. For contracts-only, `ui.components[].screen` / `node_id` are stable identifiers for observed surfaces; they do not require `screens.json`.
2. **State:** observed state fields, values/types, initial values, update ownership, invariants directly enforced by code. Do not invent invariants.
3. **Events:** events user, command, or system can emit; names/payloads/origin/consumers. Stable contract IDs may be normalized from source names, with mapping explained in `CONTRACTS.md`.
4. **Commands:** effect boundaries that actually exist (network, filesystem, DB, process, etc.), input, possible outcome events, idempotency as established by code; do not assume a retry is safe.
5. **Data:** named models, required fields, data sources, cardinality and providers. If models cannot be determined, ask before claiming them.
6. **Validation:** implemented predicates, triggers, target fields, and user-visible feedback. Absent validation rules remain absent; list possible missing rules in gaps instead of pretending they exist.
7. **Optional full scope:** reconstruct structured `screens.json` and `machine.json` from observed code and evidence. Missing/implicit transitions are gaps, not creative opportunities.

For each top-level contract entity, create one `evidence.json.claims[]` entry with a `ref`, `classification`, and `rationale`. Cite source `path:line_start-line_end` for **observed** claims. Use `user-confirmed` for facts confirmed in `DECISIONS.md`; `inferred` or `proposed` belong only to `draft` baselines. Prefer file SHA-256 hashes when reproducibility/drift monitoring is useful. Do not claim source line numbers unless you inspected that exact file.

## 4. Generate artifact bundle

### Contracts-only (default)

```text
.work/flow-spec/<feature-slug>/
  CONTRACTS.md
  DECISIONS.md
  EVIDENCE.md
  manifest.json
  contracts.json
  evidence.json
  validation-report.json
```

Use `assets/templates/manifest.existing-app.template.json`, `CONTRACTS.template.md`, `DECISIONS.template.md`, `EVIDENCE.template.md`, `contracts.template.json` and `evidence.template.json`. Set `source_mode: existing-app`, `artifact_scope: contracts-only`, `entry_screen` optional, and list every file in `manifest.files`. `EVIDENCE.md` describes inspected paths, coverage, source revision if known, observed vs inferred vs proposed behavior, gaps, and verification notes.

### Full reconstruction (optional)

Use the normal full spec baseline and add `EVIDENCE.md` + `evidence.json`, setting `artifact_scope: full`. Screen trees/state machine must agree with documented implemented behavior. In either mode, `status: ready` requires no open material questions, no inferred/proposed canonical claims, and passing validation.

## 5. Validate, reconcile, and hand off

Run `python3 <skill-root>/scripts/validate.py --repo-root <repo-root> --feature <feature-slug> --write-report`. Fix errors before calling the bundle ready. The validator checks structural and cross-file references and evidence source existence/line ranges (and hashes if present); it does **not** confirm that cited lines semantically support each claim. Read/review the implementation paths in an audit pass. Identify uninspected modules and runtime-only behavior honestly.

On future code revisions, reread changed files and update the contract and evidence together. SHA-256 drift mismatches are hard errors so outdated evidence cannot remain a passing baseline.

## Existing inline CLI extraction

For a non-fullscreen command, use the same read-only source/evidence workflow and `contracts-only` default. The `inventory` helper now highlights common command/argument parser libraries and CLI entrypoint signatures alongside TUI frameworks. Inspect exact command registration, flag and positional argument handling, environment/config precedence, stdin/TTY branching, stdout/stderr separation, retry/cancellation effects, and process exit codes from source and tests. Capture observed behavior with file/line citations; do not infer runtime output or escape sequences from a library import. If the user requests full-flow reconstruction, set `screens[*].presentation: "inline"`, include `cli` metadata and exit codes, and design transcript/prompt nodes (see [inline CLI guidance](inline-cli.md)). When command behavior depends on TTY availability, pipeline input, or color-disabled mode, record those branches as separate constraints or states; label unverified branches as inferred/draft.
