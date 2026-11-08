# Registry data contracts

[← Specification index](../README.md) · [Previous](03-cli-contract.md) · [Next](05-registry-trust-merge.md)

## 6. Registry architecture and layout

A registry is a self-contained Git repo root or local directory root. The manifest lists three required JSON document paths (technologies, skills, mappings) and an optional `repositories.json` path; collections may be empty. A custom registry can contain extra `SKILL.md` folders and references to any supported external skill source. The manifest and JSON Schemas are **proposed Nosey v1 contracts**. A registry may optionally advertise registry-init templates; ordinary registries need not provide them.

```text
nosey-registry/
├── nosey.registry.json
├── technologies.json
├── skills.json
├── repositories.json        # optional; Git/local repository identity metadata
├── mappings.json
├── skills/
│   └── workspace-style/
│       └── SKILL.md
└── README.md
```

### Default registry repository and development submodule

**User-requested delivery addition (Go PRD draft.3):** Create a separate default registry Git repository, provisionally named `nosey-registry`, and attach it to the Nosey application at `registries/default/` as a Git submodule. Owner, canonical remote URL, visibility, license and named maintainer must be confirmed before creation; no remote repository or submodule exists as a result of this specification update. Track this work as [P1R](15-delivery-and-risks.md) / [M1R](../IMPLEMENTATION_PLAN.md#m1r--create-the-official-registry-repository-and-development-submodule).

The data repository must contain:

- A manifest with stable registry ID `nosey:official`, required technology/skill/mapping document paths and both `empty`/`workspace` template descriptors; optional repository identities and authored manifest scores follow the existing contracts.
- The original 894-identity asset retained byte-for-byte under registry provenance/reference storage, with its entries/rules preserved in root `technologies.json`. The approved D7 crosswalk may add genuinely new metadata-only identities separately to that runtime document; no original entry/pattern rewrites or rule-quality re-audits.
- Reviewed conversion of the user-selected pinned `rigerc/skills-catalog` ledger/mappings into v2 skill/mapping definitions and provenance, plus `templates/registry/empty/` and `templates/registry/workspace/` with valid data-only descriptors/files. Supplied example scores are illustrative and must not be promoted to official assessments without editorial review.
- Licensing/attribution, maintainer/recovery policy and validation CI for schemas, effective references, template generation, source-byte preservation and authored scores. No executable registry hooks/plugins, nested submodules or implicitly vendored external skill repositories.

Apply the [approved catalog import/crosswalk requirements](17-approved-decisions.md#d7--initial-official-catalog-import), backed by [inspection evidence](../catalog-import-inspection.json). M1R closes on source/template validation and publication/submodule setup; end-to-end init acceptance belongs to M6, avoiding a dependency cycle.

After validating and publishing its initial immutable commit, record the real URL in the application's official-source configuration (priority 0), and add `.gitmodules` plus an exact gitlink at `registries/default/`. This is a **development/integration checkout**, not a bundled runtime registry or a replacement for Git snapshots. The application must build without it, and the released binary/package must not embed or distribute its registry/template payloads. A missing first-run runtime registry still follows §5's unavailable-source behavior; the app cannot silently fall back to the development checkout.

Developers and registry integration CI explicitly initialize only this known submodule at its recorded SHA, nonrecursively. Do not follow a moving branch or run `submodule update --remote` automatically. Review registry changes in its repository, then review the application's gitlink update separately. CI must identify missing required integration data clearly and must not provide private remote credentials to untrusted fork jobs. Runtime registry refreshes must never initialize submodules from fetched registries; the existing Git hardening rule is unchanged.

Validate a fresh external remote fetch and both init templates through the normal source/cache pipeline, then repeat offline from a complete validated snapshot. A submodule checkout can be selected explicitly as a local source for development, subject to the usual validation/trust rules; its Git membership confers no approval. Reverting a development gitlink does not pin or roll back a user's runtime registry: runtime ref/TTL configuration and last-known-good cache recovery remain separate.

### Manifest — `nosey.registry.json`

```json
{
  "schemaVersion": 1,
  "id": "example:nosey-registry",
  "name": "Example Nosey Registry",
  "description": "Opinionated workspace rules and skills",
  "files": {
    "technologies": "technologies.json",
    "skills": "skills.json",
    "mappings": "mappings.json",
    "repositories": "repositories.json"
  }
}
```

The full machine-readable JSON Schema files (draft 2020-12), with a validation-ready example, are in `schemas/` and `example-registry/`.


### Registry-init templates — external source of truth (normative)

**Where templates live:** The scaffold for `nosey registry init` is hosted in the **official default Git registry**, under `templates/registry/{template-id}/`. It is **not** embedded in the Nosey binary, not independently downloaded from a public template marketplace, and not bundled or exported as a ZIP. The Nosey binary contains only the configured official registry location, a template reader/renderer and versioned schemas. This is a design requirement; the concrete official URL remains a release configuration choice.

The official registry publishes an **optional** `templates` list in its root `nosey.registry.json`:

```json
{
  "schemaVersion": 1,
  "id": "nosey:official",
  "name": "Official Nosey Registry",
  "files": {
    "technologies": "technologies.json",
    "skills": "skills.json",
    "mappings": "mappings.json"
  },
  "templates": [
    { "id": "empty", "kind": "registry", "descriptor": "templates/registry/empty/nosey.template.json" },
    { "id": "workspace", "kind": "registry", "descriptor": "templates/registry/workspace/nosey.template.json" }
  ]
}
```

**Remote source tree** (an ordinary Git checkout, not a distributable bundle):

```text
nosey-official-registry/
├── nosey.registry.json
├── technologies.json
├── skills.json
├── mappings.json
└── templates/
    └── registry/
        ├── empty/
        │   ├── nosey.template.json
        │   └── files/
        │       ├── nosey.registry.json
        │       ├── technologies.json
        │       ├── skills.json
        │       ├── mappings.json
        │       ├── repositories.json
        │       └── README.md
        └── workspace/
            ├── nosey.template.json
            └── files/...
```

Each template descriptor (`nosey.template.json`) is validated against `schemas/template.schema.json`. It declares `schemaVersion: 1`, `id`, `kind: "registry"`, `generator: "registry-v1"`, and a list of `{source, destination}` paths. The `source` path is relative to the descriptor directory; `destination` is relative to the new registry root. Paths must be confined and free of symlink escapes; duplicate destination paths are rejected. Template processing **only copies data files and performs Nosey-owned JSON edits**, with no shell hooks, executable code, arbitrary remote fetching, script interpolation, or postinstall steps.

**Initialization sequence:**

1. Locate the official default registry (or explicitly provided `--template-registry ID`) in settings. Resolve its validated Git snapshot (prefer cached snapshot immediately, refresh according to registry TTL; optionally refresh before init when user requests). Local source registries are read live.
2. Find the template by **ID and kind** in that registry's manifest, then load and validate its descriptor and allowed source files from the same checked-out commit. Do **not** resolve the template through cross-registry field merging; the selected provider is the source of truth.
3. Copy template files into a private temporary directory. Parse the generated `nosey.registry.json` and replace `id`, `name`, and `description` using structured JSON serialization, not regex substitutions. All authored generated files remain editable JSON/Markdown.
4. If `workspace` is chosen, run the ordinary Nosey scanner, display detected technologies, and let the user select which technology definitions and compatible skill/recommendation-mapping **references** to seed. Preserve attribution and validate copied cross-references. Do not download or vend external skill repositories as a side effect. Local skill folders are copied only if explicitly selected and their licenses/paths permit copying; otherwise retain a Git source reference.
5. Write `nosey.scaffold.json` with `{schemaVersion:1, templateId, sourceRegistryId, sourceRevision, createdBy}` for traceable provenance, conforming to `schemas/scaffold.schema.json`; `sourceRevision` is the validated source snapshot commit SHA for a Git provider and null for a local provider. `createdBy` identifies the Nosey version that generated the registry. This is metadata, not an input to registry merging. Validate the entire generated registry (all JSON schemas, cross-references, local path bounds), then atomically move it into the requested path. Optionally `git init` and `registry add` only after validation and successful move.
6. Show the created directory and chosen template source. Do not publish or upload anything automatically. The result is a normal local directory; users may commit/push it to Git themselves.

**Network and cache behavior:** Templates travel with the same Git snapshots and cache TTL as registry data; the selected descriptor and all its files must be read from one consistent SHA. When offline, generation is supported only if that provider has a previously validated cached snapshot containing the template (or it is an accessible local-directory provider). If no such snapshot exists, display a clear error and offer `nosey registry update` or `--template-registry` for a local source. There is **no silently embedded fallback scaffold**. A template schema/generator version unsupported by the running Nosey version is rejected with a compatibility error, never partially generated.

**Template security:** Enforce output filename/path allowlisting, total source file size and count limits, no traversal or symlink escape, no implicit overwrite, no executable hooks, and no sensitive environment-variable interpolation. Source registry ID/URL and resolved SHA are shown before creation. Template descriptions and README contents are untrusted data and must be escaped when displayed in the TUI.

**Example descriptor** (`templates/registry/empty/nosey.template.json`):

```json
{
  "schemaVersion": 1,
  "id": "empty",
  "kind": "registry",
  "generator": "registry-v1",
  "files": [
    { "source": "files/nosey.registry.json", "destination": "nosey.registry.json" },
    { "source": "files/technologies.json", "destination": "technologies.json" },
    { "source": "files/skills.json", "destination": "skills.json" },
    { "source": "files/mappings.json", "destination": "mappings.json" },
    { "source": "files/repositories.json", "destination": "repositories.json" },
    { "source": "files/README.md", "destination": "README.md" }
  ]
}
```

**Template versioning:** `schemaVersion` describes the template descriptor format; `generator` describes which built-in Nosey transformation algorithm should process it. New template content can ship via Git without a Nosey release as long as the descriptor/generator versions remain compatible. Higher versions require explicit Nosey support.

### Technology detection — versioned `technologies.json` (normative v1.6)

**Authoritative data:** Technology Registry v2 is supplied as an externally authored, verified dataset (`reference/technology-registry-v2/technologies-894-v2.json` in this **specification package**). The package includes the source document unchanged and its original Draft 2020-12 schema as `schemas/technologies.v2.schema.json`. The referenced registry contains **894 technology identities, 3,637 rules, 19 `metadataOnly` identities**, and **seven rule kinds**. This distribution archive is not an instruction to bake the default registry or scaffolding templates into the Nosey executable: at runtime Nosey fetches the official Git registry as before. The v1 schema (`schemas/technologies.schema.json`) remains available for legacy registry documents. **No modification or re-audit of the supplied verified rule definitions is in scope for v1.7.1.**

**Version dispatch:** The registry *manifest* remains `schemaVersion: 1` and retains `files.technologies`; the technology document referenced by that path independently declares `schemaVersion: 1` or `2`. Nosey selects the corresponding versioned decoder, then normalizes both to the same internal technology/rule model without rewriting the source. A future unknown technology document version produces an explicit unsupported-version diagnostic; a previously valid cached snapshot is preserved. Rule IDs stay stable within technology identities across overlays. Custom registries and external registry-init templates may independently publish either document version. The generated default `empty` and `workspace` templates in this specification use technology version 2.

**Exact identifiers:** IDs from v2 are preserved verbatim, including `@react-three/fiber` (not `react-three-fiber`). All technology-reference fields in `skills[].technologies`, `mappings[].technologyId`, and `when.allTechnologies`/`anyTechnologies`/`noneTechnologies` accept scoped IDs. Technology identifiers are matched **exactly**, case sensitively; no implicit aliases or migration of skill mappings are performed. Custom authors must update any old aliases themselves or declare explicit migration rules in a future protocol.

```json
{
  "schemaVersion": 2,
  "technologies": [
    {"id":"nextjs","name":"Next.js","category":"framework","rules":[
      {"id":"npm-next","kind":"dependency","ecosystem":"npm","name":"next"},
      {"id":"next-config","kind":"file","glob":"**/next.config.*"}
    ]},
    {"id":"adobe","name":"Adobe","category":"cloud","rules":[],"metadataOnly":true}
  ]
}
```

**Supported rule vocabulary:** `file` (glob or path regex), `dependency` (parsed ecosystem identifier), `content` (bounded content match), `extension`, `dotenv` (names only), `detector` (trusted externally registered adapter), and v2 `mcp_package` (passive MCP-config package reference). The v2 dependency ecosystems are `npm`, `python`, `cargo`, `go`, `ruby`, `composer`, `docker`, `terraform`, `terraform.resource`, `githubAction`, `deno`, and `hex`. Adding a schema enum value does **not** imply an associated runtime parser is installed. Every ecosystem must have an explicit observation provider; unmatched provider availability is a coverage diagnostic, not a negative rule match. Use consistent project-relative evidence paths in monorepos and preserve each matched rule's original ID and kind.

**`metadataOnly`:** A technology marked `metadataOnly: true` remains a resolvable category/grouping identity in the registry and in mapping/search reference resolution. It does **not** produce a positive detected-technology result solely by being present in the catalog. A higher-priority registry may supply an enabled detection rule and change this effective state explicitly. Do not impose legacy 'every active identity needs a rule' requirements on metadata-only identities.

**Matching semantics:** Each supported, enabled detection rule is evaluated independently; a technology is detected when any evaluated rule matches (logical OR), and each positive result retains rule-specific evidence. The pre-existing source-level override and post-merge contract still applies to custom authoring. This revision does not re-audit the supplied rules, invent additional rules, change regexes, or add new rule-validation fixtures.

**Rule scope and evaluation (normative; distinct from rule validity):** Once effective registries are merged, classify every active rule in each selected workspace/project as `required`, `excluded-by-mode`, or `not-applicable`. A `required` rule is enabled under the scan configuration and applies to a selected project; its observation provider or explicitly enabled, trusted adapter is expected to run. `excluded-by-mode` covers a deliberate opt-out (for example MCP inspection off by default or an unactivated adapter); such exclusion is not negative evidence. `not-applicable` requires a positive basis for excluding a rule from a selected project scope, and must never mean merely that its candidate file/package was not found. Disabled/tombstoned rules are omitted before scope classification. An available required rule that finishes without a hit is `not-matched`; unavailable required providers/adapters, errors, and relevant budget truncation are `not-evaluated` with a reason. Excluded-by-mode rules are also `not-evaluated` with reason `excluded-by-mode`. The stable reason vocabulary is `excluded-by-mode | unavailable-provider | missing-adapter | execution-error | budget-exceeded | unsupported-rule | other`, and a provider ID or adapter ID is retained when relevant. Every `not-evaluated` result has a reason; report it without reading secret values.

**Execution completeness (normative):** The JSON `complete` property reports *execution completeness*, not total catalog detection coverage. `complete=true` exactly when all `required` rules were evaluated (matched or not-matched) without relevant evidence truncation. An intentionally disabled MCP rule or inactive detector does not by itself fail a default scan. An enabled MCP rule whose parser is missing, an enabled detector whose adapter is missing, a provider error, and required-rule budget truncation make `complete=false`. `scan --strict` exits `4` exactly when `complete=false`; a non-strict incomplete scan may warn and exit `0`. The supplied 3,637 authored rules are not a universal denominator for a scan: reporting counts must use the effective rules and selected workspace/project scopes.

**Technology inference and negative evidence (normative):** A technology is `detected` if any rule is `matched` and yields at least one nonempty, sanitized evidence item. A technology with at least one applicable active rule is `absent` only if *every* such rule was actually evaluated and produced `not-matched`. Without a positive match, a technology is `unknown` if any relevant rule was not evaluated or if it is `metadataOnly` without rule-backed evidence. A positive hit wins even if another rule is not evaluated. The same rule evaluation model feeds `allTechnologies`, `anyTechnologies`, and `noneTechnologies` mapping conditions; unknown must never be treated as absent. Intentional exclusion can therefore yield `complete=true` and a set of `unknown` technologies simultaneously.

**Coverage reporting (normative):** `scan --json` MUST contain `coverage` with `requiredRuleCount`, `evaluatedRequiredRuleCount`, `unevaluatedRequiredRuleCount`, `excludedRuleCount`, `notApplicableRuleCount`, `unknownTechnologyCount`, `unknownTechnologyIds`, and a `gaps[]` array carrying the affected `technologyId`, `ruleId`, `kind`, `reason`, and optional `provider`. `complete` equals `(unevaluatedRequiredRuleCount == 0)` and `requiredRuleCount == evaluatedRequiredRuleCount + unevaluatedRequiredRuleCount`. A rule excluded by mode remains a coverage gap even when execution is complete. `capabilities` separately describes supported providers, rule kinds, ecosystem parsers and MCP state. The UI must distinguish "none detected" from "coverage uncertain" and surface gaps without blocking browsing or user-confirmed installation. These counts describe rule evaluations across selected project scopes, so rule IDs may legitimately repeat for multiple projects.

**MCP opt-in:** `mcp_package` inspects only recognized local MCP config files and never invokes an MCP server, package manager, or configured command. The upstream-authored reference catalog documentation identifies `.mcp.json`, `.cursor/mcp.json`, `.vscode/mcp.json`, `.claude/settings.json`, `.gemini/settings.json`, `.codex/config.toml`, `opencode.json`, `opencode.jsonc`, and `.continue/config.json` (including eligible monorepo subprojects). The implementation supports JSON/JSONC and the relevant TOML shapes, skips symlinks, limits reads to 65,536 bytes per file, redacts sensitive values, and matches **package identifier tokens**, not arbitrary command substrings. Package matching uses exact identifiers or an explicit `@version` suffix. `detection.enableMcpPackage` defaults to `false`; `nosey scan --enable-mcp` is an explicit one-shot opt-in. When disabled, MCP rules are marked not evaluated rather than false. The source catalog documents passive MCP matching behavior, but no Python detector reference implementation is shipped in this planning-only package.

**Additional ecosystem providers:** Docker is based on image references, `githubAction` on declared `uses:` action references, `terraform` on provider/module declarations, `terraform.resource` on resource types, `deno` on Deno imports/dependency declarations, and `hex` on package declarations. These providers normalize observed identifiers and pass them to the existing `dependency` matching function; implementation availability is reported explicitly. The supplied 14 `detector` references are symbolic names: no source registry entry may install or run arbitrary detector code. Existing user-level pinning and adapter trust requirements remain mandatory.

**UI and CLI presentation:** Scan's filter/sort controls are unchanged. Technologies with no positive evidence never appear as detections, but may appear in Explore's catalog groupings when they have associated skills. Scan details show matched rule ID/kind/evidence and optional incomplete-coverage diagnostics; a compact "Detector coverage" section is accessible through Scan details and `nosey doctor`. JSON `scan --json` includes mandatory `coverage` and an optional `capabilities` object for available rule kinds/ecosystem parsers, unavailable adapter IDs, `mcpPackageEnabled` and unevaluated counts, retaining `outputVersion: 2`. Do not print dotenv values or raw MCP command strings.

### Skill catalog — `skills.json` v2 (normative)

Skill definitions are independent of detection: Explore can display/install catalog skills without a Scan recommendation. **This planning revision is intentionally breaking:** `skills.json` uses `schemaVersion: 2`, and upstream installer skill names use `source.skillName` rather than the logical Nosey `id` or former `source.skill` field. No migration or compatibility adapter is required before publication.

```json
{
  "schemaVersion": 2,
  "skills": [
    {
      "id": "vercel:web-design-guidelines",
      "name": "Web Design Guidelines",
      "description": "Review web UI quality and accessibility",
      "tags": ["frontend", "review"],
      "technologies": ["nextjs", "typescript"],
      "repositoryId": "vercel:agent-skills",
      "quality": "excellent",
      "score": 93,
      "assessment": {
        "method": "manual-review",
        "assessedAt": "2026-11-01",
        "assessor": "Nosey example curator"
      },
      "source": {
        "kind": "git",
        "uri": "vercel-labs/agent-skills",
        "skillName": "web-design-guidelines"
      },
      "requires": [],
      "conflictsWith": []
    }
  ]
}
```

**Identity:** `id` is Nosey's canonical registry-wide catalog key. `source.skillName` is the normalized upstream `SKILL.md` name installed through the Skills CLI. `repositoryId` identifies the actual source repository, not the registry hosting the JSON. All three identities have different meanings. The source name must not begin with an option prefix or contain path separators and must equal the skill's parsed frontmatter `name` for a local source. For Git sources, reconcile that name against the Skills CLI's advertised names before execution. No ambiguous aliases or silent substitution. Git `ref` is optional and subject to pinned-upstream source-form compatibility. A local path is relative to and contained within its declaring registry checkout.

**Ratings:** Optional `quality` (`excellent`, `good`, `fair`, `poor`) and `score` (0–100, decimals permitted) are independently authored. Missing values remain missing, not zero or algorithmically inferred. Optional `assessment` carries `method` (`manual-review`, `automated`, `third-party`, `other`), required `assessedAt` ISO date, and optional `assessor` and HTTPS `evidenceUrl`. Assessment metadata is editorial and must not be displayed as an external certification. Field-level **registry origin/provenance** is tracked by the merger separately from the authored `assessment` object. An effective assessment with neither score nor quality is an error (an overlay fragment may still contain only assessment).

**Source vs effective validation:** `skills.schema.json` accepts `{id, ...changed fields}` from custom registries to retain field-level overrides. The merger groups entries by stable ID and applies per-field precedence, with `source` and `assessment` as atomic objects and lists such as `tags`, `technologies`, `requires`, `conflictsWith` as atomic arrays. After merging and removing disabled entities, the result **MUST** pass `schemas/skills.effective.schema.json` (requires `id`, `name`, `source`) plus semantic checks. Duplicate IDs in one input document are errors, even if the JSON Schema's array rules alone do not detect them. The merge engine does not execute data-driven registry code.

**Source repository integrity:** A declared Git `repositoryId` must resolve to a declared Git repository whose normalized identity matches the Git skill source; a declared local repository must contain the local skill folder within its declared root. Inconsistencies are errors before installation. A source with no `repositoryId` may use the documented source-derived virtual repository, which has no authored average by default.

**Quarantine:** An optional skill field `quarantine` (`reason`: `repo_gone` or `skill_absent`; `since`, `lastSeenRef`, optional `lastChecked`) marks a skill whose upstream is gone. Unlike `disabled`, the entry stays fully valid (`name` and `source` remain) and referenceable, so mappings and relationship links to it stay valid; restoring a skill means deleting the field. A quarantined skill is never recommended, searched, listed or installable (installing it is a policy error). A skill that `requires` a quarantined skill is unavailable with warning `W_REQUIRES_QUARANTINED`; `conflictsWith` links to a quarantined skill are ignored; quarantined skills are excluded from repository score averages. An overlay that omits `quarantine` cannot clear it. Registries should write `quarantine` only after the generator pinned by their CI understands it, because older clients treat the unknown field as an invalid entity and drop mappings that reference it.

**Catalog relationships:** `requires` and `conflictsWith` contain other canonical skill IDs. Self-links, contradictory require-and-conflict links, unknown or disabled target skills, and dependency cycles in the effective install plan are errors. Conflicts are checked symmetrically at runtime, regardless of whether both sides declare the relationship. These catalog constraints always apply to manual Explore selections.

### Skill repository metadata — `repositories.json` and manifest `repositoryScores` (normative)

`repositories.json` defines repository identity (`id`, `name`, `description`, `kind`, `uri` or `path`, `disabled`) only; `averageScore` and `scoreCount` **must not** appear there. Scores live only in the registry manifest's `repositoryScores` map, keyed by repository ID. Example:

```json
{"repositoryScores":{"vercel:agent-skills":{"averageScore":91,"scoreCount":1},"local:workspace-skills":{"averageScore":86,"scoreCount":2}}}
```

`averageScore` is authored, in the inclusive range 0–100. `scoreCount` is optional, positive integer. No runtime inference, averaging, or fallback from skill scores is permitted. Absent `repositoryScores[id]` yields `averageScore: null` (unrated); `0` is a real score. Entries for unknown declared repository IDs issue a warning and never attach to virtual/inferred repository headings. During source merge, entries merge by repository ID and property with per-field provenance. A higher-priority override to one score does not replace unrelated scores. Authoring-time `nosey registry validate` **warns** when an authored average differs by more than 0.5 from the mean of the **same source registry's** own rated skills for that repository; this never modifies stored ratings and is suppressed if there are no eligible scored skills.

### Recommendation mapping — `mappings.json` v2 (normative)

Mappings only decide **when and why** a catalog skill is recommended, not how it is installed. `schemaVersion: 2` uses explicit `technologyId`, `recommendations`, and nested `skillId`. These names intentionally replace `technology`, `skills`, and recommendation `id`; there is no backward-compatibility requirement.

```json
{
  "schemaVersion": 2,
  "mappings": [
    {
      "id": "nextjs-recommendations",
      "technologyId": "nextjs",
      "recommendations": [
        {
          "skillId": "vercel:web-design-guidelines",
          "priority": "recommended",
          "reason": "Apply web UI review practices to a Next.js project",
          "when": { "allTechnologies": ["typescript"] },
          "requires": [],
          "conflictsWith": []
        }
      ]
    }
  ]
}
```

**Conditions:** An absent or empty `when` object is unconditional. When present, each of `allTechnologies`, `anyTechnologies`, `noneTechnologies` MUST have at least one ID; distinct clauses are combined by AND. There are no nested Boolean trees. The parent `technologyId` is an additional mandatory gate. Recommendation priority defaults to `optional` when omitted; its ordering is `core` > `recommended` > `optional`. An active mapping with `recommendations: []` intentionally emits no recommendations; when used as a higher-priority overlay it clears inherited recommendations, whereas omitting the property preserves them.

**Three-state technology evaluation (important with verified Registry v2 rules):** For each technology, the detector reports `detected` (positive evidence), `absent` (all applicable in-scope enabled rules evaluated and none matched), or `unknown` (no positive evidence and incomplete/unavailable detection; this includes a metadata-only identity with no rules). `unknown` MUST NOT be treated as absence. `allTechnologies` returns false if any item is absent, true if all detected, otherwise unknown. `anyTechnologies` returns true if any detected, false if all absent, otherwise unknown. `noneTechnologies` returns false if any detected, true if all absent, otherwise unknown. AND across parent and clauses returns false if any false, true if all true, otherwise unknown. False entries are omitted; unknown entries are **unevaluated**, shown separately from confirmed Scan recommendations with the blocking unknown technology IDs. They are not selected by default and must not inflate confirmed recommendation counts. Explore remains independent of detection.

**Merge and validation:** Mapping source overlays can contain ID plus changed properties. Top-level mappings merge by `id`; nested `recommendations[]` merge by `skillId`; `when` and relationship arrays replace atomically within a single recommendation overlay. Disabled entities are omitted. Fully materialized mappings MUST have `technologyId` and `recommendations` and pass `schemas/mappings.effective.schema.json`. Validate unique mapping IDs per source and unique nested `skillId` values within each mapping. After effective merging, an unknown mapping anchor or recommendation `skillId` is an error; unknown conditional technology IDs produce an unevaluated diagnostic, not a false result. Missing technology associations in catalog `skills[].technologies` are warnings until the source set is complete.

**Combining active mappings:** For every skill with multiple **passing** recommendation entries, select the highest priority; union mapping-level `requires` and `conflictsWith` with catalog-level sets; concatenate and deduplicate explanations/provenance in registry/mapping order. Each `when` is evaluated independently: unevaluated entries never contribute priority or relationships to confirmed results. An omitted `reason` is replaced at runtime with `Recommended because {technology name} was detected`, backed by the parent mapping's detection evidence. No fabricated score/quality follows from a recommendation.

**Install preflight:** Resolve transitive required skill IDs; offer to select missing prerequisites; reject cycles, self-links, requirements that conflict, and installed/selected conflicts until resolved by explicit user choice. Conflicts are symmetric. Existing `--ignore-conflicts` remains an intentional metadata override, with a warning and confirmation, never a trust bypass. No automatic removal. A skill with an unevaluated recommendation may still be manually selected from Explore, subject to catalog-level requirements and trust checks.

### Registry scoring, reference diagnostics and workspace seeding (normative)

`repositoryScores` merges by repository ID and field as described above, with the last-write rule determined by registry priority and source order; the implicit official registry is first at priority 0. A user registry also at priority 0 therefore wins. A source that overrides `skill.source` or `skill.repositoryId` receives an override marker with the full registry provenance chain in TUI installation confirmation and dry-run JSON. A project-scoped or unpinned override of either property requires an extra explicit approval before installing. Unknown skill references in mapping, `requires`, and `conflictsWith` are entry errors. Unknown technology IDs in skill technology tags, mapping technologyId, and conditional technology references are warnings during source validation; after effective merge, unresolved references remain diagnostics (unresolvable mapping conditions cannot produce recommendations). Catalog-level `requires` / `conflictsWith` are atomic-replaced on source merge; effective requirements union the catalog values with all passing mapping recommendation entries, and cycle detection spans both sources.

`nosey registry validate PATH` uses the official default registry as the lower-priority base before validating PATH, while `--standalone` validates PATH alone and reports missing base references as errors. The official registry itself is never overridden by an untrusted project setting. `nosey registry init --from-scan --yes` seeds **all** detected technology definitions and associated passing recommendation / skill references; repeatable `--include-tech ID` narrows that set. Non-interactive `--from-scan` without `--yes` exits 2. The `registry-v1` generator writes effective technology definitions (`id`, `name`, `category`, active `rules`), v2 mapping entries (`technologyId`, `recommendations[].skillId`), minimal v2 skill references (`id`, `name`, `source` with `skillName`, `repositoryId`), and `nosey.scaffold.json` provenance records; skill bodies, ratings, and third-party source contents are never vendored. The `workspace` template includes dedicated seeded-content instructions; the `empty` template does not.


**Official rating publication gate:** Before the official registry presents scores/quality labels as Nosey-curated judgments, its named maintainer must approve a published rubric, provenance/assessor requirements, initial assessment procedure, reassessment cadence, correction/appeal path and conflict-of-interest disclosures. User-authored registry ratings remain permitted without official approval but must visibly identify the authoring source; no score or label is a detection confidence measure. This governance requirement blocks publication of curated scores, not schema design or implementation of sorting.
