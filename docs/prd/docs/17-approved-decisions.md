# Approved implementation decisions

[← Specification index](../README.md) · [Detailed implementation plan](../IMPLEMENTATION_PLAN.md)

Status: user-approved decisions recorded on 2026-11-07, local revision `1.7.1-go-clackbt-draft.4`. These amendments take precedence over conflicting architecture/CLI planning text in the imported baseline. The original source package and AC-01–AC-72 remain preserved; specification schemas below do not constitute a running implementation.

## D1 — Acyclic registry delivery

M1R/P1R closes when the separate registry repository is validated and published, its canonical URL/revision and maintainer recorded, and the exact development submodule gitlink is reproducible. Validate source/effective data and template descriptors/files at this point. Do not require the future registry-init implementation to close this milestone. M3 proves fresh remote/cache access; M6/P4R subsequently proves empty/workspace generation and offline initialization. G2 closes only when all that evidence exists.

## D2 — Persistent installation sources

> **Superseded by [D8](#d8--thin-installer-delegation).** Nosey no longer materializes sources; existing source directories are left untouched.

Use a user-owned persistent source store separate from the disposable refresh cache. Prefer `$XDG_DATA_HOME/nosey/install-sources/` (platform application-data fallback), with immutable content-addressed directories. Before executing an installation sourced from a cached registry directory, safely materialize the validated source tree into this store and pass its persistent path to the pinned installer. Preserve the logical origin/requested ref separately from that execution path.

The tree digest covers sorted relative paths, file types, executable bits, file bytes and confined symlink targets; reject external links and special files. Lock and atomically promote fully staged trees. Never update a published tree in place. Preparing a preview may read/fetch/hash source data, but dry-run/print must not execute upstream or write project files; durable materialization is an execution-stage action. The user confirmed that raw `--print`/TUI command copying is disabled for local sources until a prior confirmed execution has prepared and verified their persistent directory; previews remain read-only with respect to that store. Derive the future content-addressed execution path without requiring that it already exists during preview.

Automatic refresh-cache cleanup must never delete installation-source trees. MVP performs no automatic garbage collection of this persistent store; receipts cannot establish that a payload is unused across all projects. Removal/repair tooling remains outside MVP. Missing or tampered trees produce diagnostics and unknown/broken inventory evidence, never optimistic success. Real pinned-CLI acceptance must verify skill-file readability and symlink targets after deleting/rotating refresh snapshots, restarting Nosey, and refreshing inventory. Identity equality alone is insufficient.

## D3 — Reviewed installation approval

> **Superseded by [D8](#d8--thin-installer-delegation).** Plan digests, `--approve-plan` and install-plan JSON v3 are removed.

Use `--approve-plan SHA256` together with `--yes` for noninteractive installs requiring additional approval of project/unpinned source overrides. General `--yes` is insufficient. The digest represents the entire reviewed selection, not approval of arbitrary future changes from a source. It never grants workspace registry trust or bypasses validation, conflicts, prerequisite checks or project-only scope. Missing/mismatched approval is policy exit 2 with no installer invocation.

Dry-run resolves the exact Git commit and source tree fingerprints without invoking the Skills CLI; local sources get content fingerprints. An unresolved revision or unreadable source may still produce a blocked preview, but cannot produce usable approval. The execution service recomputes the plan and fingerprints, verifies current trust, compares the approval digest, and executes the exact immutable reviewed content. A mutable branch must be resolved and pinned for execution, not re-resolved by upstream after approval. Local mutable source content must be sealed in the persistent store before execution. Reject a source form whose pinned execution cannot be proved with the selected upstream version.

Install-plan JSON moves to **outputVersion 3**, with a v3 schema (removed by D8). `approval` contains `required`, `digest`, `canonicalization`, `installerVersion`, `sourceFingerprints`, and `registryFingerprints`. `required` describes the additional-approval policy, not whether consent has already been supplied. `digest` is lowercase SHA-256, or null when source resolution/policy blockers prevent a reviewable plan. A non-null digest is not itself permission to execute.

`nosey-plan-v1` hashes UTF-8 canonical JSON of the complete v3 plan after removing top-level `diagnostics` and `blocked`, `approval.digest`, and each item's presentation `status`. Sort object keys lexicographically; preserve deterministic array order; use compact JSON with no BOM/trailing newline, no HTML escaping and no normalization of Unicode string contents. Encode controls and quote/backslash using JSON escapes; retain other valid Unicode as UTF-8. No floating-point values occur in this projection. All fingerprint arrays have deterministic ordering by source identity; agent arrays are sorted and deduplicated. Reject invalid Unicode rather than silently replacing it.

The projection includes canonical workspace root, installer version, actual argv, logical/execution sources, refs/revisions, skill IDs/names, agents, method, source/registry content fingerprints, overrides and conflicts. Thus changes to reviewed source content, revisions, agents, method or execution commands invalidate consent. Normalize and stage source material before the final comparison to avoid a check/use race. Concurrent installations into the same project require a project operation lock and a final inventory/conflict check. Cross-language digest golden vectors, changed-source tests and zero-process-invocation failures are M5 acceptance requirements.

## D4 — Complete CLI machine contracts

Every advertised JSON command has a closed versioned schema, not an ad hoc serialization of internal Go structs:

| Command | Active output version | Specification schema |
| --- | --- | --- |
| `scan --json` | 2 | [scan](../source-planning-package/schemas/scan-output.schema.json) |
| `search --json` | 2 | [search](../source-planning-package/schemas/search-output.schema.json) |
| `install ... --dry-run --json` | Removed by D8 | No current JSON contract |
| `registry init ... --json` | 2 | [registry init](../source-planning-package/schemas/registry-init-output.schema.json) |
| `list --json` | 1 | [installed inventory](../schemas/list-output.schema.json) |
| `registry list --json` | 1 | [registry inventory](../schemas/registry-list-output.schema.json) |
| `doctor --json` | 1 | [doctor checks](../schemas/doctor-output.schema.json) |

Retain original install-plan v2 as historical input. D8 removes install JSON, dry-run and approval plans. `--print` remains shell-escaped text. Structured errors populate diagnostics when an operation has a schema-valid response; failures before a response can be constructed use stderr/nonzero exit and must not fabricate success. Keep stdout free of progress/ANSI/logging.

Installed inventory is explicitly project-scoped, with completeness, nullable catalog identity/origin, target agents, observed path and conservative status. Missing records cannot establish absence unless the reader is complete. Registry inventory exposes normalized sources, priorities/TTL, trust and cache state without credentials. Doctor publishes stable check IDs/status/code/message; versions, provider and adapter checks are represented as named checks rather than unversioned extra fields. Validate counts/uniqueness/scope and redact all output in addition to schema checking.

Implement `config get KEY` and `config set KEY VALUE [--project]` in M1 using the same settings service as the TUI. Dotted keys name allowed settings fields; parse VALUE as a JSON value and validate its schema type. Reads show the effective selected value as JSON; writes affect only the chosen user/project layer and are atomic. Reject unknown keys, type mismatches, project adapter activation and trust-store writes. Do not advertise `--json` for these commands unless separately versioned. Preserve independently defined config merge and user-owned registry/trust rules.

## D5 — Exact scan root and file discovery

The canonical scan root is exactly `--cwd` when supplied, otherwise the current directory. Never ascend to a Git/package root or load ancestor project configuration. Discover nested projects only below this root; normalize evidence to root-relative slash paths and deduplicate observations by canonical relative path and project scope.

Honor applicable Git ignore rules plus explicit configured exclusions. Do not traverse directory symlinks or descend into nested Git repositories/submodules automatically; an explicitly selected root may itself be a submodule. Git ignore evaluation may consult containing-repository ignore metadata without expanding scan scope or loading its project configuration. Resolve the initial requested root once, then reject any walked/read path escaping it; file symlinks are read only when their canonical target remains inside the root and normal budgets allow it.

The bounded dotenv-key and opted-in MCP readers may explicitly inspect permitted project config files hidden by Git ignores; this is a narrow exception, not permission to walk ignored directories generally. Explicit user exclusions remain authoritative. Dotenv never exposes values; MCP remains passive/off by default and never runs commands. Required reads prevented by explicit exclusions, symlinks or budgets must be classified honestly as unevaluated/gaps when relevant, not negative evidence. Test nested cwd with a parent Git root, ignores/negations, symlink cycles/escapes, submodule boundaries, nested projects, permitted secret-config readers and `--cwd` parity across CLI/TUI.

## D6 — Invalid effective entities

Separate source acceptance from effective compilation. A structurally/path/schema-invalid source snapshot is rejected before activation and cannot replace its last source-valid cached revision. A source-valid partial overlay may still produce invalid effective entities when composed with other registries.

Exclude only affected invalid effective entities and dependent mappings, retaining unrelated valid entities with provenance diagnostics. Do not silently restore a lower-priority base value/entity that the invalid overlay superseded. Propagate reference invalidation through mapping anchors, conditions and required relationships until stable; skills with invalid installation prerequisites remain unplannable rather than having those constraints dropped. Preserve an invalidation ledger even for excluded technologies, so affected required detection is incomplete/unknown and never creates absence or negative-condition recommendations. Positive independently valid evidence is retained where the remaining effective technology/rules are valid.

The cache promotion rule is **source-valid snapshot**, not a claim that every future overlay composition is effective-valid. Effective indexes include diagnostics and invalidation state; last compiled entities cannot silently substitute for current invalid ones. `registry validate` exits 2 for effective errors, including excluded entities, while unrelated browsing remains usable. A structurally invalid mandatory source follows the existing deterministic error rules. Test one invalid overlay beside an unrelated valid technology, a dependent mapping and a `noneTechnologies` condition; no lower-layer resurrection, false absence or installer call is allowed.

## D7 — Initial official catalog import

The concrete maintainer workflow is in the [skills and mappings import plan](../CATALOG_IMPORT_PLAN.md).

The user selected these upstream authoring inputs for M1R; this is broader than the previously suggested web-only starter set:

- [Source ledger at inspected commit](https://github.com/rigerc/skills-catalog/blob/d1bc68a826cd233f85f291eca0cfa9b4303713e6/data/catalog-source-ledger.json).
- [Mappings at the same commit](https://github.com/rigerc/skills-catalog/blob/d1bc68a826cd233f85f291eca0cfa9b4303713e6/data/mappings.json).

Inspection of commit `d1bc68a826cd233f85f291eca0cfa9b4303713e6` found 877 ledger entries, all labeled `verified` by that upstream ledger; 289 technology entries (223 mapped, 66 detect-only); 35 combinations; and 877 distinct referenced skill IDs, all present in the ledger. These are authoring assertions and shape/reference checks, not Nosey installation or licensing verification. See [inspection evidence](../catalog-import-inspection.json) for Git blob IDs, missing IDs and verification limits.

The files use `schema_version`, slash-separated skill IDs and `skill_ids`, not Nosey's runtime contracts. M1R must produce a deterministic, reviewed conversion into v2 `skills.json`/`mappings.json` and optional repository identities. Keep original upstream IDs, pinned source document/commit and conversion decisions in registry authoring provenance, not invented fields in closed Nosey schemas. Do not rename the official registry to the upstream `catalog_id: nosey-built-in` or embed it at runtime.

**User scope clarification:** The supplied sources are already verified. Accept the ledger as the source-verification authority for this import; do not repeat source audits, license reviews or per-source installation checks. Only resolve metadata missing from required Nosey fields. Upstream verification still does not assert Nosey runtime/installer compatibility, which remains separate application work.

Import requirements:

1. Read the ledger and updated mappings at one recorded revision, preserving supplied source commits, names and declared repository aliases. No new verification campaign.
2. Convert upstream skill IDs through a stable collision-checked Nosey ID map; preserve source skill names independently and apply the same map to all recommendation references.
3. Apply the approved explicit technology crosswalk for the 41 nonmatching IDs. Equivalent identities use existing canonical IDs; genuinely new identities remain metadata-only. Preserve all original 894 entries/rules and the original reference bytes.
4. Convert technology `skill_ids` to recommendation records and combinations to `when.allTechnologies`, keeping conditions separate from unconditional mappings. Missing priority uses the normative optional default; do not invent assessments or skill prerequisites.
5. Ignore `frontend_essentials` entirely: no bonus recommendations, flags or follow-up decisions. Ledger skills are still imported independently. Ignore `mcp_scan_context` and detection examples as outside this skills/mappings import.
6. Validate generated schemas, IDs, references, mapping conditions, source metadata preservation and deterministic output. Account for every supplied skill and technology/combination mapping; fix/report conversion problems without silent drops. Do not require new source-audit evidence before this import.

## D8 — Thin installer delegation

Approved 2026-11-09. Supersedes D2, D3 and any conflicting text in the CLI contract, the installation chapter and the architecture documents, including project-only scope for interactive installs, post-install hash verification, `.nosey/install-origins.json` and the project operation lock.

`nosey install` and the TUI build one upstream `skills add` per source from the catalog and hand the terminal to the Skills CLI, which owns scope, method, agent prompts, overwrite warnings, confirmation and progress. Nosey keeps requirement resolution, conflict and duplicate-name blocking, and catalog pins, which are passed through unchanged as `#sha`. Installed state comes from `skills list --json` and `skills-lock.json`.

The CLI is an installed `skills` of the pinned version or, failing that, the pinned package located through `npx` from Nosey's cache directory and started with `node`, so workspace `node_modules` and `.npmrc` are never consulted. Nosey stops with exit 6 when neither is available and never installs the CLI itself. CI exercises both launchers against the real pinned CLI.

Known consequences: an interactive user can choose global scope at the upstream prompt; `--yes` replaces existing skills with the same name; hand-edited tracked skills still report as installed. The v3 plan schema is removed; the original planning package retains historical v2.

## Verification and delivery ownership

M1 owns JSON settings/config commands, the new JSON specification schemas and source/effective invalidation tests. M2 owns exact-root discovery and the early v3 preview. M1R owns pinned catalog conversion, registry publication and the development gitlink. M3 owns source-valid snapshot/cache behavior; M5 owns the persistent installation-source store, review digest, exact-content execution and pinned upstream proof; M6 owns end-to-end template/init acceptance. M8 verifies all seven active JSON outputs and original AC coverage plus D1–D7. These decisions add acceptance requirements; they do not imply any Nosey runtime is implemented.

Specification verification for this update: all 20 original/extension schema documents passed Draft 2020-12 meta-validation; 22 positive/negative checks exercised the four added contracts. Active document links/anchors and 72-AC primary ownership were checked, and original source assets/AC text matched the supplied archive byte-for-byte. These checks do not demonstrate the future Go validator, runtime consent enforcement or installer compatibility.
