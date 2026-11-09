# CLI and machine outputs

[← Specification index](../README.md) · [Previous](02-experience-and-tui.md) · [Next](04-registry-contracts.md)

## 5. CLI contract

All commands are **Nosey proposed CLI syntax**, distinct from the existing `skills` CLI. The approved amendments in [D3/D4](17-approved-decisions.md#d3--reviewed-installation-approval) define review-bound approval and version every advertised JSON output. `--json` on install requires `--dry-run`; reject executable/print JSON combinations. Commands run relative to the current directory unless the shared `--cwd` root option is passed. Root selection never ascends; see [D5](17-approved-decisions.md#d5--exact-scan-root-and-file-discovery).

```bash
nosey                          # Start TUI and scan workspace
nosey ui                       # Explicit TUI
nosey scan [--json] [--strict] [--enable-mcp] [--refresh] [--cwd PATH] [--query TEXT] [--technology ID] [--rec-technology ID] [--query-target technologies|skills|both] [--rule-kind KIND] [--registry ID] [--repository ID] [--priority core|recommended|optional] [--status any|installed|outdated|missing|unknown] [--quality excellent|good|fair|poor|unassessed] [--score-state any|scored|unscored] [--min-score 0..100] [--min-repo-average 0..100] [--sort-technologies name-asc|name-desc|evidence-count-desc|matched-rules-desc] [--sort-skills priority-desc|score-desc|quality-desc|repository-average-desc|name-asc]
nosey search QUERY [--registry ID] [--technology ID] [--repository ID] [--status any|installed|outdated|missing|unknown] [--quality excellent|good|fair|poor|unassessed] [--score-state any|scored|unscored] [--min-score 0..100] [--min-repo-average 0..100] [--sort relevance|score-desc|quality-desc|repository-average-desc|name-asc] [--json]
nosey search --upstream QUERY   # Delegate to upstream skills find (optional, passthrough)
nosey install SKILL_ID... [--agent ID] [--print] [--copy] [--ignore-conflicts] [--yes]   # See D8
nosey list [--json]            # Project installed inventory
nosey registry list [--json]
nosey registry add GIT_URL [--id ID] [--priority N]
nosey registry add PATH [--id ID] [--priority N]
nosey registry remove ID
nosey registry update [ID|--all]
nosey registry validate PATH [--standalone]
nosey registry init PATH [--template empty|workspace] [--template-registry ID] [--from-scan] [--include-tech ID...] [--register] [--git] [--yes] [--json]
nosey trust [--revoke]
nosey config get KEY
nosey config set KEY VALUE [--project]
nosey doctor [--json]
```

**Source-selection contract:** `registry add` and `registry remove` write only user-level registry settings, never `.nosey/config.json`. A mandatory source is a provider explicitly required for an action (such as `--template-registry ID` or `registry update ID`); unavailable mandatory sources fail with exit 3, while malformed, disabled, or untrusted mandatory sources fail with exit 2. `scan --registry ID` and `search --registry ID` are result filters, not implicit fetch/trust authorization; unknown filter IDs return exit 2. Untrusted auto-discovered workspace registries are skipped with diagnostics without blocking unrelated scans. Direct installs outside the catalog use the upstream Skills CLI directly.

**Execution rules:** [D8](17-approved-decisions.md#d8--thin-installer-delegation) defines installation. `install` without `--yes` requires a terminal. Nosey resolves requirements, blocks catalog conflicts and duplicate install names, then confirms the whole selection once before delegating one `skills add --yes` per source. Target agents are chosen once if not configured. All installs are project-scoped and replace existing names. `--yes` requires explicit or configured agents and replaces existing skills without prompts. `--print` emits shell-escaped commands without invoking the installer; `--copy` selects copy installation. Install has no JSON or approval-plan output. Cancelling Nosey confirmation runs no source. Nosey stops at the first failed source. Other commands' `--json` outputs remain stable JSON with diagnostics on stderr.

`nosey scan --json` includes `coverage` for required/excluded rule evaluation and optionally includes `capabilities` for runtime support and MCP opt-in; it returns `detectedTechnologies` and `recommendedSkills` for the filtered/sorted view, plus `unevaluatedRecommendations` for blocked-by-unknown mapping outcomes, `detectedTechnologiesTotal`, `visibleTechnologiesTotal`, `recommendedSkillsTotal`, `visibleRecommendedSkillsTotal`, and normalized `filters` and `sort` objects. `--technology` and `--rule-kind` narrow the **technology list** only, while `--rec-technology` narrows recommendations; `--query-target` selects which list receives `--query` (default `both`); `--registry`, `--repository`, `--priority`, `--status`, `--quality`, `--score-state`, `--min-score`, and `--min-repo-average` narrow the **recommendation list** only. `--query` is applied according to `--query-target` (both by default). TUI technology/recommendation filter state is likewise independent. Neither CLI nor TUI filtering changes underlying detection or recommendation calculation. `nosey search --json` returns `groups` with nested `technologyId`, `repositories` (including `repositoryId`, `averageScore` and optional `scoreCount`), and `skills` (including `quality`, `score`, `selected`, and stable ID); it also returns `totalUniqueSkills` so duplicated cross-technology rows are not double counted. Nullable output fields represent absent ratings, whereas a `0` rating remains a real value. See the complete exit-code table in §5.1; scan/search outputs use `outputVersion: 2` and a structured `diagnostics` array.


### 5.1 Exit codes and stable JSON results (normative)

| Code | Meaning | Examples |
|---|---|---|
| 0 | Successful operation, or warning-only incomplete scan | `scan.complete=false` on a bounded scan, with stderr warning |
| 2 | Usage, validation or policy-blocked operation | non-TTY init without `--yes`, blocked conflict/cycle, invalid registry, untrusted registry explicitly required by command, invalid template or init destination |
| 3 | Required registry/cache/network unavailable | no usable official or selected registry, template download unavailable |
| 4 | Detection failed or strict scan incomplete | `nosey scan --strict` and `complete=false` |
| 5 | Installation failure or partial success | per-skill results on stderr/JSON diagnostics |
| 6 | Missing/incompatible prerequisite | Git, pinned Skills CLI, its launcher/runtime (Node.js/npm if `npx`), or unsupported target agent; Go toolchain is build-time only |

A non-interactive scan skips untrusted project registries with a diagnostic; exit 0 unless an explicitly requested source is mandatory. `registry validate` fails with 2 for errors and 0 for warnings. `registry init` creates files atomically only after all validations and uses 2 for destination collision/data errors and 3 for missing template source. All seven advertised machine-readable outputs validate against their active schema and include `diagnostics`. Versions are scan/search/registry-init v2, install dry-run v3, list/registry-list/doctor v1. The exact command/schema matrix and config value rules are in [D4](17-approved-decisions.md#d4--complete-cli-machine-contracts). Original install-plan v2 is historical; it is not the active approval contract. Unknown installed state is always represented as `unknown`, never coerced to `missing`.

**Output contract v2:** The four JSON output schemas in `schemas/` are normative planning contracts, not examples of current execution. All fields consumed by the TUI/CLI have defined types and enumerated values; nested objects are closed unless an explicitly named extension point is specified. In scan output, `filters` and `sort` are fully normalized regardless of user-supplied flags; multi-select facets are arrays (empty means no restriction), `scoreState` is `any|scored|unscored`, and numeric threshold defaults are `null`. Scan `coverage` reports execution completeness and technology-coverage gaps independently. Confirmed recommendation entries require ID, name, state, priority, cause technology IDs, reasons, installed status, selection state, and explicit null-vs-zero ratings. `unevaluatedRecommendations` carry blocking unknown IDs. Search returns named technology/repository/skill groups with explicit nullable scores; install plans distinguish actual command `source` from `logicalSource`, permit `null` catalog skill IDs for direct installs and carry source provenance; registry-init records use `sourceRevision` and `createdBy`. Cross-record invariants (count totals, unique IDs, aligned grouped skill IDs/names, command-item membership, valid references, ordering and coverage equation) are mandatory semantic requirements in addition to JSON Schema validation. The original four schemas are the preserved baseline; approved D4 supersedes install-plan output with v3 and adds three v1 outputs. None is evidence of a running implementation.


**Exact normalized Scan filter defaults and sort keys:** `filters` requires `query: ""`, `queryTarget: "both"`, and always-present arrays `technologies`, `recommendationTechnologies`, `ruleKinds`, `registryIds`, `repositoryIds`, `priorities`, `statuses`, `qualities` (all `[]` by default). It additionally requires `scoreState: "any"`, `minScore: null`, `minRepositoryAverage: null`. An empty array means no restriction, including for `--status any`; a specified CLI single-value facet is normalized into a one-member array. `sort` requires `technologies: "name-asc"` and `skills: "priority-desc"` by default, with the corresponding allowed enums from §4. The query target and facets remain presentation-only. All keys must appear in `scan --json` even when defaulted, avoiding ambiguously empty objects. Search output uses the same comparator rules; it is not required to duplicate Scan's filter model.


### Registry initialization command contract

`nosey registry init PATH` starts an interactive wizard when connected to a TTY. The wizard preselects `workspace` as its starting template but does not import detected technologies until the user confirms the selection. `--template empty` makes a blank registry; `--template workspace` or `--from-scan` also offers detected technology and associated mapping/skill references. Noninteractive initialization requires `PATH` and either `--template` or `--from-scan`, plus `--yes` to approve writing files. `--template-registry ID` picks a configured, validated registry that explicitly publishes the requested template; otherwise Nosey always looks in the configured **official default Git registry**, not arbitrary higher-priority overlay registries. `--register` adds the generated local directory to the user's settings; `--git` optionally invokes `git init` in the new directory. Both are opt-in and independent. `--json` prints the created path, registry ID, template ID, source registry ID, `sourceRevision` (validated Git commit SHA or null for local templates), `createdBy`, output file list, and validation results. The TUI exposes the same wizard under **Settings → Registries → New registry** (`n`); **Add existing** (`a`) remains distinct.

Initialization fails without writing partial output when the destination already has conflicting files, a template is unavailable, or schema validation fails. No overwrite/merge mode exists in MVP. `--template-registry` must reference a registry already registered in Nosey; use `nosey registry add` first for a custom Git URL or local folder. `--template-ref` is intentionally not needed: template selection is resolved against the exact validated snapshot SHA, which is recorded in the generation metadata. The user may pin the source registry to a Git commit using existing source settings.
