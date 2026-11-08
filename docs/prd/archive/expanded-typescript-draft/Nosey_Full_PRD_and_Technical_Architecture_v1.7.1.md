# Nosey — Full Product Requirements Document & Technical Architecture

**Expanded document status:** Design baseline for engineering review; **not** implementation approval or runtime verification  
**Preserved product specification version:** 1.7.1  
**Expanded document revision:** 1.7.1-architecture-draft.1  
**Prepared:** 2026-11-07  
**Primary evidence:** `nosey-spec/PRD.md`, the 16 source JSON Schemas, the example registry, the original 894-technology reference document and its report, all in the supplied planning package.  
**Authority rule:** Original v1.7.1 normative requirements and checked-in schemas govern; additions marked **Proposed design** must be approved before implementation. No runtime, fixture, installed agent or network compatibility has been tested as part of this document.

## 0. Document governance and decision summary

### 0.1 Purpose and intended readers

This document is a consolidated engineering-ready **planning** specification for the terminal-first Nosey developer tool. It preserves the source PRD's TUI/CLI functionality, registry and schema contracts, detection semantics, installer safeguards and 72 original acceptance criteria. It adds implementable module boundaries, data flow and trust boundaries, data and API interface specifications, system quality requirements, dependency decisions, critical sequences, phase-level verification, a risk register and release decision gates. Intended readers: product owner, engineering lead, CLI/TUI engineers, test engineers and maintainers of the official external registry.

### 0.2 Product decision in one paragraph

Nosey is a **local-first TypeScript CLI/TUI**, not a hosted SaaS application. Its core responsibility is detection, explanation, registry composition, recommendations and confirmation-based installation orchestration. A Git-hosted official registry and optional user/workspace registries provide **data**, not executable plugins. The separate pinned Vercel `skills` CLI performs actual skill installations. Nosey maintains no account service and should not own skill update/removal in MVP. The default implementation is a **single Node.js process with modular domain services**, a file-backed validated cache, and sandbox-free but tightly controlled subprocess adapters. This is a recommended design, not a claim of existing code.

### 0.3 Decision status and authority

| Decision | Status | Basis / constraint |
|---|---|---|
| Four-tab one-column TUI and scriptable CLI | Source normative | Sections 3–5; original AC-01–AC-72 |
| Technology document v1/v2, skill/mapping v2, output v2 | Source normative | Checked-in JSON Schemas and §6/§5.1 |
| 894 identities / 3,637 rules; source dataset unchanged | Source-provided asset | Reference technology v2 document; do not make it a built-in runtime registry |
| Official remote Git registry, no embedded scaffold | Source normative | Registry init §6; official URL still unresolved |
| Local modular monolith and filesystem cache | **Proposed design** | Reduces deploy and transaction complexity; no server requirement |
| Node.js 22.20+ / TypeScript; Ink candidate | Source recommendation | Renderer selection remains open |
| Pinned upstream `skills@1.7.6` baseline | Unverified integration proposal | Tagged-source interpretation is not execution proof |
| No product telemetry; upstream inherits env by default | Source normative | §10; disabled mode sets telemetry opt-out variables |
| No global install or automatic remediation | Source normative | Non-goals and installation preflight |

### 0.4 Confirmed, proposed and unknown

**Confirmed in the package:** planning documents exist; schemas and sample registry are present; the source reference reports 894 technology IDs, 3,637 authored rules and 19 metadata-only IDs; 72 product acceptance criteria are recorded. **Proposed:** operational architecture, processes, filesystem layout, interface layering and detailed validation/test approach in §§12.1–12.18 and §§18–22. **Unknown/blocking:** real v1.7.6 install/list JSON result behavior, grouped `--skill` argv grammar in disposable workspaces, agent identity mapping, cache/lockfile attribution, official registry URL/governance, supported platform matrix, hardware performance benchmark and final scoring study. A checked-in schema is not an executed contract test.

### 0.5 Scope boundary

**In MVP:** Scan, Explore, Manage inventory, Settings, data-only Git/local registry overlays, external registry-init templates, v1/v2 technology decoding, all seven detection-rule kinds subject to documented opt-in/capability semantics, three-state recommendations, search and score facets, project-scope installation planning and verification, offline cache and trust review. **Outside MVP:** account/auth backend, hosted registry API, server database, universal detector execution from registry JSON, global skills management, autonomous skill installation, updater/remover, and complete upstream search mirroring. Keep all required MVP functionality from §15; the architecture must not silently defer it.

### 0.6 Requirement language and change control

The source PRD's **MUST**, **normative** and AC requirements are authoritative. Newly introduced implementation choices are prefixed **Proposed design** or recorded as an ADR. If an implementation choice conflicts with the source schemas or normative text, the source prevails until an explicit product change is approved, versioned and traced through affected schemas, fixtures, CLI output snapshots and acceptance tests. Scope is a **project directory**, distinct from a user's global skill inventory. Missing scores and unknown states must remain distinguishable from numerical zero and verified absence.

---

## 1. Executive summary

Nosey is a terminal-first CLI/TUI application for individual developers who work with AI coding agents. It inspects the current workspace, detects technologies using externally defined rules, recommends relevant agent skills using a separate externally defined mapping, and installs only user-selected skills through Vercel's `skills` CLI. Users can also browse a merged registry manually, install skills that were not recommended, and initialize from externally hosted templates and create Git-backed or local custom registries containing detection rules, skill mappings, references to external skill repositories, local `SKILL.md` skills, skill quality/score metadata, and authored repository-level average scores in the manifest. The registry manifest uses schemaVersion 1, verified technology documents use version 2, and the newly designed skill/mapping documents use version 2. These document version numbers are independent.

Running `nosey` launches a single-column, keyboard-driven TUI that immediately scans the current workspace. The persistent header displays the logo and `Scan · Explore · Manage · Settings` menu; Tab cycles menu sections. A persistent footer lists context-sensitive keybindings. Shell-friendly subcommands expose the same core operations for scripts and CI.

Upstream already provides `find`, `check`, `update` and experimental sync. Nosey adds detection, context-specific mapping, layered registries and quality/score metadata rather than replacing those upstream commands.

**Core product principle:** Nosey owns *discovery, recommendation, curation, and orchestration*; Vercel's Skills CLI owns *skill installation*.

## 2. Goals, audience, success criteria

### Audience

Primary: individual developers using coding agents such as Claude Code, Codex, Cursor, or other agents supported by Vercel's Skills CLI. A user may have several agent integrations in one workspace.

### Goals

1. Produce relevant, explainable recommendations with minimal setup.
2. Allow deterministic, editable registries independent of the Nosey release cycle.
3. Support private, team, personal, and workspace-local rule/skill catalogs.
4. Preserve user control: never install a skill without selecting it and confirming execution.
5. Expose both a fast TUI and scriptable, structured-output CLI.

### Product metrics (proposed launch targets)

- At least 95% correct technology IDs in a future representative workspace evaluation (all seven rule kinds, MCP opt-in and monorepos); this is an engine-integration goal, not a re-audit of the supplied authored rule catalog.
- 100% of confirmed recommendations expose matching technology/rule evidence and a human-readable reason, assessed in the release verification phase (without product telemetry).
- 100% of claimed successful project installs must have per-skill upstream results and corroborating installed-state evidence; ambiguous inventories must remain `unknown`.
- Cached scans target p95 < 2 seconds on an explicitly specified CI runner (CPU/RAM/OS), for a fixed fixture tree size and file count; cold-fetch and warm-cache results reported separately. Final runner configuration is a release gate.
- No silent global installation and no unconfirmed installation in TUI sessions.
- Proposed recommendation-quality launch targets: precision@3 at least 80% on an independently labeled developer-workspace corpus; at least one actionable recommendation in 80% of applicable workspaces; and at least 90% unaided task completion in structured usability sessions. These thresholds are provisional until baselines and study protocols are established. No MVP product telemetry is required.

### Non-goals for MVP

- Global-scope skill installation.
- Writing or editing `SKILL.md` contents through Nosey.
- A hosted registry service, account system, marketplace, or ranking backend.
- General-purpose arbitrary code execution from downloaded detection JSON.
- Full skill update/removal lifecycle management or automatic repair.
- Cross-registry remote full-text search beyond the configured indexed registry catalogs.

## 3. User stories and workflows

**US-01 / auto scan:** As a developer, running `nosey` scans the current workspace and displays technology matches and related skills. Each technology can show the matching rule ID, kind, project scope, and evidence path(s) without exposing sensitive values. I can independently sort and filter the detected-technologies list and recommended-skills list without rescanning or losing skill selections.

**US-02 / selective install:** As a developer, I toggle multiple skills with Space, preview the installation commands, and confirm or cancel. The selected skills are installed only in the current project and for chosen supported agents.

**US-03 / manual discovery:** As a developer, I can move to Explore, type a live query, filter and sort by technology, skill repository, registry, quality, numeric score, repository average, and installation status, and choose skills even if the workspace did not trigger their recommendation. Results are grouped by technology and sub-grouped by source skill repository.

**US-04 / custom registry:** As a developer, I can initialize a registry from a template stored in the official external Nosey registry (or an explicitly selected custom registry), then add a Git URL or local directory that contains a Nosey manifest, JSON files, referenced external skills and/or local skill directories. I can validate the registry and set priority.

**US-05 / configuration:** As a developer, I can use Settings to add/edit/disable/remove registry references, inspect and approve/revoke trust fingerprints for workspace registry proposals, change refresh interval, select installation agent(s), and choose symlink/copy or execute/print preferences.

**US-06 / scripting:** As a developer, I can run `nosey scan --json` and `nosey install <skill-id> --yes` in a terminal or script without starting the TUI.

**US-07 / offline:** As a developer, I can keep scanning and searching cached catalogs without connectivity; missing first-time registries must produce an actionable warning.

**US-08 / quality inspection:** As a developer, I can see a skill’s curator-provided quality label and score (when present), inspect its repository’s average score, sort results using that information, and distinguish missing ratings from zero scores.

### Happy-path sequence

1. Resolve the workspace root and effective global/project settings.
2. Load valid cached registry snapshots; fetch due registries according to TTL when possible.
3. Validate, merge, and index registry entities, including optional repository definitions and quality/score metadata; retain provenance for every merged field.
4. Evaluate detection rules against the workspace with bounded file and manifest reads.
5. Resolve mapping conditions; rank and deduplicate recommendations; load authored repository averages (never compute at runtime); query project-installed skills through the versioned reader.
6. Render Scan with all skills unchecked and explanatory evidence, plus independent filter/sort state for detected technologies and recommended skills.
7. User toggles skills; Nosey resolves prerequisite requirements and conflict blockers.
8. User opens install confirmation showing exact source, skill name, target agents, method and command.
9. User executes or copies commands; `--dry-run` previews without changing files.
10. Show per-skill success/failure, then return to Scan with refreshed installed status.

## 4. TUI layout and focus model

**Constraint:** One column only. A persistent header and footer surround one scrollable content area. No permanent sidebars, dual-pane layouts, or split view. Dialogs may temporarily overlay content, but preserve the header/footer unless terminal width makes that impractical.

```text
┌──────────────────────────────────────────────────────────────────────┐
│ NOSEY                          ~/code/shop · 2 technologies detected │
│ [ Scan ]    Explore    Manage    Settings                            │
├──────────────────────────────────────────────────────────────────────┤
│ SCAN                                              F Filter · S Sort  │
│                                                                      │
│ Detected technologies (2/2)                  Sort: Name A–Z           │
│  ✓ Next.js    package.json → dependencies.next                       │
│  ✓ TypeScript tsconfig.json                                          │
│                                                                      │
│ Recommended skills (2/2)                              0 selected   │
│  > [ ] Web Design Guidelines  Recommended · Excellent 93            │
│    [ ] Workspace Style        Optional · Good 84                     │
│                                                                      │
│  Why? Next.js was detected; mapping matched current technologies.    │
│                                                                      │
├──────────────────────────────────────────────────────────────────────┤
│ ↑↓ Move   Space Select   F Filter   S Sort   I Install   Tab ↹       │
└──────────────────────────────────────────────────────────────────────┘
```

### Focus and navigation contract

- `Tab`: switch immediately to next top-level section, cycling Scan → Explore → Manage → Settings → Scan. This is not a browser-like focus chain.
- `Shift+Tab`: switch to previous section.
- `↑`/`↓`: move the active row or control in the current content area; scrolling follows cursor.
- In Explore, `←` collapses the focused technology or repository group, and `→` expands it. `Enter` on a group heading toggles its expansion. Collapsing does not clear any selected skills. In Scan, `←`/`→` can move focus between the Detected technologies and Recommended skills sections when focus is not in a modal or text input.
- `Enter`: open the active row's details or commit an edit; in Scan/Explore it does **not** toggle selection (Space does).
- `Space`: toggle an active skill's checkbox; in Settings, toggle a checkbox/switch.
- `/`: focus the view's search input (Scan or Explore). While typing, keyboard characters type text; `Esc` exits input and preserves the query.
- In both Scan and Explore, `f` opens filter controls and `s` opens sorting controls. In Scan, the dialog targets the focused section (Detected technologies or Recommended skills); its target can be switched within the dialog. These actions are disabled while typing and are supported from keyboard-only navigation.
- An active confirmation or other modal traps keyboard input; `Tab`/`Shift+Tab` navigate dialog controls rather than changing top-level sections. `Esc` cancels the modal.
- `?`: open keybinding help. `q` quits from non-input, non-modal contexts.
- Preserve in-memory view state (queries, selections, cursor) while changing tabs in the same session.

### Screen requirements

**Scan** (default): From skills list press `←` to return focus to detected technologies; from detected technologies press `→` to focus recommended skills. Auto-run detection on launch; technology list with evidence and rule IDs; recommended skill rows with name, priority, provenance, installation status, optional quality/score/repository average and reason; `r` rescan, `Space` select, `a` select/deselect all **visible eligible** skills, `i` review install, `/` focus live search, `f` filter, and `s` sort. Initially, nothing is selected. Detected technologies and recommended skills are two separately focusable sections with independent sort and filter state. A 'No technologies found' state still links to Explore. Filtering and sorting are mandatory MVP functionality in Scan, not limited to Explore.

**Scan filters and sorting:** The two focusable Scan sections expose independently stored filters and sort orders. All normative facet combinations, ranking, null handling, and tie breakers are defined in **Canonical filtering and sorting contract** below. Filtering never re-runs detection and never changes which skills were recommended.

**Scan selection, counts, and state:** Filtered-out skills remain selected; the count distinguishes `N selected total` from `M selected visible` when they differ. Install confirmation includes all selected skills, including hidden selections, and explicitly notes that some are hidden by active filters. `a` acts on **visible** eligible skills only, while `i` includes the full selection. Clearing filters restores hidden rows and does not reset checkboxes. `r` rescan updates matches/recommendations but retains filter/sort preferences during the session; selections for skills no longer available are removed with a visible notice. Scan and Explore have separate filter/sort state, kept when switching tabs. Empty states differentiate no detections, no applicable recommendations, and all results hidden by filters, with an accessible `Clear filters` action. Filter/sort dialogs show a preview count, apply/cancel semantics and no surprise modification of the underlying scan. Recommended skills remain selectable even if a different section is focused by moving the cursor to that section.

**Explore:** Live (debounced) search of the *merged registry's* skill names, descriptions, IDs, tags, associated technologies, and source repository names. The result tree is always **Technology → Repository → Skill**. Technology headings show visible skill counts; repository headings show the repository name, visible count, and average score when available. Use `↑↓` to navigate headings and skills, `←→` or `Enter` to collapse/expand groups, `Space` to select skills, `f` to edit filters, `s` to choose sort order, and `i` to review installation. All results use the effective merged registry and omit disabled/invalid entities. For arbitrary upstream searching beyond configured registries, `nosey search --upstream` may hand off to `skills find`, but Nosey must never scrape its interactive output as a stable API.

**Explore filters and sorting:** Filtering and sorting use the **Canonical filtering and sorting contract** below, preserving the technology → repository → skill hierarchy.

**Grouping and deduplication:** A skill is associated with the union of its `skills[].technologies` and mapping `technologyId` references, so it may appear in multiple technology groups. It appears once in each applicable technology/repository subgroup, with a single canonical selection state keyed by `skill.id`. The selected count and installation plan count **unique** skill IDs, not visible rows. Skills with no technology association appear under `Other / Uncategorized`. A skill belongs to exactly one effective repository: explicit `repositoryId`, or a source-derived repository identity where omitted. If no catalog entry exists, a virtual repository is generated from the Git source or originating local Nosey registry. The source *registry* remains a filter and provenance field, and is not confused with the source skill *repository*.

**Explore empty states:** No query matches; filters exclude all results; no configured registries; repository has no scored skills. These states must be distinguishable and allow clearing filters or navigating to Settings.

**Manage:** Read-only installed skill inventory for project scope with agent/source/status where the underlying CLI exposes them, recent operation results from current session, and an `r` refresh shortcut. Installation updates/removal actions are explicitly out of MVP. Never conflate global inventory with project inventory.

**Settings:** Two top-level content groups: `Registries` and `Installation`. Registries allow add Git URL/local directory, enable/disable, remove, priority, refresh TTL, manual refresh, cache status. Installation allows target agent IDs, installation method (symlink/copy), execute-or-print default and verbose mode. `Ctrl+S` saves, `Esc` cancels unsaved editing, and changed values are validated inline. Registry edits do not modify the source Git repository itself.

### Explore results layout — technology → repository → skills

```text
┌────────────────────────────────────────────────────────────────────────┐
│ NOSEY                                 ~/code/shop                     │
│ Scan   [ Explore ]   Manage   Settings                                │
├────────────────────────────────────────────────────────────────────────┤
│ Search  /                                         Sort: Score ↓       │
│ Filters: Quality = good, excellent  ·  Min score = 80                  │
│                                                                        │
│ ▾ Next.js                                            3 unique skills │
│   ▾ vercel-labs/agent-skills                      Avg 91/100          │
│      [ ] Web Design Guidelines  Excellent 93/100                      │
│   ▾ Workspace Skill Collection                   Avg 86/100          │
│      [ ] Frontend Checklist      Good      88/100                      │
│      [ ] Workspace Style         Good      84/100                      │
│                                                                        │
│ ▾ TypeScript                                        2 visible skills │
│   ▾ vercel-labs/agent-skills                      Avg 91/100          │
│      [ ] Web Design Guidelines  Excellent 93/100                      │
│   ▾ Workspace Skill Collection                   Avg 86/100          │
│      [ ] Frontend Checklist      Good      88/100                      │
├────────────────────────────────────────────────────────────────────────┤
│ / Search   F Filters   S Sort   ↑↓ Move   Space Select   I Install    │
└────────────────────────────────────────────────────────────────────────┘
```

This is an illustrative terminal state, not a claim that Nosey is implemented. Repeated rows (for example Web Design Guidelines) reference the same selected skill ID. Group counts represent **visible rows** unless explicitly labeled **unique skills**; the install-selection counter always reports distinct IDs. Repository details display the authored `averageScore` and optional `scoreCount`; absent averages display `Unrated` rather than being calculated.

### Canonical filtering and sorting contract (normative for Scan, Explore, and CLI)

**Common behavior:** Search is case-insensitive literal substring matching in MVP. Distinct facets AND together, multiple selected values within one facet OR together. Numeric thresholds exclude missing ratings (a `0` rating is real). `unassessed` matches only absent quality and `--score-state unscored` matches only absent numeric score; a repository without an authored `repositoryScores` entry has `averageScore: null`. Filter → sort is the operation order. Filters never modify underlying detection, recommendations, selection or installed state. `unknown` installed state is eligible for selection; installed and blocked skills are not. `a` toggles only visible eligible skill IDs. Hidden selected IDs persist and appear in install confirmation. TUI and CLI use the same comparator library.

| Scope | Filter facets | Sort choices and default |
|---|---|---|
| Scan / detected technologies | Technology name/ID and case-insensitive query over matched rule IDs and safe evidence paths, explicit technology ID, **matched** rule kind | `name-asc` (default), `name-desc`, `evidence-count-desc` (distinct evidence paths), `matched-rules-desc` (distinct rule IDs) |
| Scan / recommended skills | Name/ID, description, tags, reasons, source repository text; technology, source registry, repository ID, mapping priority, status (`installed`, `missing`, `unknown`), quality/unassessed plus scored/unscored, min score, min authored repository average | `priority-desc` (default), `score-desc`, `quality-desc`, `repository-average-desc`, `name-asc`; flat skill list |
| Explore / all skills | Search across name/ID/description/tags/technology/repository; source registry, technology, repository, installation status, quality/unassessed plus scored/unscored, min score, min authored repository average | `relevance` (default), `score-desc`, `quality-desc`, `repository-average-desc`, `name-asc`; **always** retains technology → repository → skill tree |

**Text relevance (with query):** Rank exact ID or name, then name prefix, name substring, tags, description, technologies, and repository name; ties fall back to passing mapping priority (`core` > `recommended` > `optional` > unmapped), numeric score descending null-last, normalized name, then stable skill ID. With no query, retain default priority ordering. Quality order: `excellent` > `good` > `fair` > `poor` > unassessed. Numeric score and authored repository average compare descending with missing last; all ties use normalized name and stable ID. Technology headings order detected-workspace technologies before other alphabetical technologies, with `Other / Uncategorized` last; repository headings sort alphabetical by name except for `repository-average-desc`, which sorts authored average descending then name. Within repository groups sort skills by chosen comparator (`repository-average-desc` uses skill score as secondary comparator). Group collapse preserves selected IDs; group counts represent visible rows, selected counts distinct IDs.

**Focus independence:** Scan technology-list facets never implicitly hide recommended skills; recommendation facet `technology`/CLI `--rec-technology` explicitly narrows the recommendations only. CLI `--technology` narrows the detected list. `--query-target` chooses technologies, skills, or both. Views preserve per-section sort/filter state through tab changes and rescans. Queries do not change registry content. A filter yielding no rows produces a `Clear filters` affordance, not an empty detection result.

### Dynamic footer keybinding specification

| Context | Visible footer bindings |
|---|---|
| Scan (technology focused) | `↑↓ Move` · `F Filters` · `S Sort` · `/ Search` · `R Rescan` · `→ Skills` |
| Scan (skills focused) | `↑↓ Move` · `Space Select` · `F Filters` · `S Sort` · `I Install` · `← Tech` |
| Scan (filter/sort modal) | `↑↓ Navigate` · `Space Toggle` · `Enter Apply` · `Esc Cancel` |
| Explore | `/ Search` · `F Filters` · `S Sort` · `↑↓ Move` · `Space Select` · `I Install` |
| Explore (group focused) | `← Collapse` · `→ Expand` · `Enter Toggle` · `F Filters` · `S Sort` |
| Explore (filter/sort modal) | `↑↓ Navigate` · `Space Toggle` · `Enter Apply` · `Esc Cancel` |
| Manage | `↑↓ Move` · `Enter Details` · `R Refresh` · `Tab Sections` · `? Help` |
| Settings (view) | `↑↓ Move` · `N New registry` · `A Add registry` (in Registries) · `Enter Edit/Review trust` · `Tab Sections` |
| Settings → Registries → Trust approval | `↑↓ Review source/fingerprint` · `Enter Approve/Revoke` · `Esc Cancel` |
| Settings (editing) | `Enter Apply` · `Ctrl+S Save` · `Esc Cancel` |
| Settings → Registries → New registry wizard | `↑↓ Move` · `Enter Next` · `Space Select` · `Esc Back/Cancel` |
| Search input | `Type Search` · `Enter Results` · `Esc Done` · `Tab Switch section` |
| Install confirmation | `↑↓ Move` · `Enter Confirm` · `C Copy` · `D Dry-run` · `V Verbose` · `Esc Cancel` |
| Install execution | `Esc View log` · `Ctrl+C Cancel` (safe process termination; mark incomplete items) |
| Error dialog | `Enter Retry` (where retryable) · `C Copy details` · `Esc Close` |

Footer is reserved to two terminal lines maximum. Show at most six contextually relevant bindings, prioritizing safety-critical actions. A `?` overlay lists the entire keymap. Footer shortcuts must reflect which actions are currently enabled, such as disabling `I Install` when nothing is selected.

### Terminal and accessibility behavior

- Target full layout from 80×24. At 60–79 columns, shorten labels and truncate long paths; under 60 columns show a compact warning and still allow quitting.
- Use text labels and icons, not color alone, for success/warnings/selection. Support NO_COLOR, ordinary ANSI terminals, UTF-8, and a monochrome fallback.
- Handle resize, suspension, SIGINT, and child process stderr without leaving terminal in raw mode.
- Never put secret tokens, untrusted path content with control sequences, or unsafe ANSI into the screen or logs.

## 5. CLI contract

All commands are **Nosey proposed CLI syntax**, distinct from the existing `skills` CLI. Commands run relative to the current directory unless `--cwd` is passed.

```bash
nosey                          # Start TUI and scan workspace
nosey ui                       # Explicit TUI
nosey scan [--json] [--strict] [--enable-mcp] [--refresh] [--cwd PATH] [--query TEXT] [--technology ID] [--rec-technology ID] [--query-target technologies|skills|both] [--rule-kind KIND] [--registry ID] [--repository ID] [--priority core|recommended|optional] [--status any|installed|missing|unknown] [--quality excellent|good|fair|poor|unassessed] [--score-state any|scored|unscored] [--min-score 0..100] [--min-repo-average 0..100] [--sort-technologies name-asc|name-desc|evidence-count-desc|matched-rules-desc] [--sort-skills priority-desc|score-desc|quality-desc|repository-average-desc|name-asc]
nosey search QUERY [--registry ID] [--technology ID] [--repository ID] [--status any|installed|missing|unknown] [--quality excellent|good|fair|poor|unassessed] [--score-state any|scored|unscored] [--min-score 0..100] [--min-repo-average 0..100] [--sort relevance|score-desc|quality-desc|repository-average-desc|name-asc] [--json]
nosey search --upstream QUERY   # Delegate to upstream skills find (optional, passthrough)
nosey install SKILL_ID... [--agent ID] [--dry-run|--print] [--copy] [--verbose] [--ignore-conflicts] [--yes]
nosey install --source URI --skill SLUG [--agent ID] [--dry-run|--print] [--yes]
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

**Source-selection contract:** `registry add` and `registry remove` write only user-level registry settings, never `.nosey/config.json`. A mandatory source is a provider explicitly required for an action (such as `--template-registry ID`, `registry update ID`, or direct `install --source URI`); unavailable mandatory sources fail with exit 3, while malformed, disabled, or untrusted mandatory sources fail with exit 2. `scan --registry ID` and `search --registry ID` are result filters, not implicit fetch/trust authorization; unknown filter IDs return exit 2. Untrusted auto-discovered workspace registries are skipped with diagnostics without blocking unrelated scans. Direct installs outside the catalog must be labeled `uncatalogued` and cannot inherit official ratings or recommendation provenance.

**Execution rules:** `install` without `--yes` requires an interactive confirmation; when stdin is not a TTY, return a validation/usage error rather than silently executing. `--yes` means 'Nosey confirmation acknowledged' and is forwarded to `skills -y` only after Nosey validates sources, agents, scope and selection. `--dry-run` and `--print` never invoke the installer; `--dry-run` presents a structured plan and `--print` emits shell-escaped executable commands. `--copy` requests upstream's copy installation method; it does not mean 'copy command to clipboard'. An interactive TUI offers a separate 'Copy commands' action. `--json` outputs parseable stable JSON with diagnostics on stderr. `nosey scan --json` includes `coverage` for required/excluded rule evaluation and optionally includes `capabilities` for runtime support and MCP opt-in; it returns `detectedTechnologies` and `recommendedSkills` for the filtered/sorted view, plus `unevaluatedRecommendations` for blocked-by-unknown mapping outcomes, `detectedTechnologiesTotal`, `visibleTechnologiesTotal`, `recommendedSkillsTotal`, `visibleRecommendedSkillsTotal`, and normalized `filters` and `sort` objects. `--technology` and `--rule-kind` narrow the **technology list** only, while `--rec-technology` narrows recommendations; `--query-target` selects which list receives `--query` (default `both`); `--registry`, `--repository`, `--priority`, `--status`, `--quality`, `--score-state`, `--min-score`, and `--min-repo-average` narrow the **recommendation list** only. `--query` is applied according to `--query-target` (both by default). TUI technology/recommendation filter state is likewise independent. Neither CLI nor TUI filtering changes underlying detection or recommendation calculation. `nosey search --json` returns `groups` with nested `technologyId`, `repositories` (including `repositoryId`, `averageScore` and optional `scoreCount`), and `skills` (including `quality`, `score`, `selected`, and stable ID); it also returns `totalUniqueSkills` so duplicated cross-technology rows are not double counted. Nullable output fields represent absent ratings, whereas a `0` rating remains a real value. See the complete exit-code table in §5.1; outputs use `outputVersion: 2` and a structured `diagnostics` array.


### 5.1 Exit codes and stable JSON results (normative)

| Code | Meaning | Examples |
|---|---|---|
| 0 | Successful operation, or warning-only incomplete scan | `scan.complete=false` on a bounded scan, with stderr warning |
| 2 | Usage, validation or policy-blocked operation | non-TTY init without `--yes`, blocked conflict/cycle, invalid registry, untrusted registry explicitly required by command, invalid template or init destination |
| 3 | Required registry/cache/network unavailable | no usable official or selected registry, template download unavailable |
| 4 | Detection failed or strict scan incomplete | `nosey scan --strict` and `complete=false` |
| 5 | Installation failure or partial success | per-skill results on stderr/JSON diagnostics |
| 6 | Missing/incompatible prerequisite | Node.js, Git, pinned Skills CLI or unsupported agent |

A non-interactive scan skips untrusted project registries with a diagnostic; exit 0 unless an explicitly requested source is mandatory. `registry validate` fails with 2 for errors and 0 for warnings. `registry init` creates files atomically only after all validations and uses 2 for destination collision/data errors and 3 for missing template source. All machine-readable command outputs (`scan`, `search`, `install --dry-run`, `registry init`) contain `outputVersion: 2`, `diagnostics: []` and validate against their corresponding `schemas/*-output.schema.json`. Unknown installed state is always represented as `unknown`, never coerced to `missing`.

**Output contract v2:** The four JSON output schemas in `schemas/` are normative planning contracts, not examples of current execution. All fields consumed by the TUI/CLI have defined types and enumerated values; nested objects are closed unless an explicitly named extension point is specified. In scan output, `filters` and `sort` are fully normalized regardless of user-supplied flags; multi-select facets are arrays (empty means no restriction), `scoreState` is `any|scored|unscored`, and numeric threshold defaults are `null`. Scan `coverage` reports execution completeness and technology-coverage gaps independently. Confirmed recommendation entries require ID, name, state, priority, cause technology IDs, reasons, installed status, selection state, and explicit null-vs-zero ratings. `unevaluatedRecommendations` carry blocking unknown IDs. Search returns named technology/repository/skill groups with explicit nullable scores; install plans distinguish actual command `source` from `logicalSource`, permit `null` catalog skill IDs for direct installs and carry source provenance; registry-init records use `sourceRevision` and `createdBy`. Cross-record invariants (count totals, unique IDs, aligned grouped skill IDs/names, command-item membership, valid references, ordering and coverage equation) are mandatory semantic requirements in addition to JSON Schema validation. None of the four schemas claims to be a runnable implementation or supplied test fixture.


**Exact normalized Scan filter defaults and sort keys:** `filters` requires `query: ""`, `queryTarget: "both"`, and always-present arrays `technologies`, `recommendationTechnologies`, `ruleKinds`, `registryIds`, `repositoryIds`, `priorities`, `statuses`, `qualities` (all `[]` by default). It additionally requires `scoreState: "any"`, `minScore: null`, `minRepositoryAverage: null`. An empty array means no restriction, including for `--status any`; a specified CLI single-value facet is normalized into a one-member array. `sort` requires `technologies: "name-asc"` and `skills: "priority-desc"` by default, with the corresponding allowed enums from §4. The query target and facets remain presentation-only. All keys must appear in `scan --json` even when defaulted, avoiding ambiguously empty objects. Search output uses the same comparator rules; it is not required to duplicate Scan's filter model.


### Registry initialization command contract

`nosey registry init PATH` starts an interactive wizard when connected to a TTY. The wizard preselects `workspace` as its starting template but does not import detected technologies until the user confirms the selection. `--template empty` makes a blank registry; `--template workspace` or `--from-scan` also offers detected technology and associated mapping/skill references. Noninteractive initialization requires `PATH` and either `--template` or `--from-scan`, plus `--yes` to approve writing files. `--template-registry ID` picks a configured, validated registry that explicitly publishes the requested template; otherwise Nosey always looks in the configured **official default Git registry**, not arbitrary higher-priority overlay registries. `--register` adds the generated local directory to the user's settings; `--git` optionally invokes `git init` in the new directory. Both are opt-in and independent. `--json` prints the created path, registry ID, template ID, source registry ID, `sourceRevision` (validated Git commit SHA or null for local templates), `createdBy`, output file list, and validation results. The TUI exposes the same wizard under **Settings → Registries → New registry** (`n`); **Add existing** (`a`) remains distinct.

Initialization fails without writing partial output when the destination already has conflicting files, a template is unavailable, or schema validation fails. No overwrite/merge mode exists in MVP. `--template-registry` must reference a registry already registered in Nosey; use `nosey registry add` first for a custom Git URL or local folder. `--template-ref` is intentionally not needed: template selection is resolved against the exact validated snapshot SHA, which is recorded in the generation metadata. The user may pin the source registry to a Git commit using existing source settings.

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


## 7. Merge algorithm and precedence

**Order:** Start with the official default external registry at priority `0`, then enabled custom registries in ascending numeric priority (`0–1000`). By default a user-added registry receives `20` and a project-local registry `30`; both are editable. Equal priority is resolved by explicit saved registration order, with later entries winning; duplicate registry IDs are a validation error. A user/project settings override for the same registry ID is resolved by settings precedence (`project > user > application defaults`) before data merging.

**Entity rules:**

1. Collections `technologies`, `skills`, `mappings`, and optional `repositories` merge items by `id`.
2. Within a technology, `rules` merge by their `id`; within mappings, nonempty `recommendations` arrays merge entries by `skillId`; an explicit empty `recommendations: []` clears every inherited recommendation for that mapping. An omitted `recommendations` property leaves inherited entries unchanged.
3. All scalar fields, including skill `quality`/`score` and repository `averageScore`/`scoreCount`, use the highest-priority specified value. Missing values inherit from lower priority. `null` is not deletion syntax for the v2 skill/mapping schemas; a field must be overridden by a valid replacement value, or deletion must be explicitly designed later.
4. Non-entity arrays (e.g. `tags`, `requires`, `conflictsWith`, `allTechnologies`) replace atomically. `source` and `when` objects replace atomically to prevent hybrid sources or unexpected condition weakening.
5. `disabled: true` tombstones a merged entity, rule or mapping skill record. A higher-priority `disabled: false` may re-enable it. Disabled entries remain in diagnostics/provenance but are absent from effective Scan/Explore indexes.
6. Every active effective technology requires `name` and `category`; non-`metadataOnly` technology identities require at least one enabled, valid rule with `kind` and an appropriate matcher, while an effective `metadataOnly: true` identity may legitimately have zero rules and remains undetected without positive evidence. Every active skill requires `name` and `source`; every active mapping requires `technologyId` and a `recommendations` collection. An active explicitly declared repository needs `name`, `kind`, and an appropriate Git `uri` or local `path`. Incomplete effective objects invalidate only affected objects, with diagnostics. Structural corruption of the manifest invalidates the registry snapshot.
7. Preserve origin of each materialized field and show it in detail/debug views. Validate all references and dependency cycles after merging. Resolve each `repositoryId` and attach only authored manifest averages after all overrides and disabled entries are applied; absent scores remain null.

**Detection override invariants:** The `kind` of an existing rule ID cannot change between registries. Changing alternative selector forms (e.g., file `glob` to `pathRegex`) requires a *new* rule ID and disabling the original. The merged effective matcher must pass the same schema checks as a standalone rule. Missing properties on overlay fragments inherit; a rule with only `id` is legal in a source file only when a complete lower-priority rule exists. An overlay that creates a rule with no `kind` or matcher fails effective validation, and its technology is diagnosed. Do not silently skip malformed active rules.

**Example:** Default skill `{id:"x", name:"Original", tags:["base"], source: ...}` plus custom `{id:"x", name:"Custom"}` produces `name:"Custom"`, original `tags`, and original `source`. A custom `{id:"x", tags:[]}` clears tags; it does not union them.

### Workspace trust, source validation and registry precedence (normative)

Workspace registry entries under `<workspace>/.nosey/config.json` are inert until the user approves a fingerprint of the normalized workspace-root path plus the canonical JSON registry entries. Approval is saved **only** in user config; changing entries or workspace path requires renewed approval. `nosey trust` displays and approves the current fingerprint; `nosey trust --revoke` invalidates it. In non-TTY mode, untrusted project registries are skipped with `W_UNTRUSTED_PROJECT_REGISTRY`; no prompt or code execution occurs.

Allowed Git source syntaxes in user registry config, `registry add`, repository definitions, and skill sources are `owner/repo`, `https://host/path`, `ssh://[user@]host/path`, and `git@host:path`. Reject option-like leading `-`, `ext::`, `file:`, control characters, embedded credentials, and paths escaping the allowed workspace or registry checkout. Refs require `^[A-Za-z0-9][A-Za-z0-9._/-]*$` and must not contain traversal segments. Use argv arrays, never a shell; reject ambiguous options. Do **not** emit `--` as an option terminator under the pinned `skills@1.7.6` grammar. The generated installer plan includes validated source URI, ref and final provenance, plus distinct `skillIds[]` (nullable Nosey catalog IDs for uncatalogued direct installs) and `skillNames[]` (upstream names) for each grouped command.

Registry Git clones must set `GIT_ALLOW_PROTOCOL` to approved transport protocols, `protocol.ext.allow=never`, `GIT_TERMINAL_PROMPT=0`, disable hooks, submodules, LFS smudge and filters, and enforce bounded download, file count, depth, and symlink restrictions. Content can never run scripts. A follow-up check rejects any symlink resolving outside the checkout, including nested template and local-skill paths. Local adapters are installed only in a user-level trusted directory, and may be enabled only from user-level configuration. Every adapter is pinned by ID, version, and content hash, spawned out-of-process with bounded JSON stdin/stdout, timeout, output size and normalized redacted evidence; project config cannot enable adapters. Adapter execution is **not** a sandbox: it requires explicit user trust. `nosey doctor` lists registered adapters and their signatures.


## 8. Registry fetching, caching and trust

- Sources: Git repo URL or local directory; **not** a raw JSON URL in v1. The application ships with a configurable pointer to the official external Git registry, not a bundled registry/template checkout (actual URL is a release/build configuration decision).
- Git repositories are fetched into a content-addressed cache. Record commit SHA, fetch time, manifest hash, validation status, and origin URL. Local directories are read live.
- Default TTL: 24 hours (user configurable, 1–168 hours). On start, prefer a valid cached snapshot to unblock the TUI; refresh when due. Manual `registry update` bypasses TTL.
- Failed fetch: retain the last validated snapshot and show a nonfatal stale badge; if no snapshot exists, exclude that registry and show an actionable error. Never replace a valid cache with an invalid update.
- Refs/branches can be pinned; show resolved SHA and stale warning. For repeatable installs, preserve the source reference in the plan.
- Credentials must be delegated to installed Git/credential helper; do not store credentials in Nosey configuration or logs.
- Prevent registry path traversal, symlink escape, oversized JSON/archives, malicious ANSI, arbitrary shell execution or unsafe/untrusted detector loading. Remote registries are data only; executable custom adapters require explicit local installation and user trust.

### Stable logical installation origin (normative)

The canonical Nosey install origin combines the declaring registry's stable identity, the skill's logical Git origin or registry-relative local path, its requested ref if any, and the upstream `SKILL.md` frontmatter name. For direct installs, record the user-provided source and mark the origin `uncatalogued`; never fabricate a catalog ID. The canonical origin MUST NOT include a content-addressed cache directory, temporary checkout path, or mutable absolute cache root. For a Git-backed Nosey registry whose local skill is installed by passing a cached checkout directory to upstream, preserve *two* sources: the logical origin for identity and the actual path passed to `skills add` for command auditing. The resolved Git checkout SHA is separate from any requested ref and is recorded only when actually established.

Store Nosey-owned project attribution in `<workspace>/.nosey/install-origins.json` (`schemas/install-origins.schema.json`), keyed by stable origin, upstream skill name and target agent IDs. This is an attribution record, not installation proof: post-install status always reconciles it with the pinned project reader and physical agent scope. Cache rotation, refetch or garbage collection alone MUST NOT create a duplicate Manage item or flip an otherwise unchanged skill from installed to missing. Actual upstream lockfile attribution and local-cache source behavior remain mandatory version-pinned compatibility checks before implementation sign-off.

### Mapping determinism (normative)

For a skill recommended by multiple passing mapping entries: the highest priority wins (`core` > `recommended` > `optional`); missing `priority` means `optional`. `requires` and `conflictsWith` are set unions of catalog-level values and values in all **passing** mappings; reasons accumulate in deterministic mapping-ID order. An entry whose own `when` fails contributes nothing. Graph cycles over the effective combined `requires` graph block installation of affected items. Conflicts with already-installed skills are never silently removed: the blocker offers deselection, a clearly labeled **copy-only** upstream removal command (not executed by Nosey), or an explicit `--ignore-conflicts` override with named conflicts displayed and confirmation. No automatic removals exist in MVP.


## 9. Detection and recommendation engine

1. Discover the workspace root from `--cwd` or the current directory; identify nested project boundaries in monorepos; record all evidence paths relative to the displayed root.
2. Walk files with configurable excludes; always skip `.git`, dependencies, generated artifacts and binary contents by default. Honor ignore rules where feasible without hiding explicitly configured manifest checks. Normalize paths to `/` before evaluating glob/regex matchers.
3. Resolve the versioned technology documents and their existing overlay contracts. Treat the supplied v2 rules as verified and unchanged; no additional catalog rule-validation work is required. Determine **runtime capability** separately: a rule may be valid but not evaluable with the available ecosystem provider or trusted detector. Keep any unavailable capability distinct from a negative match, and surface diagnostics without silently suppressing coverage gaps.
4. Evaluate available `file`, `dependency`, `content`, `extension`, `dotenv` and trusted `detector` rules, plus `mcp_package` only on explicit opt-in. The dependency observation layer includes version-2 Docker, Terraform/provider/resource, GitHub Actions, Deno and Hex when corresponding parsers are available. Cache parsed manifests by project, enforce bounded reads and adapter timeouts; dotenv inspects keys only. An absent parser is `not-evaluated` rather than false.
5. Combine results using logical **OR** across active rules. Continue gathering bounded evidence from other matched rules even after the first positive match; never use rule count or metadata ratings as detection confidence. Evidence includes technology ID, rule ID, kind, project-relative location, and safe matched identifier; a monorepo may contain multiple instances of one technology.
6. Resolve mappings by detected technology, evaluate `when`, merge duplicate skill recommendations, then filter disabled/uninstallable items. Recommendations can be enriched with the normalized detection evidence explaining their origin.
7. Order Scan recommendations by `core`, `recommended`, `optional`, then name. Explore uses separately configured group-aware sort and filter semantics. Do not preselect any skill. Show why a recommendation was made.
8. Inspect local installed state via a versioned `InstalledSkillReader` (prefer pinned `list --json`, then use a conservative fallback only when justified); if unavailable, show 'unknown' and avoid false claims of installation. Never silently fall back to checking global scope.

**Default safety budgets (proposed, tunable):** Skip content files larger than 1 MiB, bound file depth/count and regex input length, limit detector runtime, and mark execution incomplete only when a *required* evaluation is truncated or its provider/adapter is unavailable; optional excluded capabilities instead reduce technology coverage without automatically failing strict mode. The supplied rule catalog is treated as verified and is not revalidated as part of this update. Never print dotenv values or arbitrary matched content. Benchmark exact limits before release. Exclude example/template dotenv files (`.env.example`, `.env.sample`) from implicit dotenv scanning; authors can explicitly include them using a rule-specific glob, and the UI must still not show values.

### Installed-state inventory reader (normative; pinned upstream 1.7.6)

`InstalledSkillReader` exposes `read(workspace, agents): InstalledSkillRecord[] | UnknownStatus`. Prefer the supported `skills list --json` output of the pinned upstream release. Its items expose `name`, `path`, `scope`, `agents` (display names), `source`, `sourceUrl`, `sourceType` and upstream tracking `status` (`tracked`, `untracked`, and `missing` where present). Map agent *display names* to canonical agent IDs using a pinned compatibility map; an unknown or ambiguous display name prevents a definite agent-specific installed claim. Do not copy upstream tracking states directly to Nosey's `installed | missing | unknown` enum. Confirmed `installed` requires positive project-scope identity and agent evidence; `missing` requires a complete project reader with no matching identity and no ambiguities; malformed/partial reader output or source/agent mismatch results in `unknown`. No fallback to a global list is permitted.

For a specifically unusable structured inventory, a documented conservative fallback may inspect human-readable output, an upstream lockfile or known agent-local skill directories; weaker observations must not be promoted to definitive `installed` or `missing` without corroboration. Verify the `SKILL.md` frontmatter name, preserve unmatched names as **unmanaged** in Manage, and show an explicit caution for unknown selectable items. The stable key is the canonical *logical origin + requested ref where applicable + upstream frontmatter skill name* as described in §8, not the temporary local checkout path. Project-scoped Nosey origin receipts support attribution but cannot alone prove installation.


## 10. Installation execution and preflight

Nosey spawns the upstream executable through argv and the workspace root as `cwd`, never a shell; Node.js 22.20+ is documented upstream. Release config pins the supported upstream `skills` version. Proposed baseline for validation is **`skills@1.7.6`**, which must be exercised in CI before release; exact dependency versions may advance only with golden-fixture updates. An optional `installation.cliVersion` may select an exact version **inside** Nosey's published range. An incompatible version causes `nosey doctor` / install prerequisite exit 6; scan and Explore remain available. `npx --yes skills@1.7.6 ...` is the illustrative pinned invocation (not a claim that v1.7.6 was executed in this spec environment).

**External single-skill command:**

```bash
npx --yes skills@1.7.6 add vercel-labs/agent-skills --skill web-design-guidelines --agent claude-code --yes --json
```

**Grouping:** Resolve the finalized, deduplicated skill selection and group by `(normalized source, ref, sorted agents, method)`. Use one `add` per group with a deterministic, version-pinned serialization of multiple `--skill` values. In `skills@1.7.6`, `--skill` consumes values until the next dash-prefixed argument; do not assume repeated flags or a `--` separator work without a compatibility check. An implementation may use independently verified single-skill calls where grouping cannot be encoded unambiguously. The dry-run JSON plan carries parallel `skillIds[]` (nullable catalog IDs) and `skillNames[]` per command, and each `items[]` entry includes `skillId`, `skillName`, `source` (actual execution source), `logicalSource`, and provenance; their ordering is stable and the arrays must be the same length. Verify each member afterward. If an entire group fails, retry individual members only when safe and not already confirmed installed; never claim a partial group installed solely because the process exited 0. Abort dependent items when prerequisites fail.

**Agent contract:** Agent auto-detection only supplies proposed defaults in the TUI confirmation. Every executing command gets a user-confirmed explicit agent list; non-interactive installs need repeatable `--agent ID` or configured `installation.agents`. Never emit `--all`, `--agent '*'`, or `--global`. Agent names come from a pinned version compatibility map, CI-checked against upstream. Unknown agent IDs produce warnings and block execution until corrected.

**Pinned source and argument grammar:** In the tagged `skills@1.7.6` source parser, `owner/repo@skill` selects a *skill*, not a Git revision. A Git ref uses a `#ref` fragment; `#ref@skill` combines a ref and a skill selector in supported source forms. Prefer `--skill` to make skill selection explicit. Encode a Git ref only for a source form supported by the pinned parser; for unknown URL/SSH/scp combinations fail preflight rather than silently drop or misinterpret a requested ref. Local paths do not accept Git refs. The plan preserves `requestedRef` separately from optional `resolvedRevision`; an unverified symbolic ref is never reported as a resolved commit. Do not emit `--` as an option terminator: the pinned parser does not implement it as a terminator. Strictly reject option-like, path-traversal and ambiguous user-controlled tokens, use argv arrays (never a shell), and check multi-skill argv segmentation against the pinned parser before executable grouping.

**Structured per-skill outcomes:** Prefer `skills add --json --yes` for each permitted execution group and parse its per-skill installed/failed entries and available source/ref/hash/path/agent fields. Neither a zero process exit nor a successful group-level response proves each skill installed. Reconcile each individually successful result with subsequent project inventory and verified agent scope; record ambiguity as `unknown`. Partial failures must retain per-skill results and may retry only independently failed items when idempotent; never rerun already verified installs. Treat stdout/stderr as untrusted bounded content; JSON parse failure does not become success. The source parser, flag grammar, list/add output shapes, agent display-name mapping, actual Git revision resolution and cache-path tracking must be proven on the pinned release before implementation approval.

**Direct source bypass:** `nosey install --source URI --skill SLUG` is a user-requested `uncatalogued` installation. It bypasses catalog *lookup*, not source validation or trust/policy gates. It carries no invented Nosey skill ID, mapping cause, repository score or official assessment; the plan/Manage record exposes user-supplied source, logical origin, upstream skill name, ref (if valid), project-only agent scope and install method. Interactive execution requires confirmation and noninteractive execution requires `--yes`.

**Preflight:** Validate workspace write access, CLI version, user-approved scope, list of agents, trusted sources, source/ref syntax, `SKILL.md` frontmatter, selected install method, catalog/mapping prerequisites and conflicts. Source overrides show their final URI and registry provenance. Project/unpinned overrides require separate approval. `--ignore-conflicts` is an explicit opt-out and never selected by default. `--dry-run` / `--print` never invoke upstream; `--copy` means file copy installation, distinct from the TUI **Copy commands** action. `--yes` is required for non-interactive execution.

**Upstream-telemetry:** The CLI documents anonymous telemetry and `DISABLE_TELEMETRY`. Nosey collects **no product telemetry in the MVP**. Its subprocess inherits the user's telemetry environment by default (`installation.upstreamTelemetry: inherit`); setting `disabled` passes `DISABLE_TELEMETRY=1` and `DO_NOT_TRACK=1`. Nosey does not prevent upstream traffic unless the underlying environment does.

**Known upstream commands:** `add`, `list`, `find`, `check`, `update`, and experimental sync are owned by upstream. Nosey differentiates through workspace detection, conditional mappings, overlays, local registries, and author-supplied ratings. The v1.7.1 upstream compatibility contract and unresolved runtime obligations are contained here (§§8–10 and §16); the tagged `skills@1.7.6` parser documents `#ref` and machine-readable `list/add` behavior, while actual install side effects and lockfile reconciliation remain unverified at planning time. All command plans are serialized as tokens first and only escaped for preview/printing; stderr/stdout are bounded and scrubbed.

## 11. Settings design

### Storage

- User: `$XDG_CONFIG_HOME/nosey/config.json` (fallback to platform-appropriate config directory).
- Optional workspace: `<workspace>/.nosey/config.json`, values override user preferences for the current workspace.
- Cache: `$XDG_CACHE_HOME/nosey/` (or platform equivalent); do not check it into a workspace.
- Untrusted project registries are inert until approved with `nosey trust` or the equivalent Settings → Registries trust review; both write to the same user-owned trust store. A changed canonical project-registry fingerprint automatically revokes that approval. Only user-level config can activate detector adapters. Registry content is read-only from the TUI; edits to repository content happen in the user's editor/Git workflow.
- Persist config by writing a temporary validated file then atomic rename. Show unsaved changes and confirmation before destructive removal.

### Fields

| Settings section | Fields | Defaults |
|---|---|---|
| Registries | ID, Git URL/local path, enabled, priority, pinned ref | Built-in default registry at priority 0 |
| Refresh | TTL hours, refresh now, cache status | 24 hours globally, per-registry override (`ttlHours`) |
| Installation | Agent IDs, method, default mode, verbose | Agent detection used only for confirmation defaults; symlink, execute, off; pinned CLI version and upstream telemetry inherit |
| Detection | File exclusions, max content bytes, MCP package inspection | Standard generated/vendor excludes, 1 MiB, MCP inspection off (explicit opt-in) |

Validate when adding/editing a registry; indicate offline/pinned/incompatible sources. Changing registry order/priority triggers a merge and Scan/Explore refresh, without automatically installing anything. Settings → Registries MUST list workspace registries pending approval with their canonical workspace root, origin URI/path, registry IDs and full canonical fingerprint; provide explicit **Approve** and **Revoke approval** actions. There is no implicit trust on first use, and no project-config flag may approve its own registry. `registry add/remove` are user-scoped; project-scoped registries are edited through the project settings file or Settings workspace controls with an approval step.

### Settings merge (normative)

User config is the base and workspace config overrides it. Scalar leaves replace, `registries` merge by stable ID, and `installation.agents` / `detection.exclude` arrays replace atomically. `detection.enableMcpPackage` is an ordinary boolean scalar, defaulting to false; temporary `--enable-mcp` applies only to that invocation. Local `source.path` in workspace settings is resolved from the workspace root; a user config local registry path must be absolute or `~/`-prefixed. The default priority for a workspace-local registry is 30. A registry-specific `ttlHours` overrides `updates.ttlHours`, otherwise default TTL is 24h. Official registry starts at priority 0 and is first for equal-priority comparison. Project settings may not activate adapters. Trust-state fingerprints are maintained in the user-controlled trust store.


## 12. Technical architecture

Recommended implementation: **TypeScript (Node.js 22.20+)** to interoperate with the Skills CLI and support JSON Schema validation; a React-based TUI such as Ink is a reasonable choice but not mandated. Keep core use cases independent of terminal rendering.

```text
CLI command parser / TUI shell
            │
            ▼
Application service (scan, search, install, settings)
            │
    ┌───────┼───────────┬─────────────┬──────────────┐
    ▼       ▼           ▼             ▼              ▼
Registry  Validator   Detection   Recommender   Skills CLI adapter
sources   + merger    engine      + index       (spawn, verify)
    │       │           │             │              │
    └───────┴───────┬───┴─────────────┴──────────────┘
                    ▼
             Cache / Settings / Logs
```

**Modules:** `technology-document-decoder-v1-v2`, `detector-capability-registry`, `detector-mcp-config`, `detector-docker`, `detector-terraform`, `detector-github-actions`, `detector-deno`, `detector-hex`, `registry-source` (Git/local), `registry-cache`, `schema-validation`, `registry-merger`, `detector-file`, `detector-manifest`, `detector-content`, `detector-extension`, `detector-dotenv`, `detector-adapter`, `recommendation-engine`, `search-index`, `skills-cli-adapter`, `settings-store`, `cli`, `tui`. Unit-test services without spawning an actual TUI; integration-test the installer adapter with a fake process runner.

**Internal normalized types:** `Technology{id,name,category,rules[],metadataOnly?}`, `Detection{technologyId,state:detected|absent|unknown,evidence[],coverageGaps[]}`, `RuleEvaluation{ruleId,projectPath,scope:required|excluded-by-mode|not-applicable,status:matched|not-matched|not-evaluated,reason?}`, `DetectorCapabilities{supportedRuleKinds,supportedEcosystems,unavailableDetectors,mcpPackageEnabled}`, `DetectionEvidence{technologyId,ruleId,kind,projectPath,path?,matchedIdentifier?}`, `Skill{id,name,source,repositoryId,quality?,score?,provenance}`, `Repository{id,name,kind,averageScore?: number|null,scoreCount?: number|null,provenance}`, `SearchGroup{technologyId,repositories[]}`, `Recommendation{skillId,state:confirmed|unevaluated,priority?,reasons[],requires[],conflictsWith[],unknownTechnologies[]}`, `ScanCoverage{requiredRuleCount,evaluatedRequiredRuleCount,unevaluatedRequiredRuleCount,excludedRuleCount,notApplicableRuleCount,unknownTechnologyIds[],gaps[]}`, `InstallPlan{workspace,agents,method,items[],commands[],logicalOrigins[]}`, and `RegistrySnapshot{id,sha,loadedAt,diagnostics}`. Status values are typed enums, never inferred from free text.


### 12.1 Architecture goals, principles and deployment model

**Proposed design.** Build a single distributable `nosey` Node.js CLI, with a renderer-independent application core used by both terminal UI and noninteractive commands. No daemon, browser service, REST server, account server, SQL database or backend service is necessary for MVP. Keep orchestration synchronous at the command boundary while allowing bounded asynchronous Git refresh, detection and child-process execution. Node.js 22.20+ and TypeScript are the source-recommended baseline. Prefer a compiled ESM application with the exact Node/package-version strategy approved during build-system selection; use one lockfile and reproducible CI installation. Ink is a candidate renderer, not an approved dependency.

Architecture priorities, in order: **(1) safe user intent and source trust, (2) deterministic and explainable outcomes, (3) explicit uncertainty, (4) stable machine-readable contracts, (5) fast warm-cache operation, (6) maintainable local deployment**. Optimizations must never obscure install source, reinterpret an unknown as absent, auto-approve project registries or silently change project/global scope.

### 12.2 Component and trust-boundary diagram

```text
                  UNTRUSTED EXTERNAL DATA / PROCESSES
    Git registry remotes         Skill Git/local sources      Pinned Skills CLI
           |                              |                        ^  |
           v                              v                        |  v
    +-----------------+       +----------------------+       +------------------+
    | Hardened Git    |       | Preflight + source   |------>| argv-only child  |
    | fetch adapter   |       |/ref validation       |       | process adapter  |
    +--------+--------+       +-----------+----------+       +--------+---------+
             |                            |                           |
       TRUST/VALIDATION BOUNDARY          |                   project agent dirs
             v                            |                   + origin receipt
   +---------------------+                |
   | immutable validated |                |
   | registry snapshots  |                |
   +----------+----------+                |
              v                           |
  +-------------------------------+       |
  | source decode -> overlay merge|       |
  | -> effective schema + semantic|       |
  | validation -> provenance index|       |
  +---------------+---------------+       |
                  |                       |
      +-----------+-------------+         |
      v                         v         |
 +------------------+  +------------------------+
 | bounded detection|  | skill index / mapping  |
 | + coverage graph |->| and 3-state evaluation | 
 +------------------+  +------------+-----------+
                                   |            |
                          +--------+------------+------+
                          | application use cases     |
                          | scan/search/install/init  |
                          +--------+------------+------+
                                   |            |
                              CLI JSON       TUI state
                              stderr         one-column UI

   TRUSTED LOCAL CONTROL: user config, approved workspace fingerprint,
   explicitly trusted user adapters, versioned schemas and source-policy code.
```

**Boundary A — data ingress:** Every Git/local registry and its template/skill metadata is untrusted until path-constrained, bounded, decoded, schema-validated and semantically reconciled. Git transport credentials remain owned by the user's Git helper. **Boundary B — execution:** No catalog metadata can run code. Only an explicit user-confirmed install invokes the pinned external installer, and only a user-installed, hash-pinned adapter can run detector code. **Boundary C — user workspace:** Read scanning is bounded; project skill files and Nosey attribution receipts are written only by explicit installation or initialization actions. **Boundary D — presentation:** Evidence, filenames, registry descriptions, subprocess output and terminal control characters are escaped and secret-minimized before display/logging.

### 12.3 Module decomposition and dependency direction

| Layer | Module or port | Responsibilities | Key outputs |
|---|---|---|---|
| Presentation | `cli`, `tui`, view-state adapters | Parse args, keymap, accessibility, prompts, dialogs, JSON + stderr; no business rules | Render models or machine outputs |
| Application | `scan-service`, `search-service`, `install-service`, `registry-service`, `settings-service`, `doctor-service` | Order use-case steps, policies, cancellation and diagnostics | Result envelopes and plans |
| Pure domain | `registry-merger`, `technology-normalizer`, `condition-evaluator`, `recommendation-engine`, `install-planner`, `comparators` | Deterministic transforms; no process spawning | Effective entities, detection state, plans |
| Domain policy | `coverage-aggregator`, `trust-policy`, `source-policy`, `provenance-resolver` | Invariants, uncertainty, identity and authorization | Explicit states and blockers |
| Infrastructure | `git-provider`, `local-provider`, `snapshot-cache`, `schema-validator`, `filesystem-walker`, ecosystem observers | External and filesystem I/O behind typed ports | Validated snapshots and bounded observations |
| External boundary | `skills-cli-adapter`, `installed-skill-reader`, `process-runner`, `adapter-runner` | Exact argv, version pin, source reconciliation, per-skill evidence | Observed outcomes; no optimistic success |

**Dependency rule:** presentation → application → domain ports; infrastructure implements domain ports. Avoid importing TUI code into detectors or decision engines. Inject `Clock`, `Filesystem`, `ProcessRunner`, `RegistryProvider`, `InstalledSkillReader`, `Environment` and `Logger` for deterministic unit/integration tests. Keep technology data schemas and CLI schemas as separately versioned public contracts; never reuse renderer-only types as wire formats.

### 12.4 Proposed repository and package organization

This is a **candidate source tree**, not a statement about files supplied in the planning ZIP:

```text
src/
  bin/nosey.ts                     # process entry, signal cleanup
  cli/{parse,commands,json,errors}.ts
  tui/{app,shell,scan,explore,manage,settings,modals,accessibility}/
  application/{scan,search,install,registry,init,settings,doctor}/
  domain/registry/{decode,merge,provenance,semantic-validation}/
  domain/detection/{model,coverage,engine,observers,conditions}/
  domain/recommendations/{evaluate,combine,sort-filter,install-graph}/
  domain/installation/{origin,plan,preflight,reconciliation}/
  infrastructure/git/{fetch,snapshot,hardening}/
  infrastructure/cache/{snapshot-store,locks}/
  infrastructure/config/{settings-store,trust-store}/
  infrastructure/skills/{cli-compat,runner,list-reader,add-reader}/
  infrastructure/adapters/{trusted-runner,registry}/
  schemas/                         # checked-in versioned contract copies
  fixtures/                        # engineering to author; absent in source ZIP
  tests/{unit,contracts,integration,e2e}/
```

Expose interfaces via domain/application modules and keep integration-version quirks in `cli-compat`. Schema copies should be generated or copied from a single, checked-in source during release preparation and compared byte-for-byte to the approved contracts. No fixture/code directories are claimed to exist today.

### 12.5 End-to-end scan / search sequence

1. `WorkspaceResolver` determines `--cwd`, project root and eligible nested project boundaries without escaping approved boundaries; normalize all evidence to workspace-relative slash paths.
2. `SettingsResolver` loads validated user + project configuration and checks the user-trust fingerprint before allowing project registry additions. A rejected workspace registry creates a diagnostic, not an implicit allow decision.
3. `SnapshotResolver` returns last validated immutable Git snapshots immediately when available; TTL refresh can update an **entire** snapshot only after validation. A CLI scan must record the snapshot IDs/revisions it actually used; asynchronous updates cannot change a result halfway through construction.
4. `RegistryCompiler` decodes technology-doc v1/v2 separately from manifest v1 and skills/mappings v2, validates sources, merges by defined precedence, validates effective entities and relationships, and materializes field-level provenance.
5. `ObservationPlanner` classifies in-scope active rule/project pairs as `required`, `excluded-by-mode` or `not-applicable`; selects actual providers based on capability and opt-in settings, then runs bounded scans.
6. `DetectionEngine` computes `matched`, `not-matched` and `not-evaluated` with reason, then derives three-state technology classification and the required coverage equation. Positive evidence wins; missing negative evidence cannot prove absence.
7. `RecommendationEngine` applies the same three-valued condition evaluator to mapping anchor + clauses; emits confirmed and unevaluated entries separately, unions passing requirements, and preserves evidence/provenance.
8. `InstalledSkillReader` reads only project-scope results from the pinned upstream implementation, returning `unknown` if source, scope, agent or inventory completeness cannot be established.
9. `ViewProjector` applies immutable presentation-only filter and sort functions to the computed scan/search model. The TUI shares selected IDs across duplicate Explore rows and preserves hidden selections; JSON serializes the canonical normalized v2 schemas.

**Failure isolation:** Broken overlay entities produce diagnostics; a structurally invalid registry snapshot is never activated. A source fetch error uses the last validated snapshot, and an unavailable required detector makes `complete=false` rather than converting unknown into absence. A failed installed-state reader must not prevent browsing.

### 12.6 Data model, normalization and key invariants

All data models should be strongly typed and use discriminated unions rather than ambiguous nullable booleans. Canonical persisted/wire fields remain defined by the package's schemas; the following are **internal** design types, not added JSON requirements:

| Model | Key | Source of truth | Invariant |
|---|---|---|---|
| `RegistrySnapshot` | `(registryId, sourceRevision, contentHash)` | Validated Git snapshot or live local root | Only all-valid snapshots replace cache pointer |
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

**Providers:** built-in `file`, `dependency`, `content`, `extension`, `dotenv`; named dependency observers for npm, python, cargo, go, ruby, composer, Docker, Terraform provider/resource, GitHub Action, Deno and Hex; opt-in passive MCP-package reader; and externally registered user-trusted detector adapters. A vocabulary enum is not proof that a parser is implemented. Each provider implements `observe(projectContext, budget, cancellation): ObservationResult` and declares concrete supported ecosystems, rule kinds, provenance and budget behavior.

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

**Verification rule:** A process exit code or group-level success is **insufficient**. Parse bounded `skills add --json --yes` output per skill and reconcile with `skills list --json` project-scope inventory and approved agents; map agent display names through a pinned compatibility table. Mark `installed` only with enough evidence to satisfy the v1.7.1 contract; `unknown` where source/ref/agent identity or reader completeness is ambiguous. Write/update `.nosey/install-origins.json` only as attribution, never as independent proof. Failed prerequisites block dependent operations; retries must exclude verified installed items. Since upstream effects are external, automatic rollback/removal is explicitly prohibited. Cancellation records incomplete work.

### 12.11 Versioned local API and machine-output contracts

There is **no public HTTP API** in MVP. The application exposes a **local command API**. The corresponding JSON outputs are the public integration contracts; readers must use the exact shipped schemas, with `outputVersion: 2` and `diagnostics: []` on all four required output shapes.

| Command/result | Schema | Minimum semantic contract |
|---|---|---|
| `nosey scan --json` | `schemas/scan-output.schema.json` | `complete`, required coverage equation, detected/confirmed/unevaluated distinction, full normalized filters/sorts and visible/total counts |
| `nosey search --json` | `schemas/search-output.schema.json` | Stable technology/repository/skill grouping, explicit nullable scores, `totalUniqueSkills` deduplication |
| `nosey install ... --dry-run --json` | `schemas/install-plan-output.schema.json` | `blocked`, source/agent/method, aligned command `skillIds[]` and `skillNames[]`, actual vs logical source, conflicts and overrides |
| `nosey registry init ... --json` | `schemas/registry-init-output.schema.json` | Template/source IDs, `sourceRevision`, `createdBy`, created files, final validity and diagnostics |

Other operations (`list`, `registry list`, `doctor`) may offer JSON as described in §5, but do not invent a published v2 schema for them without a versioned contract decision. Reserve stdout for valid JSON in JSON mode, use stderr for human diagnostics, and map deterministic exit statuses 0/2/3/4/5/6 to original §5.1. Apply limits to untrusted subprocess output and avoid embedding raw ANSI/control sequences in JSON strings. Any JSON schema change is a contract review event with both positive and negative golden fixtures.

### 12.12 Persistence, cache and transactional writes

| Data | Proposed location / authority | Read/write policy |
|---|---|---|
| User config | `$XDG_CONFIG_HOME/nosey/config.json`, platform fallback | User-owned atomic validated write; authoritative for approvals and user registries |
| Workspace config | `<workspace>/.nosey/config.json` | Data only; untrusted registry references require user trust fingerprint |
| Trust approvals | User config or user-owned trust-store segment | Stable normalized workspace path + canonical registry settings fingerprint; revocation explicit |
| Git registry snapshots | `$XDG_CACHE_HOME/nosey/` content-addressed checkout | Last validated revision retained on fetch/schema failure; avoid partial activation |
| Effective projection/index | Derived content-addressed cache, optional | Rebuildable; never source-of-truth for edits/trust |
| Installation origins | `<workspace>/.nosey/install-origins.json` | Atomic attribution per logical origin + skill + agents; not installation proof |
| UI filters/selections | Process memory for current TUI session | Retained across tabs/rescans according to §4; not silently persisted |
| Logs and recent operations | Bounded stderr/current-session views | Secrets and path control chars scrubbed; no hidden analytics upload |

**Write pattern:** Build to private same-filesystem temp location, validate, fsync as appropriate, atomic rename for config/receipts/scaffold. For registry-init, reject destination collisions before creating files and rename a validated directory into the final requested path; optional `git init` and `registry add` follow successful move. Use per-cache-source locking to avoid torn checkout/metadata pointers when multiple Nosey processes refresh concurrently. Derive and document stale snapshot, cache cleanup and crash-recovery behavior; cache cleanup must not change logical installed identity.

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

Distribute a single CLI package with pinned runtime/dependency range and checked-in versioned Nosey JSON Schemas. The **official default registry remains remote**; the binary may contain its Git URL and schema readers but **must not bundle its registry data or scaffold templates** as fallback. Use reproducible builds and lockfile verification in CI, dependency audit, schema contract checks, platform/terminal smoke tests and a disposable project where real `skills@1.7.6` CLI invocations are exercised. CI must check `--skill` grouping grammar, ref syntax, add/list JSON schemas, agent translation, `SKILL.md` frontmatter, cache-backed local source and project-scoped install reconciliation. Releases must be blocked by the specific unverified assumptions recorded in §16 until tests succeed and owners sign off.

### 12.18 Architecture decisions and alternatives

| ADR | Recommended choice | Rejected/alternate option | Revisit condition |
|---|---|---|---|
| ADR-01 | Local modular monolith | Background daemon / microservices | Multi-user remote workflows are in approved scope |
| ADR-02 | TypeScript / Node.js 22.20+ | Python or Rust core | Evidence of integration, packaging or perf shortfall |
| ADR-03 | Git snapshot + file-backed cache | Hosted database / custom registry API | Hosted collaboration/marketplace is approved |
| ADR-04 | Schema-first pure merge + immutable effective index | Mutable ad hoc registry object | Data set/incremental computation proves bottleneck |
| ADR-05 | Three-valued detection and mapping | Boolean-only inference | Never: boolean conflates unknown with absent |
| ADR-06 | Pinned upstream installer via argv | Reimplement agent installer or `npx` as shell | Upstream contract incompatible or unsupported long-term |
| ADR-07 | Per-skill verified install outcomes | Exit-code-only success | Never: violates source AC and false-success gate |
| ADR-08 | Explicit workspace trust fingerprints | First-use auto-trust | Never in MVP due to workspace takeover risk |
| ADR-09 | Ink candidate behind TUI adapter | Terminal-specific coupling in core | Renderer decision after prototype accessibility test |

**Architecture approval conditions:** Confirm product owner and engineering lead accept the dependency direction, define supported platform and installer version matrix, and freeze approved schema references. Any change from this proposal is captured as a new ADR with impact on AC IDs and output contracts.


## 13. Failure states and expected behavior

| Condition | Behavior |
|---|---|
| No Git connection and cached registry available | Continue with stale snapshot; show last refreshed time |
| Initial Git fetch fails with no cache | Skip source, warn, offer retry or offline local registry |
| Template provider unreachable with no valid cached template | Fail registry init without writing output; show update/local-template-source guidance |
| Template version incompatible or traversal in template files | Reject template with precise source/path diagnostics, preserving destination |
| Malformed manifest or unsupported schema version | Reject snapshot, show precise file/path/error; retain previous valid cached version |
| Unknown detector adapter or unavailable ecosystem provider | If explicitly enabled/required, mark rule `not-evaluated` and scan incomplete; if deliberately inactive, exclude by mode and leave technology coverage unknown without failing an otherwise complete scan. Preserve other positive matches |
| MCP package detection not enabled | Leave MCP rules `not-evaluated` with `excluded-by-mode`, report coverage gaps, allow `complete=true` if all required evaluations finished, and never execute MCP commands |
| Unsupported technology document version | Reject the affected snapshot without changing a previously valid cache; show version and source |
| Missing skill reference / dependency cycle | Exclude or block affected recommendations and give explicit IDs |
| Broken local skill path / symlink escape | Block installation and show source and path error |
| Skills CLI absent/incompatible | Keep scan/search usable, show doctor/install help and commands |
| Agent selection ambiguous | Prompt for target agent(s); do not silently install to all |
| Install process fails | Keep per-skill results, offer retry and copy log; no claim of success |
| Not a writable project | Allow detection and browsing; block execution with an actionable message |
| Very small/unsupported terminal | Compact layout or informative error; restore terminal safely |

### 13.1 Skills and mapping schema v2 invariants (normative)

- These v2 source documents support partial overlays; their **effective** counterparts are explicitly versioned and require all active fields. An unsupported source document version fails, without silent conversion. The shipped official registry and remote scaffold templates must be authored using skill/mapping schema version 2. No backward compatibility or migration tooling is specified for pre-release v1 skill/mapping documents.
- Semantic checks operate on the effective catalog, not merely independently valid registry fragments. Intra-document duplicate IDs, dangling required skills, source/repository mismatch, requirement cycles and asymmetric conflicts are diagnosed as defined in §6.
- Registry provenance for source and ratings is separately visible in Details and Install; authored `assessment` is optional metadata, not a verified certification.
- Scan's machine-readable output contains both `recommendedSkills` (confirmed) and `unevaluatedRecommendations` (unknown condition/anchor), preserving `complete` scan diagnostics. No unevaluated entry is silently called recommended. Filtering and selection never change these underlying states.

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

**AC-23:** Scan provides keyboard-accessible `f` filtering and `s` sorting for **both** the detected-technology section and recommended-skills section. The active section is explicit; `Esc` cancels edits, and `Enter` applies them. Scan displays visible/total counts; `Clear filters` restores all rows.

**AC-24:** Technology sorting supports alphabetical ascending/descending, evidence count, and matched rule count. Recommendation sorting supports priority, score, quality, repository average, and name. Filter combinations and all sorting modes have stable tie-breakers, preserve detection/recommendation data, and produce reproducible results without rescanning.

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

**AC-50:** Footer never exceeds six bindings; Scan technology pane has a documented reverse-focus action and Tab exits text search to switch top-level section.

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

## 15. Delivery scope and order

**MVP (must-have):** Remote-registry-sourced `nosey registry init` (empty and workspace templates, Git/local template providers, cache, output validation, optional Git initialization and local registration) with no bundled template archive, full four-tab TUI, automatic scan, version-dispatched v1/v2 technology documents, source-verified v2 catalog compatibility, built-in file/dependency/content/extension/dotenv detection with safe glob/regex matchers, opt-in MCP package inspection, capability reporting and additional ecosystem observation providers, extensible trusted-detector interface, JSON manifests/mappings/skills and optional repositories metadata, quality/score and repository averages, deterministic field merge, Git/local registries, cache + TTL, recommendations with conditions/dependencies/conflicts, technology → repository grouped Explore search with filters and sorts, separately filterable/sortable Scan technologies and recommended skills, multi-select, project-only installer/copy commands/dry-run/logs, Settings TUI, scriptable scan/search/install/list/registry commands, validation, and robust errors.

**Post-MVP:** Rich plugin detector ecosystem, upstream full-text search integration if a supported stable API emerges, saved filters/search presets and advanced query operators, skill update/removal workflows, collaboration and registry publishing tooling, dependency resolver suggestions, metrics, automatic registry test harnesses, and broader package ecosystem parsers.

**Suggested milestones:** (1) schemas + registry loader/merger + validation design; (2) detection/mapping engines; (3) Skills CLI adapter + shell CLI; (4) TUI/keyboard/focus; (5) offline/security/compatibility and release hardening.

### v1.7.1 MVP milestone coverage

1. **M1 — registry foundation and trust:** Freeze the machine-output and completeness contracts, official registry manifest/schemas, version-aware technology loader (v1/v2), layered sources, cache/TTL, workspace approval fingerprints, source hardening and drift warnings. Upstream source-parser contract approval precedes adapter implementation.
2. **M2 — detection and adapters:** Seven vocabulary kinds (including opt-in `mcp_package`), all specified ecosystem observation providers, required/excluded rule coverage accounting, `metadataOnly` handling, monorepo evidence, pinned out-of-process trusted adapters and safety limits. No catalog rule revalidation.
3. **M3 — recommendation and browsing:** Skills/mappings v2 effective validation, three-state condition evaluation, independent recommendation relevance evaluation, rating provenance, catalog/mapping requirements, conditional union and cycle checking, score/quality display, Scan/Explore filters/sorts, grouping, installed `unknown` state.
4. **M4 — installation integration:** Pinned CLI runner and version-specific compatibility proof, approved agent mapping, tested argv segmentation, JSON add/list parsing, source grouping, stable logical origin and project attribution, dry-run, telemetry disclosure, per-skill post-verification and result attribution.
5. **M5 — registry init and settings:** Remote official templates, `empty`/`workspace` generator, sourceRevision provenance and no vendored skill payloads, Settings trust approval and TTL controls, coverage/source integration verification and official-score publication governance.

No MVP item is deferred silently. Unverified upstream behavior remains a release blocker, not a presumed implemented feature.


## 16. Remaining implementation decisions

- Official default registry Git URL, governance, template maintenance ownership, and publishing the supplied verified technology v2 catalog in that external registry (to be configured before shipping).
- Exact TypeScript TUI renderer/library (Ink is a candidate).
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



## 18. Implementation-ready requirement inventory and traceability

The original **AC-01 through AC-72** in §14 remain the detailed acceptance source. This inventory groups them by architectural workstream and makes their primary verification modality explicit; it does **not** renumber or replace them.

| Requirement domain | Primary acceptance set | Accountable component | Evidence needed before sign-off |
|---|---|---|---|
| Navigation, terminal controls and accessibility | AC-01, AC-02; relevant TUI criteria across §14 | TUI shell and session state | Keyboard-only TUI journey recording, narrow terminal/NO_COLOR/signal tests |
| Technology schema versioning and detection evidence | AC-03, AC-04, AC-59–AC-61, AC-69 | Technology decoder, observation providers, coverage aggregator | v1/v2 source fixture, seven-rule-provider corpus, truth-table and incomplete-scan tests |
| Merge, overlays, precedence, trust | AC-05, AC-06, AC-62–AC-65, AC-71 | Source loader, schema validator, merger, trust policy | Partial-overlay/snapshot invalidation tests, field provenance checks |
| Recommendation and search semantics | AC-07, AC-08, AC-64, AC-67 and filtering criteria in §14 | Three-state recommendation evaluator, index/comparators | Property-based mapping tests, filters/ordering, cross-technology dedup |
| Skill selection, installation and identity | AC-09, AC-10, AC-66, AC-70 plus remaining installer ACs | Install planner, Skills CLI adapter, inventory reader | Pinned upstream golden fixtures + real disposable-project verification |
| Registry templates and initialization | AC-68 plus registry-init ACs in §14 | Template provider, generator, atomic writer | Offline/pinned snapshot tests, traversal, collision and scaffold provenance |
| Registry quality and evaluation | AC-72 plus scoring ACs in §14 | Registry metadata, editorial process | Authored-score provenance and independent recommendation quality study |
| Settings, user trust and diagnosis | AC-71 plus settings/doctor ACs in §14 | Settings store, trust store and doctor | Changed-fingerprint, non-TTY, source mandatory/optional exit tests |

**Traceability protocol (proposed):** Each engineering ticket carries an AC ID, affected schema filenames, one or more tests and an owner. Every intentional requirement change must update the source PRD, the schema if affected, matching positive/negative fixtures, README/version notes and release-gate checklist. No requirement is considered implemented because a type/schema file exists.

## 19. Cross-cutting nonfunctional requirements and verification plan

| ID | Requirement / measurable rule | Status | Verification |
|---|---|---|---|
| NFR-01 | No executable installation without deliberate confirmation (`--yes` for non-TTY); project-scope-only agent selection | Source normative | Fake-runner negative tests and upstream project install e2e |
| NFR-02 | All four published JSON command formats validate against checked-in output v2 schemas; documented semantic invariants pass | Source normative | Golden serialization tests and cross-field property checks |
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

**Contract tests** validate every supplied JSON Schema plus source-vs-effective overlay semantics, closed objects and enum values, duplicate-ID semantic checks, `outputVersion=2`, full default filters, cross-record counts, grouping uniqueness and aligned command item arrays. Test data must be newly authored; the supplied package contains no executable fixture/test harness. Do not rewrite the reference 894-technology catalog to make tests pass.

**Integration tests** use a fake source provider, fake process runner, failing cache/store and bounded filesystem fixtures for offline/stale behavior, cancellation, immutable-snapshot promotion, user/workspace trust gate, registry-init collision/symlink cases, source compatibility, partial add output and unknown installed identity. Then run real pinned `skills@1.7.6` checks in disposable projects with several agent identities, Git refs, local cached skill directories, and both successful and failed installs.

**System tests** cover launch-to-Scan, Explore grouping/selection, hidden selections, Install confirmation/Cancel/Dry-run/Print/Execute, Settings registry trust approval, New Registry wizard, registry CLI, doctor, strict vs non-strict incomplete scan, noninteractive exit behavior and terminal cleanup under SIGINT. Reconcile expected results with AC-01–AC-72 individually.

**Security tests** include malicious Git URI variations, argv injection, ref traversal, symlink escape, unbounded file/regex/MCP read, untrusted ANSI strings, secret redaction, project self-approval attempt, adapter hash mismatch and bounded child output. Negative tests must assert **no installer invocation** on preflight failure.

### 20.2 Release gates (must have owners and recorded evidence)

| Gate | Exit evidence | Blocker if absent |
|---|---|---|
| G1 — Source and public contract freeze | Approved 16 schemas, v1.7.1 product decisions, version table, full source/effective semantic test suite | Engineering cannot assume inputs/outputs are final |
| G2 — Trusted registry publication | Official Git registry URL; maintainer; signed-off template IDs, source SHA, v2 technology catalog publication and recovery plan | No safe default data source/init provider |
| G3 — Detection and recommendation correctness | Seven rule kinds and ecosystem provider capabilities demonstrated; completeness semantics and unknown-safe evaluation; independently reviewed corpus protocol | Cannot promise positive/negative recommendations |
| G4 — Upstream CLI compatibility | Actual v1.7.6 argv/ref tests, add/list JSON outcomes, display-name ↔ ID mapping, source/lockfile/cache-origin reconciliation in disposable project | Install cannot be signed off on parser inspection alone |
| G5 — Installation safety and recovery | Project-only safeguards, per-skill verification, partial failures, cancellation, no false success | Release safety failure |
| G6 — UX, platforms and performance | Supported OS/terminal matrix; keyboard and narrow-layout acceptance; named p95 fixture/runner results; safe exit/NO_COLOR | Product and operational incompleteness |
| G7 — Editorial and launch usefulness | Curated-score rubric/owner/correction policy; labeled recommendation precision@3 and applicability coverage studies, usability task completion | Curated official ratings and usefulness claims must not be published |

### 20.3 Baseline metrics protocol (proposed)

Treat ≥95% technology ID correctness, ≥80% recommendation precision@3, ≥80% of applicable workspaces receiving actionable recommendations, ≥90% unaided study completion and warm-cache p95 <2s as **provisional targets**, not demonstrated outcomes. Before measuring, approve the sampling frame, corpus version/SHA, independence of human labels, definition of an applicable workspace and actionable skill, top-3 ranking tie rule, sample sizes, confidence intervals and exclusions. Report technology-state false positives **and** unknown coverage separately; do not calculate success by dropping unknown cases invisibly. Record terminal width and OS for usability, and hardware/file-tree characteristics for performance. No runtime product telemetry is required to conduct a controlled pre-release evaluation.

## 21. Delivery plan, dependency graph and handoff

Phases follow the full MVP scope of §15; these are **sequence recommendations**, not time or staffing commitments. Estimate only after G1/G2 and installer evidence reduce uncertainty.

| Phase | Deliverable / vertical slice | Prerequisite | Definition of done |
|---|---|---|---|
| P0 — contractual spike | Freeze version map; inspect/test pinned upstream; specify supported OS and official registry | Source package available | Gate G1 plus documented G4 findings and architecture ADR approval |
| P1 — registry core | Git/local source, cache, trust fingerprint, schema decode, effective merge, provenance, doctor diagnostics | P0 | Invalid snapshot cannot displace valid; editable overrides correct |
| P2 — detector | Workspace boundary discovery, seven rule kinds with providers, MCP opt-in, adapter trust and coverage state | P1 | v2 894-ID reference loads unchanged; rule-state and coverage truth tables pass |
| P3 — recommendation/search | skills/mappings v2, conditional engine, repository scores, ranking, Explore index and JSON scan/search | P2 | No false recommendation from unknown; deterministic query/filter/grouper |
| P4 — project installation | Source/ref/agent preflight, plan/dry-run/print, upstream runner, installed reader, origin receipt | P0, P1, P3 | Reconciled real per-skill project results and failure recovery; G4/G5 |
| P5 — TUI and settings | Four tabs, keyboard focus, dynamic footer, modes, filtered selections, registry trust and init wizard | P1–P4 | AC-aligned keyboard journeys, accessibility and narrow-terminal checks |
| P6 — release hardening | Full OS/terminal/test matrix, performance, docs, governance and recommendation studies | P1–P5 | G1–G7 all passed and sign-off logged |

**First end-to-end implementation slice:** Run `nosey scan --json` using a trusted local example registry, detect Next.js and TypeScript in a small synthetic project, emit safe rule evidence, confirm one eligible mapping, and validate the output v2 schema. Next prove an installation **dry-run** with source/skill/agent/identity only, then a real disposable-project adapter test before enabling project write actions. This limits integration risk without reducing eventual MVP scope.

**Handoff checklist:** Each phase passes API/type declarations, schema references, tested decisions, fixture inventory, diagnostics, security-relevant trust boundaries, integration behavior, rollback/repair limitations and owner sign-off to the next phase. Implementation code and tests are not included in the supplied planning archive and have not been created or executed by this expanded PRD.

## 22. Risk register and unresolved decision owners

| Risk / open issue | Likelihood × impact (qualitative) | Mitigation | Proposed owner / gate |
|---|---|---|---|
| v1.7.6 CLI output or multi-skill grammar differs from source interpretation | High × Critical until tested | Tagged-version real e2e with disposable agent targets; fallback to verified one-skill invocations | Integration lead / G4 |
| Cached local skill installs cannot be reconciled with upstream lockfile/source | High × High until tested | Separate logical and execution source; origin receipts plus physical project inventory | Integration lead / G4–G5 |
| Official default registry URL/template publication not decided | Certain × Critical | Assign URL, maintainer, immutable SHA release process and offline bootstrap policy | Product/registry owner / G2 |
| Source overlay can combine misleading source/repo/score provenance | Medium × High | Atomic source fields, explicit overrides, effective repository match and author labels | Domain + security / G1,G5 |
| Disabled MCP/provider creates false negative technology inference | Medium × High | Required/excluded modes, Kleene conditions, `unknown` and coverage gaps | Detection lead / G3 |
| Hostile Git paths, template symlinks or argv tokens trigger unintended effects | Medium × Critical | Canonical path checks, hardened Git subprocess, fuzz/negative security tests | Security + platform / G5 |
| Performance target is unspecifiable without fixture/runner | Certain × Medium | Agree reproducible corpus/hardware and separate refresh/cache measurement | QA/performance / G6 |
| Official scores interpreted as certification or objectively measured quality | Medium × High | Source attribution, rubric, independent correction/reassessment process | Registry governance / G7 |
| Complex TUI grouping and hidden selections create unreviewed install set | Medium × High | Total vs visible counts, confirmation shows hidden selections, keyboard journeys | Product/UX / G6 |
| Schema files are mistaken for executable implementation evidence | Medium × High | Distinct planning and tested gates; reject unsupported success claims | Release manager / G1–G7 |

### Required approvals before implementation and launch

- **Product owner:** confirm MVP scope, target users and exact wording of recommendations with uncertain coverage; define study dataset/acceptance thresholds.
- **Engineering lead:** approve Node/TypeScript distribution design, TUI library after spike, supported platforms, performance budgets and cache concurrency design.
- **CLI integration lead:** approve real upstream compatibility and per-skill identity reconciliation, or formally block install execution.
- **Security owner:** review source/registry/trust controls, untrusted input boundaries, adapter policy and no-global guarantee.
- **Registry maintainer:** publish official URL/registry/template revision, scoring editorial policy and rollback procedures.
- **QA/release owner:** map all 72 existing acceptance criteria to executable evidence and attest release gates.

## 23. Glossary and contract index

**Catalog skill ID:** Nosey-wide logical ID used by registry merge, selection and mappings (e.g. `vercel:web-design-guidelines`). **Upstream skill name:** `source.skillName`, matching `SKILL.md` frontmatter and CLI `--skill`. **Source registry:** Git/local Nosey metadata provider. **Skill repository:** Git/local repository from which skills are installed. **Requested ref:** User-declared Git ref, not necessarily a resolved commit. **Snapshot revision:** Actual validated Git commit SHA. **Logical origin:** Stable installer identity independent of cache checkout location. **Execution source:** Actual path/URI passed to upstream CLI. **Installed unknown:** Reader cannot prove installed or missing at the requested project/agent scope. **Execution complete:** All required detection rules evaluated, not necessarily complete technology knowledge. **Unevaluated recommendation:** Mapping gate contains unknown detection and cannot yet be confirmed. **Author-assessed score:** Source registry's subjective metadata, not measured detection confidence.

### Package-backed contract files

- `nosey-spec/PRD.md` — original 17-section v1.7.1 normative planning specification with AC-01–AC-72.
- `nosey-spec/schemas/registry.schema.json`, `technologies.schema.json`, `technologies.v2.schema.json`, `repositories.schema.json` — registry/technology/repository data.
- `nosey-spec/schemas/skills.schema.json`, `skills.effective.schema.json`, `mappings.schema.json`, `mappings.effective.schema.json` — source overlays and effective objects.
- `nosey-spec/schemas/settings.schema.json`, `template.schema.json`, `scaffold.schema.json`, `install-origins.schema.json` — local configuration, template and origin attribution.
- `nosey-spec/schemas/scan-output.schema.json`, `search-output.schema.json`, `install-plan-output.schema.json`, `registry-init-output.schema.json` — command-output v2 public JSON envelopes.
- `nosey-spec/example-registry/` — planning examples only, not a fixture test suite.
- `nosey-spec/reference/technology-registry-v2/technologies-894-v2.json` — unchanged reference source catalog; not a runtime-embedded default.

**External upstream reading (identified by the original PRD, not revalidated here):** <https://github.com/vercel-labs/skills>, <https://github.com/vercel-labs/skills/blob/v1.7.6/src/source-parser.ts>, <https://github.com/vercel-labs/skills/blob/v1.7.6/src/cli.ts>. The integration behavior described in §§8–10 remains subject to disposable-workspace execution tests.

---

**End of expanded planning specification.** No implementation, install, validator execution, recommendation evaluation or live upstream compatibility certification is implied by this document.
