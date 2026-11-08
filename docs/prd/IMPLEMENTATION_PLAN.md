# Nosey implementation plan for this Go starter

Status: proposed implementation sequence; no Nosey application behavior has been implemented by this handoff. Updated 2026-11-07 against the multifile Go PRD, local integration revision draft.4 with user-approved gap resolutions.

Build Nosey in this repository using Go, Cobra and Bubble Tea v2. Preserve the PRD's product behavior and versioned JSON contracts; Go/ClackBT is now the confirmed implementation choice in the multifile PRD. Start with contract and compatibility gates, then deliver a local-registry scan/search/dry-run slice, followed by remote registries, complete detection, verified installation, the four-tab TUI and launch verification. The first slice is an engineering checkpoint, not a reduced MVP.

## 1. Inputs, authority and decisions

The current multifile Go archive is integrated beneath this directory. Its original source package is unchanged; local planning edits are described in the index:

- [Current PRD index](README.md), [Go architecture](docs/09-architecture-foundation.md), [acceptance criteria](docs/11-acceptance-criteria.md), [scope](docs/12-scope-and-decisions.md), [release gates](docs/14-nonfunctional-and-validation.md), and [phase roadmap](docs/15-delivery-and-risks.md).
- [Previous expanded draft](archive/expanded-typescript-draft/Nosey_Full_PRD_and_Technical_Architecture_v1.7.1.md): historical input only; its TypeScript recommendation is superseded.
- [Approved decisions D1–D7](docs/17-approved-decisions.md): newer user decisions take precedence over conflicting baseline text. [Catalog inspection](catalog-import-inspection.json) records the pinned authoring-input evidence.
- [Original PRD](source-planning-package/PRD.md): normative v1.7.1 behavior.
- [Contract schemas](source-planning-package/schemas/): 16 preserved baseline Draft 2020-12 schema documents plus [four approved output extensions](schemas/).
- [Example registry](source-planning-package/example-registry/): authoring examples, not an executed integration fixture.
- [Technology reference](source-planning-package/reference/technology-registry-v2/technologies-894-v2.json): source catalog; preserve it and its v2 schema byte-for-byte.

The preserved source normative requirements/schemas are the baseline; approved D1–D7 amendments govern the updated behavior and output versions. Source-package files remain unchanged. The newer user-selected Go/ClackBT architecture supersedes the original TypeScript recommendation; preserve the original source package as historical product/contract evidence. The PRD phase roadmap defines scope; this document supplies repository-specific work breakdown and the comparison in §8. Do not treat upstream source descriptions, schemas or examples as proof of runtime compatibility.

| Decision | Treatment in this implementation |
| --- | --- |
| Go/ClackBT | Confirmed by the newer PRD; no further stack approval is pending. Pin the actual starter/dependency baseline; no Node application or Ink renderer. |
| TUI framework | Reuse installed Bubble Tea v2, Bubbles v2 and Lip Gloss v2. Keep core services independent of terminal rendering. |
| Upstream installer | Retain the proposed exact `skills@1.7.6` baseline until disposable-project verification approves it. Node remains an upstream installer prerequisite; Go does not remove that dependency. |
| Data and wire contracts | No language-driven changes: manifest/settings/template/scaffold v1; technology v1/v2 dispatched independently; skills/mappings v2; scan/search/init outputs v2; install-plan v3; list/registry-list/doctor v1. Original install-plan v2 is historical. |
| Default registry | Create a separate `nosey-registry` data repository and add it at `registries/default/` as a pinned development submodule (M1R). Confirm owner/URL/visibility before creation. Runtime still fetches the external Git source; no embedded registry/template fallback. |
| Go-specific compatibility | Prove Draft 2020-12 validation, presence-aware decoding, glob semantics and source regex compatibility before selecting implementations. Do not silently reinterpret patterns or rewrite the supplied catalog. |

Non-goals remain accounts, a hosted service/database, global skill installation, automatic install/repair, Nosey-owned update/removal, executable registry plugins, raw HTTPS registry providers, and editing skill contents. Optional upstream search passthrough is not required for the first slice. All §15 MVP features remain required, including ecosystem providers, trusted detector support, registry init and Settings.

## 2. Verified starting point and adaptation map

Paths below are existing repository components; packages described as new in the next section are proposals.

| Existing component | Current behavior | Planned treatment |
| --- | --- | --- |
| `cmd/go-cli-template/root.go`, `main.go` | Cobra root launches a directory list; `main` maps every error to exit 1 | Replace demonstration root with Nosey service wiring; implement typed exit codes 0/2/3/4/5/6, context cancellation and clean stdout/stderr separation. |
| `greet.go`, `prompt.go`, `config*.go` in that command directory | Demonstration commands and TOML configuration | Remove demo commands from the product command tree as replacements land; retain version/completion and evaluate the optional skill generator separately. |
| `internal/app/app.go`, `internal/workflow/workflow.go` | Empty dependency container and service extension points | Use for dependency assembly and application workflows rather than placing business logic in Cobra handlers. |
| `internal/domain/config.go` | UI/editor theme defaults | Add explicit Nosey domain types; keep presentation preferences separate from schema-defined product settings. |
| `internal/config/loader.go`, `manager.go` | Typed TOML layering; theme manager writes directly | Implement JSON settings and explicit per-ID/presence-aware merging; add atomic validated persistence. Generic TOML merge/save behavior is not the Nosey settings contract. |
| `internal/runtimeconfig/`, `internal/logging/` | Environment validation and logging infrastructure | Reuse where appropriate; define product env policy without allowing project files to activate adapters or write trust. Keep logs redacted and off JSON stdout. |
| `internal/ui/` | Theme, responsive layout, list/help and confirmation helpers | Reuse primitives; replace the directory-list model with a persistent four-tab shell and Nosey view state. |
| `internal/clack/` | Bundled prompt runtime with native composable workflow models | Reuse for standalone interactive wizards where appropriate; do not nest terminal programs inside the four-tab TUI. Preserve its license. |
| `internal/adapters/shell/shell.go` | Shell alias formatting | Not an installer runner. Add an argv-based process adapter; shell escaping is for display/copy only. |
| `scripts/setup/main.go`, `internal/package/package.toml` | Existing dry-run/apply initialization with required identity metadata | Use setup once identity values are supplied. Review generated README/AGENTS, imports, command rename, Makefile and workflow changes. Its directory walk currently excludes `docs/`; keep source PRDs immutable. |
| `Makefile`, `.github/workflows/ci.yml` | Format, vet/custom lint, race tests, runtime coverage and build | Preserve checks, replace demo-specific coverage targets as demos retire, add core Nosey contract/integration gates and platform builds. |

Initialization needs the real Go module path, repository URL and author. `nosey` is the executable name. Do not infer publishing identity from the template's existing metadata. Continue domain work before renaming if these values are unavailable; do not run external sync/release automation.

## 3. Proposed Go boundaries

Only create packages when the corresponding milestone uses them. Rename `cmd/go-cli-template/` to `cmd/nosey/` through reviewed setup; until then, existing paths remain valid.

| Location | Responsibility |
| --- | --- |
| `cmd/nosey/` (renamed) | Thin Cobra handlers, flag validation, command help, process entry and exit mapping. |
| `internal/app/` (extend) | Construct services and inject filesystem, clock, process runner, providers, inventory and logging. |
| `internal/workflow/` (extend) | Scan, search, plan/install, list, settings, registry/init, trust and doctor use cases with `context.Context`. |
| `internal/domain/` (extend) | Typed IDs, states, rule evidence/coverage, diagnostics, provenance, snapshots, recommendations and plans; no TUI/process dependencies. |
| `internal/contracts/` (new) | Version dispatch, schema validation and explicit wire DTOs; approved schema copies embedded here only, byte-compared against the supplied contract source. No embedded registry or template payloads. |
| `internal/registry/` (new) | Source/effective validation, normalization, deterministic merge, reference checking, field provenance, repository scores and template generation. |
| `internal/detection/` (new) | Bounded walk, observation providers, rule evaluation, evidence sanitation and coverage aggregation. |
| `internal/recommendation/` (new) | Three-state conditions, mapping unions, prerequisites/conflicts and deterministic recommendation ranking. |
| `internal/catalog/` (new) | Shared query/filter/sort logic and technology → repository → skill grouping for CLI/TUI parity. |
| `internal/installation/` (new) | Pure plan building, source/ref policy, prerequisite graph, stable logical identity and conservative reconciliation. |
| `internal/config/` (extend) | JSON settings/defaults/validation, workspace path bases, presence-aware merge and user-owned trust storage. |
| `internal/adapters/{git,cache,filesystem,process,skills,detector}/` (new) | Hardened I/O, immutable snapshots/locks, bounded subprocesses, pinned upstream compatibility/inventory and trusted adapter execution. |
| `internal/ui/` (extend) | One-column shell, Scan/Explore/Manage/Settings, dialogs, selection/focus and accessibility. |

Dependencies flow from presentation to workflows to domain policies/ports; adapters implement ports and `app` wires them. Use small interfaces at actual I/O boundaries, not an interface per struct. Build typed domain models separately from closed output DTOs. Represent omitted/null/false/zero/empty-array distinctly during overlays; ordinary struct zero values must not erase inheritance semantics.

## 4. Delivery milestones

Owner labels below are accountable roles to assign to named people when scheduling, not claims of an existing team. Every implementation ticket inherits its milestone's dependencies, validation and rollback. Milestones are ordered by dependency rather than unsupported calendar estimates.

### M0 — Freeze contracts and resolve implementation risks

**Owner:** engineering lead, with CLI integration and security reviewers. **Dependencies:** none.

1. Record the confirmed Go choice, actual checkout/starter baseline, product non-goals, coverage/unknown truth tables and schema authority. Assign owners to unresolved decisions in §7.
2. Verify Go schema-validator behavior against Draft 2020-12 features actually used by all 16 original and four approved additional contracts; source/effective semantic validation remains separate. Select bounded glob/regex behavior with tests for exact/regex selectors, path scope and original pattern semantics. Any incompatibility blocks that evaluator; do not return a false negative.
3. Run an isolated, explicit compatibility harness against exact `skills@1.7.6`: Node/runtime requirements, supported Git/ref/local source forms, single/group `--skill` grammar, add/list JSON, agent display-name mapping, copy/symlink, partial failures, project/global attribution and cache-local identity after refresh. Store sanitized evidence and dependency versions. This is planned work, not performed by this plan.
4. Keep install execution unavailable until these upstream checks pass. If grouping fails but single-skill calls are proven, use the PRD-permitted single-skill path; unresolved source/identity issues remain blockers.

**Acceptance:** recorded contract/Go baseline; all contracts load with a chosen validator; bounded matcher strategy recorded; real pinned upstream behavior captured or explicit blocking findings recorded. **Risk:** language semantics and upstream identity can invalidate later assumptions. **Rollback/operations:** disposable projects only; no user-workspace installs or global changes. Evidence, not a speculative adapter, is this milestone's deliverable.

### M1 — Product foundation, settings and local registry compiler

**Owner:** core engineer. **Dependencies:** M0 contract/Go decisions; identity metadata for the rename.

1. Initialize product identity, remove obsolete demo command registrations, add typed diagnostics/exit handling, inject workflow dependencies and establish all seven active JSON output contract tests. Implement config get/set with allowed dotted keys, typed JSON values, atomic selected-layer writes and scope restrictions (D4).
2. Add JSON settings at the PRD's user and `.nosey/config.json` paths. Implement defaults, scalar replacement, registry-by-ID merge, atomic agent/exclude arrays, default workspace priority 30, TTL 24h/per-registry override, user/workspace path bases and atomic validation-before-save. User trust remains a separate store.
3. Implement D6 invalid-effective-entity isolation: exclude affected entities/dependent mappings, preserve gaps/provenance, never resurrect superseded base values or infer absence. Source-invalid snapshots are rejected before activation; source-valid partial overlays are compiled with these diagnostics. Implement local provider and registry compilation: manifest v1, technology v1/v2 dispatch, skills/mappings v2 source overlays, optional repositories, post-merge effective validation, duplicate/reference checks and per-field provenance.
4. Preserve keyed child arrays, tombstone/re-enable behavior, atomic source/assessment/when arrays, and explicit empty recommendation clearing. Reject rule-kind/matcher-form changes under an inherited ID with both origins in the diagnostic.
5. Validate authored quality, score, assessment and repository averages; retain null vs zero, reject scores in `repositories.json`, and emit authoring-time drift warnings only as specified. Runtime never calculates repository averages.

**Acceptance:** positive/negative source/effective schema fixtures, legacy v1 technology fixture, scoped IDs, partial overlays and null/presence cases pass; config tests cover both path bases and failed writes. **Risk:** generic config merging silently changes semantics. **Rollback/operations:** no implicit migration of starter TOML to product JSON; preserve old files and document any deliberate migration separately. No application data written by read-only validation.

### M1R — Create the official registry repository and development submodule

Detailed execution sequence: [Skills and mappings import plan](CATALOG_IMPORT_PLAN.md), covering the already verified source catalog, stable identity conversion, updated mapping generation and registry data validation; frontend bonus is excluded.

**Owner:** registry maintainer with repository/CI owner. **Dependencies:** M0 baseline and M1 local schema/effective-validation checkpoint. Map to PRD P1R; remote verification completes with M3.

1. Confirm repository owner, canonical Git URL, visibility, default branch, maintainer and license/attribution policy. Create the separate `nosey-registry` repository; this plan does not create or publish it.
2. Populate `nosey.registry.json` with ID `nosey:official`, required document paths and `empty`/`workspace` template descriptors. Preserve the unchanged 894-identity reference under registry provenance storage and its entries/rules in root `technologies.json`; add only reviewed genuinely new metadata-only identities under D7. Import the user-selected ledger/mappings at commit `d1bc68a826cd233f85f291eca0cfa9b4303713e6` into reviewed v2 skills/mappings. Create explicit technology-ID crosswalk and collision-checked skill-ID map, reuse the supplied source verification/metadata and account for all 877 skills/289 technology entries/35 combinations. Resolve only required missing metadata; do not repeat source audits or import frontend bonus. Optional repository identities and manifest-authored scores retain provenance; upstream verification labels are not ratings. Do not publish example ratings as curated official assessments.
3. Add both external template trees, preserving their versioned generator/output contracts. Keep the data repository free of runtime hooks/plugins, vendored external skill repositories and nested submodules. Add maintenance guidance and CI for schema/effective-reference/template validation, byte-identity of the imported technology asset, and score governance; do not rewrite or re-audit authored detection rules.
4. Publish a validated initial registry commit and record its SHA. Add its confirmed remote to this application as the `registries/default/` Git submodule, recording `.gitmodules` plus the gitlink at that exact commit. Use an absolute canonical URL; do not copy its files into the application Git tree. Updates are reviewed registry commits followed by a separate reviewed application gitlink update, never an automatic branch-following update.
5. Add explicit, nonrecursive initialization of this one trusted submodule for registry integration CI/development. Ordinary app builds must work with it absent; registry integration checks must fail clearly if their required checkout is missing. Release binaries/packages must not embed or bundle its data/templates. Untrusted fork jobs must not receive private registry credentials.
6. Configure the actual official remote/ID at default priority 0. M3 proves a fresh runtime fetch/cache using the remote; M6 proves init uses that snapshot. Neither runtime path reads `registries/default/` implicitly. Local testing may explicitly select the checkout as a local registry, subject to normal validation/trust rules.

**Acceptance:** cloned registry and exact submodule SHA agree; preserved reference-asset bytes match source and runtime entries preserve original rules; all registry/template source schemas and effective references pass; the import has a complete reviewed disposition and ID maps. End-to-end remote/offline empty/workspace initialization is M6 acceptance, not a condition for M1R closure. App build succeeds without submodule; integration tests distinguish missing test checkout from a missing runtime registry. **Risk:** a dev checkout becomes an accidental production fallback or credentials leak in CI. **Rollback/operations:** revert the app gitlink to the previous validated commit and retain last-known-good runtime snapshots; do not force-push registry history or imply the gitlink pins every user's runtime registry. Runtime refs/TTL remain explicit settings. This adds delivery checks, not new public schema fields or replacement AC IDs.

### M2 — First usable slice: local scan → recommendation → dry-run

**Owner:** core/CLI engineer. **Dependencies:** M1 and M0 matcher decision.

1. Implement D5 exact-root resolution (`--cwd` or current directory, never ascend), bounded nested-project discovery, Git ignores/config exclusions, no directory-symlink traversal and narrowly permitted dotenv/MCP reads. Implement bounded file/dependency observations for the example Next.js/TypeScript case, technology evidence, rule states and coverage. Missing providers remain visible as gaps, never inferred absence.
2. Add condition evaluation, basic recommendation combination, shared catalog search and pure install-plan construction. Validate source/agents/conflicts without executing an installer.
3. Expose `scan`, `search`, and install preview/dry-run from thin Cobra handlers, including the full required active-version DTO shapes (scan/search v2, install-plan v3), normalized defaults and deterministic ordering. Implement applicable flags from PRD §5 in the same milestone; do not create a competing command-reference document.
4. Exercise a newly authored temporary workspace with an explicitly supplied local registry: scan finds technologies with evidence, search finds the corresponding skills, and selected skills yield a blocked or executable-looking preview according to policy. Preview cannot execute upstream processes.

**Acceptance:** schema-valid scan/search/plan output and semantic counts; no ANSI/log output on JSON stdout; no TUI under non-TTY; dry-run/print invokes no upstream process and writes no project files; strict incomplete scan exits 4, non-strict incomplete scan warns and exits 0. **Risk:** shipping the slice as full MVP would hide missing capabilities. **Rollback/operations:** read-only workspace access and temporary test state; execution stays disabled. No default-registry launch claim until M3/G2.

### M3 — Trusted remote sources, cache and registry commands

**Owner:** registry/security engineer. **Dependencies:** M1; integrate with M2 workflows. M1R must publish the default remote before default-source acceptance, but custom-source adapter work can proceed independently.

1. Add hardened Git provider and immutable validated snapshot cache: source/ref validation, disabled hooks/filters, safe protocols, symlink containment, bounded fetch, TTL, pins, concurrent refresh locking and atomic snapshot promotion.
2. Implement user-owned trust fingerprints over canonical workspace/registry proposals. Changed entries revoke approval; project settings cannot approve themselves or enable adapters. Incidental untrusted sources are skipped with diagnostics; explicitly mandatory sources fail deterministically.
3. Add registry list/add/remove/update/validate and `trust --revoke`; add/remove persist user settings only. Default validation composes official underneath, while `--standalone` requires self-contained references. Preserve priority tie order and field provenance.
4. Use the supplied official URL once assigned. Distinguish first-use unavailable sources from valid stale/offline snapshots; source-invalid refreshes must retain the previous snapshot. Source-valid snapshots may compile with effective exclusions/gaps per D6; stale effective entities never silently replace currently invalid ones.

**Acceptance:** offline scan/search, failed refresh recovery, priority ties, changed fingerprints, mandatory-vs-incidental error codes, traversal/option injection and concurrent reader/refresh tests. **Risk:** cache poisoning or project-controlled trust. **Rollback/operations:** retain last validated snapshot; revoke trust or disable a source without deleting user registry contents. No executable source content.

### M4 — Complete detector and recommendation/search semantics

**Owner:** detection/catalog engineer. **Dependencies:** M2–M3.

1. Complete `file`, `dependency`, `content`, `extension`, `dotenv`, `detector`, `mcp_package`, including glob/pathRegex scopes and exact/regex dependency and dotenv selectors. Dotenv reads keys only; examples/samples do not count by default.
2. Provide observations for npm, Python, Cargo, Go, Ruby, Composer, Docker, Terraform, Terraform resources, GitHub Actions, Deno and Hex; test selected project scopes/monorepos. Report provider availability explicitly.
3. Keep MCP passive and off by default; never execute MCP commands. Add user-enabled trusted detectors with pinned ID/version/hash, out-of-process JSON, timeouts, bounded output and redaction. A downloaded registry cannot install an adapter.
4. Implement complete required/excluded/not-applicable accounting and the detected/absent/unknown truth tables. Positive evidence wins; exclusions may leave technology unknown with execution complete; relevant errors/budget truncation make required execution incomplete.
5. Complete mapping unions, unknown-blocked entries, highest priority, default reasons, prerequisites, symmetric conflicts/cycles and canonical deduplication. Unknown branches do not add confirmed requirements/conflicts.
6. Finish all shared Scan/Explore/search facets and deterministic comparators, null-last rating sorts, exact case-sensitive IDs and case-insensitive query relevance. Metadata-only entries remain browseable. Preserve authored rating provenance.

**Acceptance:** provider capability matrix, controlled seven-kind corpus, coverage equations, strict/default/MCP failure matrix, secret sentinel tests, scoped IDs, conditional unions and CLI filter/sort parity. Compatibility tests exercise the decoder/engine; they must not rewrite or re-audit the supplied 894-identity catalog. **Risk:** unsupported or truncated evaluation looks like absence. **Rollback/operations:** downgrade failures to explicit unknown/gaps; honor scan cancellation and limits. Required providers still must pass before MVP release.

### M5 — Verified project installation and Manage inventory

**Owner:** CLI integration engineer. **Dependencies:** M0 upstream sign-off, M3 trust/source policy, M4 recommendation graph.

1. Implement the pinned runner and version-specific list/add decoders using M0 evidence. Add `list`, install preflight and doctor checks. Inventory failures yield unknown; unmatched records are unmanaged.
2. Add D2 immutable content-addressed persistent installation-source store under user application data, separate from refresh caches. Materialize cached-registry local skill sources there before execution, with locking/hash verification and atomic promotion; no automatic store GC. Preserve stable logical origin/requested ref/frontmatter name separately from execution/persistent path; record receipts under the published install-origin contract. Receipts are attribution, never proof of current installation.
3. Require selected skills, explicit confirmed agents and TUI confirmation or CLI `--yes`. Validate write access, frontmatter, source/ref forms, conflicts, prerequisites and overrides. For non-TTY project/unpinned overrides require `--approve-plan SHA256` plus `--yes` against D3 install-plan v3. Recompute source/registry fingerprints, validate under a project operation lock, and execute immutable reviewed content. Changed source/ref/agents/method/commands invalidates consent; unavailable exact source resolution blocks usable approval. A matching digest never grants workspace trust or bypasses conflicts.
4. Build argv without a shell, global/all/wildcard flags or unsupported option separator. Group only when proven; otherwise use safe single-skill commands. Keep aligned catalog IDs/names, including null IDs for direct uncatalogued source installs. `--copy` selects copy installation; copying command text remains a distinct UI action.
5. Parse each add outcome and re-query project/agent inventory to corroborate each claimed install. Preserve partial failures, block dependent items, and retry only safe isolated failures. Inherit upstream telemetry by default; disabled mode passes both documented opt-outs; no Nosey product telemetry.

**Acceptance:** fake-runner negative tests plus real pinned disposable-project success/partial failure, agent scope, malformed JSON, cancellation, source/ref and cache-rotation reconciliation, actual symlink/payload readability after deleting refresh snapshots, deterministic cross-language approval hashes and zero invocation on missing/mismatched approval. Exit 5 for installation failure/partial success; exit 6 for incompatible prerequisites. **Risk:** process success is mistaken for per-skill success. **Rollback/operations:** report verified/unknown outcomes and refresh inventory; do not implement automatic uninstall/repair or pretend file-backed receipts undo upstream changes.

### M6 — External registry initialization

**Owner:** registry engineer. **Dependencies:** M3 sources/cache, M4 workspace seeding, M1R official templates and publication.

1. Load `empty`/`workspace` descriptor and files from the exact validated external official snapshot or explicitly selected configured provider. Local providers and complete cached templates work offline; no bundled fallback.
2. Implement interactive and non-TTY init through the same service. Require non-TTY path, template/from-scan and `--yes`; honor repeatable include-tech selections. Seed only confirmed selected definitions/references; never vendor external skills.
3. Validate descriptor paths, output schemas/effective references, local skill paths and destination collisions before committing staged output. Add scaffold provenance with `sourceRevision` (Git SHA or local null) and `createdBy`.
4. Implement opt-in `--git` and user-scoped `--register`, with explicit reporting/recovery for failures in these follow-up actions. Do not replace or delete pre-existing content during cleanup.

**Acceptance:** both templates, local/offline/pinned providers, valid v2 generated skills/mappings, output v2, traversal/symlink/collision rejection and fault-injected writes leave no partial generated output. **Risk:** multiple filesystem/config/Git effects cannot be one universal transaction. **Rollback/operations:** stage generation; track created artifacts and report follow-up failures accurately; compensate only owned changes, never existing user files.

### M7 — Four-tab TUI using shared workflows

**Owner:** TUI engineer. **Dependencies:** M2 for an initial shell; final acceptance requires M3–M6.

1. Replace the demonstration root with default `nosey`/`nosey ui`, auto-scan and a one-column scroll area with persistent header and context footer (at most six bindings). Reuse theme/responsive helpers without carrying directory-manager behavior into the product.
2. Build Scan's separately focusable technology/recommendation sections, independent filters/sorts, evidence/details and three distinct empty states. Filtering does not rescan; preserve selections and total/visible counts.
3. Build Explore technology/repository grouping and collapse, live query/facets and unique-ID selections across duplicate rows. Show hidden selections in the install plan. `a` acts on visible eligible skills; every skill begins unchecked.
4. Build Manage inventory (including unmanaged/unknown), Settings registry editing/TTL/installation preferences, explicit trust Approve/Revoke and New registry vs Add existing. Connect init wizard to M6 without a nested Bubble Tea program.
5. Implement exact Tab/Shift+Tab, search exit, modal focus trap, arrows, Enter/Space, `f`/`s` and cancellation semantics from PRD §4. Preserve session query/cursor/selection state across tabs; refresh removes unavailable selections with notice. Run asynchronous work through commands and cancel stale work safely.

**Acceptance:** keyboard-driven launch → scan → filter → hidden selection → review → cancel/execute → refreshed inventory journey; Settings trust/init journeys; narrow terminals, NO_COLOR, resize and signal cleanup. Use model tests and focused terminal integration tests rather than relying on screenshots alone. **Risk:** hidden selection or focus leaks change the reviewed install set. **Rollback/operations:** cancellation preserves project state before execution; installed outcomes remain per-skill verified after execution; retain scriptable commands for diagnostics.

### M8 — Release verification and onboarding

**Owner:** QA/release owner, registry maintainer and product owner. **Dependencies:** M0–M7 including M1R and all decision gates.

1. Reconcile every AC below with named tests and recorded evidence. Validate all seven active JSON output schemas and semantic invariants, plus D1–D7 acceptance, help/flags, exit codes, malicious source/ANSI/secret inputs, offline/concurrency/cancellation behavior and real pinned upstream compatibility.
2. Run format checks, `go test ./...`, race tests, vet, custom lint, coverage and build through the adapted Makefile/CI. Expand core coverage beyond the starter runtime packages with meaningful behavior tests. Add supported-platform build and runtime evidence; cross-compilation alone is not terminal/install support proof.
3. Measure separate cold-fetch and warm-cache distributions on a named runner/tree. Approve the platform/terminal matrix and recommendation study protocol. Record provisional ≥95% technology correctness, ≥80% precision@3, ≥80% actionable coverage, ≥90% task completion and warm p95 <2s results; do not present these as achieved beforehand.
4. Confirm official URL/catalog/templates, editorial scoring rubric, named maintainer, correction/reassessment process and source attribution. Update the application README for onboarding, installation prerequisites, trust and essential workflows after commands stabilize. Preserve upstream licenses/notices; do not generate redundant command-reference pages.
5. Prepare reviewed release artifacts and recovery instructions. Publishing/signing/release automation requires a separate explicit request.

**Acceptance:** PRD G1–G7 each has named owner and actual exit evidence; no unverified install or completeness claims. **Risk:** planning assets get mistaken for runtime verification. **Rollback/operations:** retain prior binary and validated snapshots; replace binary/disable bad source independently; do not roll back or delete user-installed skills automatically. Future persisted-format changes require explicit versioned migration/recovery, not silent rewrites.

## 5. Dependencies and first implementation batch

Dependency order: M0 → M1 → M2; M1 → M1R publication → M3 default-source verification; M1 → M3; M2 + M3 → M4; approved M0 installer evidence + M3 + M4 → M5; M3 + M4 → M6; M7 can begin against M2 services but closes after M5/M6; all milestones feed M8. Upstream verification is intentionally early because identity/parser findings can affect the core data model.

The first implementation batch should close M0 and start M1 with: typed states/diagnostics and exit mapping; approved schema validation with positive/negative cases; presence-aware local registry decoding/merge; JSON settings defaults/paths; and workflow injection. Then complete the M2 local scan/search/dry-run slice. Start M1R after local validation works so the official repository/templates are ready for M3/M6. Do not begin by building all screens or by replacing the sample registry with a bundled runtime registry.

No dates are assigned without staffing and M0 results. Break milestones into reviewable tickets carrying owner, AC IDs, affected schema names, acceptance tests, data effects and rollback. The original source package and archived previous draft remain unchanged; approved extensions live in `docs/prd/schemas/` and D1–D7, maintained with the current Go PRD/plan.

## 6. Acceptance traceability

Every original criterion is assigned below. A milestone assignment means planned responsibility, not implemented/tested status. Follow the source §14 wording; tests are to be authored during implementation.

| Milestone | Primary AC ownership | Required evidence |
| --- | --- | --- |
| M0 | 41 | Exact pinned source/parser/runtime compatibility, approved Go contract semantics; feeds M5 acceptance. |
| M1 | 04, 05, 06, 18, 19, 20, 30, 32, 35, 36, 37, 56, 57, 62, 63, 65 | Source/effective/legacy contracts, overlays, provenance and rating semantics; D4 config/output specifications and D6 invalidation tests. |
| M2 | 11, 16, 52 | End-to-end schema-valid CLI slice, all normalized defaults/semantic invariants, no-execution preview; rerun as capabilities expand. |
| M3 | 15, 38, 40, 51, 54 | Offline/cache retention, trust/source security, deterministic merge and validation composition. |
| M4 | 03, 07, 17, 22, 31, 33, 34, 47, 48, 49, 58, 59, 60, 64, 67, 69 | Bounded provider/rule execution, unknown-safe mappings, capability/coverage equations, query/filter parity. |
| M5 | 10, 12, 13, 39, 42, 43, 44, 45, 46, 66, 70 | Proven argv/source/agent policy, logical identity, add/list attribution, per-skill project verification and cache rotation. |
| M6 | 26, 27, 28, 29, 53, 68 | External/cached template generation, source revision, no overwrite/vendor payload, atomic output. |
| M7 | 01, 02, 08, 09, 14, 21, 23, 24, 25, 50, 71 | Four-tab keyboard journeys, settings/trust persistence, hidden selections, grouping and CLI/TUI service parity. |
| M8 | 55, 61, 72 | Full release regression, immutable reference assets, measured usefulness/editorial gates. |

D1–D7 are additional acceptance requirements with ownership in the approved-decisions chapter; the original 72 AC IDs remain unchanged. M1R adds repository/submodule acceptance supporting AC-15, AC-26–AC-29, AC-55, AC-61 and AC-68 plus G2; their primary ownership above stays unchanged.

Cross-cutting NFR ownership: M1/M2/M8 cover output contracts (NFR-02); M3 covers snapshot/trust safety (05/06); M4 covers completeness/capabilities (03/12); M5 covers install confirmation/verification/telemetry (01/04/09); M7 covers persistent UI state (07); M8 owns performance evidence (08). Atomic writes (10) and secret/terminal sanitation (11) are acceptance gates at every relevant I/O boundary, not last-phase cleanup.

## 7. Open decisions and handoff gates

| Item | Accountable role | Needed by | Effect while unresolved |
| --- | --- | --- | --- |
| Real module path, repository URL and author | Product/repository owner | M1 identity initialization | Keep existing module/entry path while developing isolated core behavior. |
| Schema/glob/regex strategy and immutable starter baseline (Go choice resolved) | Engineering lead | M0 closure | Do not silently replace normative matching or schema semantics. |
| Exact upstream runtime/argv/add/list/agent/source identity evidence | CLI integration lead | M0/M5 | No executable installer sign-off; safe read-only/preview work can proceed. |
| Official `nosey-registry` owner/URL/visibility, submodule SHA, catalog/template revision, maintainer and recovery policy | Registry maintainer/repository owner | M1R creation, M3 default integration, M6 default init, G2 | Repository creation/submodule addition remain planned until identity is confirmed; explicit local/custom providers support core development. |
| Supported OS/terminal matrix; Node/Git prerequisite policy | Engineering/release lead | M0 spike scope, finalized M8 | Do not claim platform support from Go builds alone. |
| Cache locking, cancellation and Windows atomic-write behavior | Engineering/security lead | M3/M6 | Require fault-injection/runtime evidence before persistence sign-off. |
| Corpus/runner, recommendation study thresholds and official-score rubric | Product, QA and registry owners | G3/G6/G7 | Targets remain provisional; launch and official scoring claims blocked. |

Engineering handoff: target individual developers selecting agent skills in a project; existing code is a Go starter with reusable CLI/TUI infrastructure; intended scope is the complete §15 MVP. Core contracts are the supplied schemas plus semantic invariants, not screen structs. Security constraints are project-only confirmed installs, user-owned trust, data-only registries, bounded observers and honest unknown states. All milestones above are planned; the immediate next step is M0 compatibility/contract work, followed by the M1 local registry foundation.


## 8. Comparison with the supplied Go PRD implementation plan

The supplied [§21 roadmap](docs/15-delivery-and-risks.md) uses P0–P6; this repository plan uses finer M milestones. They agree on the stack, full MVP, schema-first core, early pinned-upstream proof, local scan-first slice, shared services, external-only registry/templates and G1–G7. Go is now resolved, replacing this plan's earlier proposed language adaptation.

| Supplied PRD phase | Repository milestones | Reconciliation / retained detail |
| --- | --- | --- |
| P0 contractual spike | M0 and M1 identity/contract baseline | Record immutable starter provenance and launcher choice. Freeze contracts in P0; implementation test evidence completes G1 later, avoiding a requirement to finish the full suite before implementing it. |
| P1 registry core | M1 + M3 | Keep separate local compiler/settings and hardened remote/cache/trust work. JSON settings must replace the starter's TOML demonstration semantics. |
| P1R official repository/submodule (added here) | M1R, with M3 remote verification | Both supplied plans previously left the registry URL/publication as a blocker without a concrete creation/submodule task. Add owner, data/templates, CI, immutable publication and exact gitlink steps. |
| P2 detector | M2 initial provider slice + M4 full capability matrix | Keep the synthetic local Next.js/TypeScript scan early; all seven rule kinds/providers remain mandatory for MVP. |
| P3 recommendation/search | M2 basic end-to-end + M4 full catalog semantics | Add schema-valid search/dry-run beside the early scan; preserve unknown-safe conditions, independent filters and determinism. |
| P4 installation | M5 (M2 pure preview first) | Retain explicit per-skill post-verification, cache-path-independent identity, additional override trust and precise rollback limits. |
| P4R external registry init (made explicit here) | M6 | The supplied phase table put the wizard in P5 but did not isolate its generation service. Make external template generation/atomicity testable before attaching the wizard. |
| P5 TUI/settings | M7 with M1/M3 persistence services | Initial Scan projection may start after M2; finish all tabs and keyboard journeys after installation/init services. No nested terminal loops. |
| P6 release hardening | M8 | Keep platform/runtime evidence, performance/studies, 72-AC ownership, fault recovery and actual gate sign-off. |

The supplied architecture's nested `internal/domain/...`, optional `internal/contract/` and schema-adapter paths and this plan's cohesive top-level packages are alternatives, not two systems to build. Use §3 here as the initial repository layout, preserve the dependency direction in the PRD, and split packages only when the first feature needs them. Do not duplicate contracts, engines or schema sources just to satisfy both example trees.

Local verification performed during the earlier handoff: the unchanged starter passed `go test ./...` and `make format-check build`. This is evidence about the current starter, not Nosey functionality or the archive author's upstream inspection. The registry repository/submodule, compatibility harness and all Nosey services remain unimplemented. Draft.4 additionally authors four specification schemas and inspects the selected remote catalog; neither is runtime implementation. No remote repositories, submodules or publishing actions were created during these documentation updates.


## 9. Approved gap resolutions and implementation checks

Use [D1–D7](docs/17-approved-decisions.md) as the full contract. Decisions are settled, not an invitation to reinterpret them during implementation:

| Decision | Concrete implementation obligation | Milestone |
| --- | --- | --- |
| D1 acyclic delivery | Close M1R on validated publication/gitlink; remote fetch in M3 and runtime/offline init in M6 | M1R/M3/M6 |
| D2 durable payloads | Persistent installation sources independent of disposable caches; no automatic GC; test actual installed file usability | M5 |
| D3 reviewed approval | v3 preview, source/revision fingerprints, deterministic SHA-256, `--approve-plan` + `--yes`, immutable exact-content execution | M2 preview / M5 execution |
| D4 complete CLI contracts | Seven versioned JSON outputs, typed config get/set, all advertised flags assigned | M1/M2/M5/M8 |
| D5 scan boundary | Exact cwd/`--cwd`, bounded nested projects, ignores/exclusions, no directory symlinks, explicit dotenv/MCP exceptions | M2/M4 |
| D6 invalid overlays | Source-valid snapshots distinct from effective validity; exclude affected/dependent entities with unknown/gaps and no lower-layer resurrection | M1/M3/M4 |
| D7 initial registry input | Pinned ledger/mappings conversion, all-entry disposition, reviewed ID crosswalk and skill-ID map; new identities metadata-only | M1R |

M0 still selects tested Go schema/glob/regex implementations and verifies pinned upstream behavior. Repository owner/URL/visibility, module identity, OS/terminal support and measured launch-study results still need concrete values/evidence. Do not ask again whether to use Go, persistent sources, review-bound approval, exact-root scans or the approved invalidation policy; those choices have been made.
