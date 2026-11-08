# Scope, decisions & upstream

[← Specification index](../README.md) · [Previous](11-acceptance-criteria.md) · [Next](13-requirement-traceability.md)

## 15. Delivery scope and order

**MVP (must-have):** Remote-registry-sourced `nosey registry init` (empty and workspace templates, Git/local template providers, cache, output validation, optional Git initialization and local registration) with no bundled template archive, full four-tab TUI, automatic scan, version-dispatched v1/v2 technology documents, source-verified v2 catalog compatibility, built-in file/dependency/content/extension/dotenv detection with safe glob/regex matchers, opt-in MCP package inspection, capability reporting and additional ecosystem observation providers, extensible trusted-detector interface, JSON manifests/mappings/skills and optional repositories metadata, quality/score and repository averages, deterministic field merge, Git/local registries, cache + TTL, recommendations with conditions/dependencies/conflicts, technology → repository grouped Explore search with filters and sorts, separately filterable/sortable Scan technologies and recommended skills, multi-select, project-only installer/copy commands/dry-run/logs, Settings TUI, scriptable scan/search/install/list/registry commands, validation, and robust errors.

**Post-MVP:** Rich plugin detector ecosystem, upstream full-text search integration if a supported stable API emerges, saved filters/search presets and advanced query operators, skill update/removal workflows, collaboration and registry publishing tooling, dependency resolver suggestions, metrics, automatic registry test harnesses, and broader package ecosystem parsers.

**Delivery addition:** Create the official default registry as a separate Git data repository and add its exact initial commit as the application's `registries/default/` development submodule. This is P1R in the [current phase roadmap](15-delivery-and-risks.md), with the [repository/runtime boundary specified in §6](04-registry-contracts.md#default-registry-repository-and-development-submodule). It is not a bundled runtime fallback.

The M1–M5 list below is preserved source workstream coverage, not the repository implementation plan's M0–M8 numbering. Use the phase roadmap and its linked comparison for execution dependencies.

**Suggested milestones:** (1) schemas + registry loader/merger + validation design; (2) detection/mapping engines; (3) Skills CLI adapter + shell CLI; (4) TUI/keyboard/focus; (5) offline/security/compatibility and release hardening.

### v1.7.1 MVP milestone coverage

1. **M1 — registry foundation and trust:** Freeze the machine-output and completeness contracts, official registry manifest/schemas, version-aware technology loader (v1/v2), layered sources, cache/TTL, workspace approval fingerprints, source hardening and drift warnings. Upstream source-parser contract approval precedes adapter implementation.
2. **M2 — detection and adapters:** Seven vocabulary kinds (including opt-in `mcp_package`), all specified ecosystem observation providers, required/excluded rule coverage accounting, `metadataOnly` handling, monorepo evidence, pinned out-of-process trusted adapters and safety limits. No catalog rule revalidation.
3. **M3 — recommendation and browsing:** Skills/mappings v2 effective validation, three-state condition evaluation, independent recommendation relevance evaluation, rating provenance, catalog/mapping requirements, conditional union and cycle checking, score/quality display, Scan/Explore filters/sorts, grouping, installed `unknown` state.
4. **M4 — installation integration:** Pinned CLI runner and version-specific compatibility proof, approved agent mapping, tested argv segmentation, JSON add/list parsing, source grouping, stable logical origin and project attribution, dry-run, telemetry disclosure, per-skill post-verification and result attribution.
5. **M5 — registry init and settings:** Remote official templates, `empty`/`workspace` generator, sourceRevision provenance and no vendored skill payloads, Settings trust approval and TTL controls, coverage/source integration verification and official-score publication governance.

No MVP item is deferred silently. Unverified upstream behavior remains a release blocker, not a presumed implemented feature.


## 16. Remaining implementation decisions

The gap choices are now settled in [D1–D7](17-approved-decisions.md), including persistent sources, reviewed-plan approval/v3, seven JSON contracts, exact scan root, invalidation policy and the pinned initial catalog import. They are implementation requirements, not open product decisions. Source-ledger/catalog selection does not settle the new official registry's owner/URL/visibility.

- P1R official `nosey-registry` owner, canonical Git URL, visibility, license, maintainer, initial catalog/template SHA and pinned application submodule. Repository/submodule creation is required planned work, not completed setup.
- **Resolved:** Go with `rigerc/clackbt` is the chosen implementation. Open: exact starter commit pin and how the initialized module/repository name will be set. Bubble Tea v2 is the fullscreen renderer; bundled Clack prompts cover inline confirmation/wizard flows.
- Supported OS/terminal matrix and final performance budgets.
- Whether a future version supports raw HTTPS JSON as a registry source; excluded from v1 by interview decision.
- Whether richer AI agent detection or pinning behavior should be added later.

Implementation-entry blockers are the unverified pinned-CLI multi-skill argv behavior, reliable per-skill add/list output and source/agent reconciliation; scan completeness semantics and schemas must be frozen before implementation. Before launch, assign a named official registry/template maintainer, publish a versioned scoring rubric with evidence and correction/reassessment policy, and establish a measured recommendation-usefulness baseline and the supported OS/terminal and runtime performance matrix. These are gates, not reasons to remove any MVP feature.

## 17. External upstream reference

Official Vercel Skills CLI: https://github.com/vercel-labs/skills  
CLI command reference: https://github.com/vercel-labs/skills/blob/main/docs/commands.md  
Vercel skill directory: https://skills.sh/  

All `nosey` command names, JSON files and TUI interactions in this PRD are proposed requirements. The pinned `skills@1.7.6` tagged parser indicates Git refs use `#ref` rather than `@REF`; tagged CLI source indicates `list --json` and `add --json --yes` are available, but the actual result details, agent display-name translations, per-skill error cases, Git checkout/lockfile behavior and cache-local attribution still require disposable-project execution before integration sign-off. The implementation must never treat tagged-source inspection as proof of a successful install. Version-pinned source: https://github.com/vercel-labs/skills/blob/v1.7.6/src/source-parser.ts and https://github.com/vercel-labs/skills/blob/v1.7.6/src/cli.ts.

**Technology Registry v2 input:** `reference/technology-registry-v2/technologies-894-v2.json` and `schemas/technologies.v2.schema.json` are byte-for-byte unchanged. The accompanying report is preserved; its explanatory README has been edited to remove references to excluded executable scripts and tests. These are specification/reference assets and do not replace the external runtime Git registry.
