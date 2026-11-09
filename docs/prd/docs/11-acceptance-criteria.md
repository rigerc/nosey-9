# Acceptance criteria

[← Specification index](../README.md) · [Previous](10-errors.md) · [Next](12-scope-and-decisions.md)

## 14. Testable acceptance criteria

**AC-01:** `nosey` opens a TUI; before any manual action it begins a workspace scan and shows the current workspace path.

**AC-02:** The header logo/menu and dynamic footer remain visible while each content screen scrolls. Tab and Shift+Tab cycle precisely among four tabs, including when the search field has keyboard focus (Tab leaves search); while a modal is active, Tab navigates modal focus only.

**AC-03:** A technology is `detected` only with positive sanitized evidence; `absent` requires every relevant active applicable rule to have evaluated `not-matched`; otherwise it is `unknown`. Positive evidence wins over unevaluated rules. Details expose rule ID/kind and safe workspace-relative evidence; execution completeness and coverage are reported separately.

**AC-04:** The example registry declares a technology document at version 2, decodes without changing its Next.js/TypeScript evidence, and the v1 loader remains compatible with legacy documents. Metadata-only technology identities are permitted without detection rules and are not reported as detected.

**AC-05:** An override registry changes one property without losing unrelated base properties; keyed child arrays merge by ID; in mappings an explicit empty `recommendations: []` clears inherited items; atomic fields replace as specified.

**AC-06:** `disabled:true` hides an entity from effective results and a higher-priority `disabled:false` restores it. Invalid references are diagnosed.

**AC-07:** Mapping conditions, deduplication, priority ordering, prerequisites, conflicts, and cycles behave deterministically; unit-test implementation is a later engineering deliverable.

**AC-08:** Explore live-filters the merged catalog by query, registry, technology, source repository, installed state, quality, minimum skill score, and minimum repository average. A skill may be manually selected without a matching detection.

**AC-09:** All skills start unchecked; Space toggles an item, `a` toggles visible items, and `i` opens a plan. Escape cancels without writing project files.

**AC-10:** Each executable plan groups by source/ref/agent/method when a tested upstream multi-skill grammar supports the grouping; otherwise it emits separate safe calls. It uses only project scope and explicitly confirmed agents. No `--global`, wildcard agents, or implicit `--all`.

**AC-11:** Dry-run and print modes execute no install processes. Executable install requires TUI confirmation or CLI `--yes`.

**AC-12:** Git-backed and local skill sources are installable when compatible with the actual CLI; unsupported sources generate actionable errors, not direct-copy fallbacks.

**AC-13:** Following install, each item receives independently verified installed/missing/unknown status from the versioned inventory reader. Ambiguous and unmanaged skills are surfaced; errors never imply missing. UI returns to Scan with results.

**AC-14:** Settings changes persist; project scalar overrides, per-ID registry merges, atomic replacement of selected arrays, user/project path bases, default priority 30 and per-registry TTL follow the published merge contract. Untrusted project registries do not activate until user-approved.

**AC-15:** A refresh failure never destroys a valid cache, and the app can still scan/recommend offline using cached sources.

**AC-16:** Every CLI JSON response satisfies its closed output schema (including normalized filters/sort, required evidence, coverage, provenance and `diagnostics`), with semantic consistency of counts and IDs; defined exit codes behave as §5.1 requires and noninteractive commands never launch the TUI. Only `scan --strict` turns an incomplete scan into exit 4.

**AC-17:** Implementation acceptance checks reject unsafe Git URI/ref/path forms, symlink escapes and hooks/filters; project config cannot enable adapters. Trusted user adapters use pinned ID/version/hash, out-of-process JSON I/O and timeout, and obey evidence redaction.

**AC-18:** A skill's optional `quality` enum and `score` within 0–100 validate; out-of-range scores or unknown labels fail with specific JSON paths. Omitted scores remain null and `0` is valid.

**AC-19:** Optional `repositories.json` defines repository identity only. It rejects `averageScore` and `scoreCount`. The registry manifest allows optional `repositoryScores` entries by ID and validates ranges, sample counts, and references.

**AC-20:** Repository scores are authored solely in `nosey.registry.json` as `repositoryScores[repositoryId].averageScore`; absent means `null` and no runtime aggregation occurs. `scoreCount` is shown when provided; undeclared repository IDs produce diagnostics.

**AC-21:** Explore presents every matching result under a technology group and repository subgroup. Skills belonging to multiple technologies may appear twice visually but are selected, counted, and installed once by stable skill ID. Group collapse preserves selection; groups without visible results disappear.

**AC-22:** Explore and CLI search share exact case-insensitive query matching and a relevance score ordered by ID/name exact, prefix, substring, tags, description, technology and repository, with deterministic tie-breakers, null-last rating sorts and grouping.

**AC-23:** Scan provides keyboard-accessible `f` filtering and `s` sorting for the recommended-skills section; detected technologies are filtered and sorted through `nosey scan` flags. `Esc` cancels edits, and `Enter` applies them. Scan displays visible/total counts; `Clear filters` restores all rows.

**AC-24:** Technology sorting, available through `nosey scan`, supports alphabetical ascending/descending, evidence count, and matched rule count. Recommendation sorting supports priority, score, quality, repository average, and name. Filter combinations and all sorting modes have stable tie-breakers, preserve detection/recommendation data, and produce reproducible results without rescanning.

**AC-25:** Selections persist through filters/sorts; `a` toggles only visible eligible skills (including unknown state), and hidden selections appear in the plan. `scan --rec-technology ID`, `--query-target`, and `--status unknown` reproduce the TUI semantics without altering detected data.

**AC-26:** `nosey registry init` reads a template descriptor and files from the selected validated external Git/local registry, not from a bundled template archive or from an undocumented baked-in scaffold. Official default is used unless a custom source is explicitly selected.

**AC-27:** Init `empty` creates all required valid registry files; `workspace` offers a preview of detected technologies and seeds only user-selected definitions/references. Output passes schemas, reference checks and local-path validation. External skills are not downloaded or vendored implicitly.

**AC-28:** Template descriptor/file paths cannot traverse or escape the source checkout or destination, and templates cannot invoke code/hooks. Existing destination content is never overwritten without a separately designed explicit workflow.

**AC-29:** Offline init succeeds from a complete validated template cache or an accessible local provider, but fails cleanly with no output when neither exists. The generated `nosey.scaffold.json` records the source registry, template ID and revision. `--git` and `--register` are opt-in.

**AC-30:** Existing version-1 partial override and effective definition rules remain unchanged. Technology Registry v2 documents are dispatched by their own `schemaVersion`; effective `metadataOnly: true` identities may have no rules and produce no positive detection. The supplied catalog/rules are not revalidated or rewritten by the v1.7.1 specification update.

**AC-31:** All seven rule kinds (`file`, `dependency`, `content`, `extension`, `dotenv`, `detector`, `mcp_package`) remain supported in the specification; this planning package does not ship test fixtures or re-audit the verified registry rules. `file` supports `glob` and `pathRegex` with `relative`/`basename` scope; dependency and dotenv selectors each support exact and regex forms. File/content glob strings are not interpreted as regex. Multiple rules use OR and retain per-rule evidence.

**AC-32:** A higher-priority registry can override `nextjs.rules[next-config].glob` without replacing another Next.js rule. An `id`-only rule fragment that cannot resolve to a complete inherited rule fails effective validation. Reusing a rule ID with a different `kind` or changing mutually exclusive matcher forms under the same ID is rejected with a diagnostic containing both source registries.

**AC-33:** Path normalization, ignore lists, bounded runtime evaluation, oversized-file skipping and monorepo evidence are release-verifiable. Optional MCP/adapter opt-outs report `excluded-by-mode` without automatically failing `complete`; missing required providers and relevant truncation make `complete=false`. All gaps are reported without false negatives or modifying supplied patterns.

**AC-34:** Dotenv detection reads only variable **keys**; future security acceptance verifies values never appear in logs, search results, JSON output, diagnostics, or evidence. `.env.example`/`.env.sample` do not count by default. No downloaded registry can install or execute a detector implementation.

**AC-35:** Authored manifest `repositoryScores` scores validate 0..100, positive `scoreCount`, strict repository ID pattern and unknown-field rejection; `repositories.json` rejects scores.

**AC-36:** Repository score merges are by ID/field, with provenance; an override of one score preserves others, unknown declared IDs warn, virtual repositories remain unrated.

**AC-37:** Authoring-time drift checks warn iff the authored average differs from the same-registry scored-skill mean by more than 0.5 and skip repositories with no rated skills.

**AC-38:** Workspace registry trust is fingerprinted by canonical workspace and registry entries; Settings presents pending fingerprint detail and explicit Approve/Revoke actions backed by the user-owned trust store. Edits revoke approval, non-TTY scans skip untrusted auto-discovered entries with diagnostics, and project registries cannot activate adapters.

**AC-39:** Install preview and dry-run flag source/repositoryId overrides, show registry chain, and demand extra approval for project/unpinned overrides.

**AC-40:** Git source URI/ref and template/local paths reject adversarial CLI options and traversal at schema and semantic layers; clone disable-hook/filter protocols and external symlinks.

**AC-41:** Pinned Skills CLI invocations use validated argv without a shell or unsupported `--` separator; Node/version compatibility and missing features fail safely. Upstream parser/JSON behavior requires version-pinned integration proof before implementation sign-off.

**AC-42:** Installed identity uses canonical logical origin/requested ref and upstream frontmatter name, not a temporary cache path. `list --json` agent display names and upstream tracking states map conservatively to Nosey agent IDs and installed states; ambiguity yields `unknown`, and unmatched installed skills appear unmanaged.

**AC-43:** Commands group only with pinned tested multi-skill argv semantics; `add --json --yes` attributes each member success/failure and post-install `list --json` verifies project agent scope. Retry safely isolated failures only; process/group success alone cannot assert per-skill success.

**AC-44:** Auto-detected agents only prefill the confirmation screen; explicit confirmed agents are required and no global/all/wildcard flags are passed.

**AC-45:** Upstream telemetry environment is inherited by default; disabled mode passes documented opt-out environment variables and Nosey emits no own MVP product telemetry.

**AC-46:** Pinned `skills@1.7.6` source refs use supported `#ref` syntax, never `owner/repo@REF`; the latter is a skill selector. Ref concatenation is gated per source form, unknown combinations fail preflight, and plans preserve requested refs without inventing resolved commits.

**AC-47:** Catalog-level requirements/conflicts and passing mapping entries union; highest mapping priority wins, missing priority defaults optional, cycles block selections and installed conflicts require an explicit path.

**AC-48:** Relevance search order and unknown installed state work identically in UI and CLI, with deterministic observable output.

**AC-49:** Both `nosey scan --rec-technology` and `--query-target` faithfully reproduce Scan filtering scopes and preserve original detected recommendations.

**AC-50:** Footer never exceeds six bindings; the Scan technology strip never takes focus, and Tab exits text search to switch top-level section.

**AC-51:** `registry validate` default merges official underneath; `--standalone` treats unknown official references as errors.

**AC-52:** All four machine-readable output commands implement their complete, closed nested JSON Schema contracts, including normalized filters/sort, coverage, evidence, provenance, conflicts and null-vs-zero ratings. Acceptance includes rejection of missing/invalid fields and semantic consistency of counts and references; no fixture files are shipped in the planning package.

**AC-53:** Non-TTY registry init with `--from-scan --yes` includes detected definitions/references or chosen repeatable `--include-tech`; generation metadata and machine output consistently use `sourceRevision` as validated Git SHA (null for local), with `createdBy`, and no skill payload is vendored.

**AC-54:** Per-registry ttlHours, registry priority ties, user/project settings merge and path bases have deterministic, acceptance-verifiable semantics.

**AC-55:** Future CI release gates include legacy detector support, monorepo and pinned upstream installer verification, v2 decoder/capability integration and MCP opt-in behavior; requirements are mapped to milestones and logged. The verified 894-technology catalog is not subjected to additional rule-validation gates.

**AC-56:** Loader selects `technologies.schema.json` for technology document `schemaVersion: 1` and `technologies.v2.schema.json` for version 2 without changing the manifest `schemaVersion: 1`; unsupported document versions produce actionable diagnostics and preserve cached sources.

**AC-57:** Scoped technology IDs including `@react-three/fiber` are accepted unchanged in skill associations, mapping technology references and mapping conditions. No implicit alias mapping occurs.

**AC-58:** All 19 supplied `metadataOnly` identities remain indexable for Browse/Explore and never become detected without positive rule evidence; rule-backed identities are separately identified from actually evaluated detector coverage.

**AC-59:** Missing required Docker/Terraform/GitHub Actions/Deno/Hex observation providers or explicitly enabled trusted detectors yield `not-evaluated`, `complete=false` and a capability warning. Deliberately excluded optional detectors do not fail strict mode; both preserve positive evidence and never create false negatives.

**AC-60:** MCP config inspection is disabled by default and can be opted into via Settings or `scan --enable-mcp`; opt-out records excluded coverage but does not alone make the scan incomplete. Opted-in provider failure does. No MCP commands execute; Scan JSON reports capability state without leaking secrets.

**AC-61:** Technology Registry v2 catalog and its supplied v2 schema remain byte-for-byte unchanged in this planning package. No rule-pattern rewrites, rule-quality audits, runnable validation scripts or test fixtures ship with the specification.

**AC-62:** `skills.json` and `mappings.json` source documents are version 2; their explicit field names `source.skillName`, `technologyId`, `recommendations[]` and `skillId` pass their new schemas. Pre-release v1 skill/mapping documents need no decoder or migration path.

**AC-63:** Full active entities pass `skills.effective.schema.json` and `mappings.effective.schema.json` after ID-based merging. Incomplete active entities and duplicate IDs are rejected, while valid partial source overlays remain permitted. Atomic source, assessment, when and scalar-array replacement, plus explicit empty-recommendations clearing, are verified.

**AC-64:** An absent `when` passes; every present technology-condition array must be nonempty. Unknown technologies never count as absent; `scan --json` separates confirmed recommendations from `unevaluatedRecommendations` with blocked unknown technology IDs.

**AC-65:** An optional authored `assessment` validates method, ISO date, HTTPS evidence URL and assessor; absent score is distinct from `0`. `unassessed` quality and `unscored` score filtering are independent. Field-provenance labels are sourced from effective registry merges, not invented by assessment metadata.

**AC-66:** Dry-run commands expose aligned `skillIds[]` (null for uncatalogued skills) and upstream `skillNames[]`, actual execution source and stable logical origin. Git skill names cannot resemble options or paths. Local frontmatter matches `source.skillName`; unknown required IDs, self-links, cycles, contradictory relationships and asymmetric conflicts are rejected.

**AC-67:** Multiple passing mappings for the same canonical skill deduplicate installation, choose highest priority, union requirements/conflicts and accumulate reasons deterministically. Unknown-condition entries do not contribute to those confirmed unions. Missing reason text yields an evidence-backed default explanation.

**AC-68:** External templates generate valid v2 skills/mappings and `nosey.scaffold.json` with `sourceRevision`/`createdBy`, while workspace seeding preserves canonical references and leaves supplied Technology Registry v2 rules untouched.


**AC-69:** A default scan with MCP intentionally disabled and all required evaluations complete returns `complete=true` (strict exit 0) while MCP-only technologies may be `unknown`. Explicitly enabled MCP or adapter execution failure, required provider absence, or relevant truncation yields `complete=false` (strict exit 4). Coverage counters and gaps reconcile.

**AC-70:** Installing a local skill from a content-addressed Git-registry checkout, then rotating/refreshing the cache, preserves one stable logical identity; a stale cache path alone cannot cause false `missing`, duplicate Manage records, or false installed claims. Project provenance is a receipt, not proof of current installation.

**AC-71:** Settings offers explicit workspace-registry approval/revocation; `registry add/remove` affect user settings only; mandatory source errors have deterministic codes; untrusted incidental sources are skipped; and `install --source` is marked `uncatalogued` with no invented score or catalog identity while retaining all preflight restrictions.

**AC-72:** Launch approval requires documented representative recommendation-quality results (precision@3, actionable coverage and usability completion), and a named owner/rubric/reassessment and correction procedure for officially curated scores; independently authored ratings remain attributed to their source registries.
