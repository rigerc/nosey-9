# Architecture: data, workflows & contracts

[← Specification index](../README.md) · [Previous](09-architecture-foundation.md) · [Next](09c-architecture-quality-and-decisions.md)

### 12.5 End-to-end scan / search sequence

1. `WorkspaceResolver` uses exactly canonical `--cwd` or current directory, never ascends, and discovers nested project boundaries only within it. Honor ignores/exclusions and narrow dotenv/MCP exceptions; never follow directory symlinks. See [D5](17-approved-decisions.md#d5--exact-scan-root-and-file-discovery); normalize evidence to root-relative slash paths.
2. `SettingsResolver` loads validated user + project configuration and checks the user-trust fingerprint before allowing project registry additions. A rejected workspace registry creates a diagnostic, not an implicit allow decision.
3. `SnapshotResolver` returns last validated immutable Git snapshots immediately when available; TTL refresh can update an **entire** snapshot only after validation. A CLI scan must record the snapshot IDs/revisions it actually used; asynchronous updates cannot change a result halfway through construction.
4. `RegistryCompiler` decodes technology-doc v1/v2 separately from manifest v1 and skills/mappings v2, validates sources, merges by defined precedence, validates effective entities and relationships, and materializes field-level provenance.
5. `ObservationPlanner` classifies in-scope active rule/project pairs as `required`, `excluded-by-mode` or `not-applicable`; selects actual providers based on capability and opt-in settings, then runs bounded scans.
6. `DetectionEngine` computes `matched`, `not-matched` and `not-evaluated` with reason, then derives three-state technology classification and the required coverage equation. Positive evidence wins; missing negative evidence cannot prove absence.
7. `RecommendationEngine` applies the same three-valued condition evaluator to mapping anchor + clauses; emits confirmed and unevaluated entries separately, unions passing requirements, and preserves evidence/provenance.
8. `InstalledSkillReader` reads only project-scope results from the pinned upstream implementation, returning `unknown` if source, scope, agent or inventory completeness cannot be established.
9. `ViewProjector` applies immutable presentation-only filter and sort functions to the computed scan/search model. The TUI shares selected IDs across duplicate Explore rows and preserves hidden selections; JSON serializes the canonical normalized v2 schemas.

**Failure isolation:** Apply [D6](17-approved-decisions.md#d6--invalid-effective-entities): exclude invalid effective entities/dependent mappings, retain unrelated data and invalidation gaps, never resurrect lower layers. Broken overlay entities produce diagnostics; a structurally invalid registry snapshot is never activated. A source fetch error uses the last validated snapshot, and an unavailable required detector makes `complete=false` rather than converting unknown into absence. A failed installed-state reader must not prevent browsing.

### 12.6 Data model, normalization and key invariants

All data models should be strongly typed and use discriminated unions rather than ambiguous nullable booleans. Canonical persisted/wire fields remain defined by the package's schemas; the following are **internal** design types, not added JSON requirements:

| Model | Key | Source of truth | Invariant |
|---|---|---|---|
| `RegistrySnapshot` | `(registryId, sourceRevision, contentHash)` | Validated Git snapshot or live local root | Only source-valid snapshots replace cache pointer; composition-specific invalid entities are excluded with gaps |
| `EffectiveTechnology` | exact case-sensitive `technologyId` | v1/v2 technology documents after merge | Rule IDs retain `kind`; metadata-only is not automatic detection |
| `RuleEvaluation` | `(technologyId, ruleId, projectPath)` | Detector run | `required` counts evaluated or unevaluated exactly once |
| `Detection` | `(technologyId, workspace/project)` | Rule-state aggregate | `detected` needs safe positive evidence; `absent` needs complete negative evidence |
| `EffectiveSkill` | `skill.id` | skills v2 after merge | `source.skillName` is distinct from catalog ID |
| `EffectiveRepository` | `repositoryId` | repository catalog + authored manifest scores | Missing average is null; never runtime-compute it |
| `EffectiveMapping` | `(mapping.id, recommendation.skillId)` | mappings v2 after merge | Passing entries only contribute priority/relationships |
| `Selection` | canonical `skill.id` | TUI session memory | Duplicate technology-group rows share one selected state |
| `InstallOrigin` | registry + logical source/ref + upstream skill name | Source resolution + project attribution receipt | No cache path or checkout temp name in logical identity |
| `InstalledStatus` | origin + target agent set | Pinned CLI reader + corroboration | `installed`, `missing` or `unknown`; receipt alone is not proof |

**Null and unknown discipline:** A missing `quality`, `score` or repository average is not zero. A missing installed record is not `missing` if the inventory is incomplete. A rule excluded by mode is not negative evidence. Absence of observed technology is not equivalent to known absence. No renderer can reclassify these states.

### 12.7 Registry compilation pipeline, overlays and consistency

**Pipeline:** ingest → source-file constraints → Draft 2020-12 validation → normalized source entity sets → ordered field-level overlay → effective schema validation → reference/graph validation → provenance index → immutable versioned effective catalog. Keep source validation and effective validation distinct because `skills.schema.json` and `mappings.schema.json` allow partial overlays that their `*.effective.schema.json` counterparts reject.

Source order is official priority 0 first, then user/project sources sorted ascending by priority and saved registration order; later fields win. Preserve rule `kind`, use ID-based merge for technology rules and mapping recommendation children, replace other arrays atomically, use an explicit `disabled` tombstone, and regard `recommendations: []` as a clear operation. Top-level source and assessment objects replace atomically. Do not let `null` silently delete values when unsupported. Field provenance should include source registry ID, effective snapshot/revision, original path and whether the property was overridden. For source/repository overrides, surface the chain of origin before execution. Compile on a consistent set of snapshot revisions: changing a snapshot during compile requires a fresh compile, not a mixed-generation index.

**Storage format (proposed):** Persist original validated snapshots under a content-addressed directory and a small atomic manifest of active revisions. Effective projections may be cached as derived data keyed by `(snapshot IDs+revisions, approved workspace registry fingerprint, settings hash, schema/tool versions)`; they must be fully reproducible from source snapshots. Derived caches have no authority to bypass current trust policy or effective validation.

### 12.8 Detection adapter design and coverage state machine

**Providers:** built-in `file`, `dependency`, `content`, `extension`, `dotenv`; named dependency observers for npm, python, cargo, go, ruby, composer, Docker, Terraform provider/resource, GitHub Action, Deno and Hex; opt-in passive MCP-package reader; and externally registered user-trusted detector adapters. A vocabulary enum is not proof that a parser is implemented. Each Go provider implements a small interface such as `Observe(ctx context.Context, project ProjectContext, budget Budget) (ObservationResult, error)` and declares supported ecosystems, rule kinds, provenance and budgets; this is a **proposed Go interface**, not an existing ClackBT API.

A `RuleEvaluation` has scope `required | excluded-by-mode | not-applicable` and status `matched | not-matched | not-evaluated`. `not-evaluated` always carries a reason from the stable vocabulary. Unavailable required providers, enabled-but-unavailable adapters, execution error and required-rule budget truncation are counted as required unevaluated. Explicitly off MCP and deliberately unactivated adapters remain excluded and may leave technology status unknown without causing a `complete=false` scan. `complete` is derived **only** from required evaluation status, not the total number of 3,637 authored reference rules.

**Truth table for derived technology:** any positive matched rule with nonempty safe evidence → `detected`; no positive plus all relevant active rules evaluated `not-matched` → `absent`; otherwise → `unknown`. In mapping conditions, use Kleene-style three-state evaluation and AND the parent technology gate with each present condition clause. An unknown condition yields an `unevaluatedRecommendation`, not a confirmed recommendation, never silently lower priority.

**Resources:** Read file names and parsed package identifiers rather than secrets; inspect `.env` key names only and skip implicit example `.env` files. Bounded content reads default 1 MiB per file; MCP configuration read limit 65,536 bytes; honor scan file-count/depth/time caps from a documented configuration. Never invoke MCP commands. Run trusted local custom adapters out-of-process with hash/version pin, JSON IPC, timeout, bounded output and no claim of OS sandboxing. Provide cancellation results and deterministic gap counts.

### 12.9 Recommendation, filtering and selection architecture

`RecommendationEngine` accepts the **unfiltered** effective catalog and derived technology-state map. It first validates every mapping anchor and condition ID, evaluates each mapping independently, deduplicates by skill ID, chooses highest passing priority, unions catalog and passing mapping relationships, and aggregates human-readable reasons in stable mapping order. Dependency graph traversal then identifies transitive prerequisites, cycles and conflicts before planning. An unevaluated mapping never contributes a confirmed recommendation's priority or dependency edges.

The same pure `filterAndSort` implementation must serve TUI Scan, TUI Explore and CLI output. Keep independent state for Scan technologies, Scan recommended skills and Explore; no facet changes detection facts. Explore groups **Technology → source skill repository → skill**, including `Other / Uncategorized`, and may duplicate skill rows across technologies while selection stays unique by `skill.id`. All sorting/null tie-breaking, status eligibility, score-quality independence, query semantics and hidden-selection counts follow §4 exactly. Avoid accidentally merging *source registry* and *actual skill repository* identities.

### 12.10 Installer planner, execution and reconciliation lifecycle

```text
selection -> dependency closure -> source/agent resolution -> preflight
     -> [blocked] OR [confirm / --yes] -> execute argv groups
     -> parse per-skill JSON -> read project inventory -> reconcile
     -> verified installed | definite failed | unknown/incomplete
```

**No speculative side effect:** `--dry-run`, `--print`, and TUI Copy commands must not execute the installer or mutate project files. Plan only from schema-validated, trusted sources and explicit agent IDs. `--copy` is the upstream copy method, not clipboard. A non-TTY install without `--yes` exits 2; `--yes` is acknowledged consent only after policy preflight.

**Stable plan items:** retain `(catalogSkillId|null, upstreamSkillName, original source, actual execution source, logical origin, requested ref, resolved revision if truly known, target agents, method, provenance, prerequisites, conflicts)`. Group by `(normalized execution source, ref, sorted agents, method)` only if the pinned parser's multi-`--skill` grammar is proven. Otherwise prefer independently verified one-skill argv calls over ambiguous bulk grouping. Every invocation uses `spawn`/argv with `cwd=workspace`; never a shell, no wildcard/global flags and no unsupported `--` separator.

**Verification rule:** A process exit code or group-level success is **insufficient**. Parse bounded `skills add --json --yes` output per skill and reconcile with `skills list --json` project-scope inventory and approved agents; map agent display names through a pinned compatibility table. Mark `installed` only with enough evidence to satisfy the v1.7.1 contract; `unknown` where source/ref/agent identity or reader completeness is ambiguous. Write/update `.nosey/install-origins.json` only as attribution, never as independent proof. Failed prerequisites block dependent operations; retries must exclude verified installed items. Since upstream effects are external, automatic rollback/removal is explicitly prohibited. Cancellation records incomplete work. [D2/D3](17-approved-decisions.md#d2--persistent-installation-sources) require persistent source materialization and a final hash/trust/inventory check under a project operation lock before executing immutable reviewed sources.

### 12.11 Versioned local API and machine-output contracts

There is **no public HTTP API** in MVP. The application exposes a **local command API**. The corresponding JSON outputs are the public integration contracts; readers must use the exact shipped schemas, with explicit versions and diagnostics on all six advertised JSON shapes, per [D4](17-approved-decisions.md#d4--complete-cli-machine-contracts).

| Command/result | Schema | Minimum semantic contract |
|---|---|---|
| `nosey scan --json` | `schemas/scan-output.schema.json` | `complete`, required coverage equation, detected/confirmed/unevaluated distinction, full normalized filters/sorts and visible/total counts |
| `nosey search --json` | `schemas/search-output.schema.json` | Stable technology/repository/skill grouping, explicit nullable scores, `totalUniqueSkills` deduplication |
| `nosey install ...` | None ([D8](17-approved-decisions.md#d8--thin-installer-delegation)) | Terminal handoff; `--print` emits shell-escaped commands |
| `nosey registry init ... --json` | `schemas/registry-init-output.schema.json` | Template/source IDs, `sourceRevision`, `createdBy`, created files, final validity and diagnostics |

The approved D4 decision adds [list v1](../schemas/list-output.schema.json), [registry-list v1](../schemas/registry-list-output.schema.json) and [doctor v1](../schemas/doctor-output.schema.json). These are required contracts, not optional unversioned JSON. Original install-plan v2 remains historical; scan/search/init keep v2. Reserve stdout for valid JSON in JSON mode, use stderr for human diagnostics, and map deterministic exit statuses 0/2/3/4/5/6 to original §5.1. Apply limits to untrusted subprocess output and avoid embedding raw ANSI/control sequences in JSON strings. Any JSON schema change is a contract review event with both positive and negative golden fixtures.

### 12.12 Persistence, cache and transactional writes

| Data | Proposed location / authority | Read/write policy |
|---|---|---|
| User config | `$XDG_CONFIG_HOME/nosey/config.json`, platform fallback | User-owned atomic validated write; authoritative for approvals and user registries |
| Workspace config | `<workspace>/.nosey/config.json` | Data only; untrusted registry references require user trust fingerprint |
| Trust approvals | User config or user-owned trust-store segment | Stable normalized workspace path + canonical registry settings fingerprint; revocation explicit |
| Git registry snapshots | `$XDG_CACHE_HOME/nosey/` content-addressed checkout | Last validated revision retained on fetch/schema failure; avoid partial activation |
| Installation-source payloads | Persistent user application-data `nosey/install-sources/` | Immutable source trees; separate from refresh cache; no automatic GC; D2 |
| Effective projection/index | Derived content-addressed cache, optional | Rebuildable; never source-of-truth for edits/trust |
| Installation origins | `<workspace>/.nosey/install-origins.json` | Atomic attribution per logical origin + skill + agents; not installation proof |
| UI filters/selections | Process memory for current TUI session | Retained across tabs/rescans according to §4; not silently persisted |
| Logs and recent operations | Bounded stderr/current-session views | Secrets and path control chars scrubbed; no hidden analytics upload |

**Write pattern:** Build to private same-filesystem temp location, validate, fsync as appropriate, atomic rename for config/receipts/scaffold. For registry-init, reject destination collisions before creating files and rename a validated directory into the final requested path; optional `git init` and `registry add` follow successful move. Use per-cache-source locking to avoid torn checkout/metadata pointers when multiple Nosey processes refresh concurrently. Derive and document stale snapshot, cache cleanup and crash-recovery behavior; cache cleanup must not change logical installed identity.
