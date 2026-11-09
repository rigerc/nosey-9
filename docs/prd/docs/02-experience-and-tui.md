# Workflows & TUI

[← Specification index](../README.md) · [Previous](01-product-overview.md) · [Next](03-cli-contract.md)

## 3. User stories and workflows

**US-01 / auto scan:** As a developer, running `nosey` scans the current workspace and displays technology matches and related skills. The TUI lists detected technology names in a read-only strip; `nosey scan --json` provides each technology's matching rule ID, kind, project scope, and evidence path(s) without exposing sensitive values. I can sort and filter the recommended-skills list in the TUI, and the detected-technologies list through `nosey scan` flags, without rescanning or losing skill selections.

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
6. Render Scan with all skills unchecked, a read-only detected-technologies strip, and filter/sort state for recommended skills.
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
│ Detected technologies  2                                             │
│ ╭──────────────────────────────────────────────────────────────────╮ │
│ │ Next.js · TypeScript                                             │ │
│ ╰──────────────────────────────────────────────────────────────────╯ │
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
- In Scan and Explore, `←` collapses the focused technology or repository group, and `→` expands it. `Enter` on a group heading toggles its expansion. Collapsing does not clear any selected skills.
- `Enter`: open the active row's details or commit an edit; in Scan/Explore it does **not** toggle selection (Space does).
- `Space`: toggle an active skill's checkbox; in Settings, toggle a checkbox/switch.
- `/`: focus the view's search input (Scan or Explore). While typing, keyboard characters type text; `Esc` exits input and preserves the query.
- In both Scan and Explore, `f` opens filter controls and `s` opens sorting controls. In Scan, these dialogs target Recommended skills; the detected-technologies strip is not filterable or sortable in the TUI. These actions are disabled while typing and are supported from keyboard-only navigation.
- An active confirmation or other modal traps keyboard input; `Tab`/`Shift+Tab` navigate dialog controls rather than changing top-level sections. `Esc` cancels the modal.
- `?`: open keybinding help. `q` quits from non-input, non-modal contexts.
- Preserve in-memory view state (queries, selections, cursor) while changing tabs in the same session.

### Screen requirements

**Scan** (default): Auto-run detection on launch; a read-only, non-focusable strip of detected technology names, truncated to one line, with `t` opening the full list; recommended skill rows with name, priority, provenance, installation status, optional quality/score/repository average and reason; `r` rescan, `Space` select, `a` select/deselect all **visible eligible** skills, `i` review install, `/` focus live search, `f` filter, and `s` sort. Initially, nothing is selected. Recommended skills are the only focusable Scan section. Rule IDs and evidence paths are available through `nosey scan --json`, not in the TUI. A 'No technologies found' state still links to Explore. Filtering and sorting are mandatory MVP functionality in Scan, not limited to Explore.

**Scan filters and sorting:** The TUI exposes filters and sort orders for recommended skills. Detected-technology filters and sort orders are CLI-only (`nosey scan`). All normative facet combinations, ranking, null handling, and tie breakers are defined in **Canonical filtering and sorting contract** below. Filtering never re-runs detection and never changes which skills were recommended.

**Scan selection, counts, and state:** Filtered-out skills remain selected; the count distinguishes `N selected total` from `M selected visible` when they differ. Install confirmation includes all selected skills, including hidden selections, and explicitly notes that some are hidden by active filters. `a` acts on **visible** eligible skills only, while `i` includes the full selection. Clearing filters restores hidden rows and does not reset checkboxes. `r` rescan updates matches/recommendations but retains filter/sort preferences during the session; selections for skills no longer available are removed with a visible notice. Scan and Explore have separate filter/sort state, kept when switching tabs. Empty states differentiate no detections, no applicable recommendations, and all results hidden by filters, with an accessible `Clear filters` action. Filter/sort dialogs show a preview count, apply/cancel semantics and no surprise modification of the underlying scan. Recommended skills remain selectable even if a different section is focused by moving the cursor to that section.

**Explore:** Live (debounced) search of the *merged registry's* skill names, descriptions, IDs, tags, associated technologies, and source repository names. The result tree is always **Technology → Repository → Skill**. Technology headings show visible skill counts; repository headings show the repository name, visible count, and average score when available. Use `↑↓` to navigate headings and skills, `←→` or `Enter` to collapse/expand groups, `Space` to select skills, `f` to edit filters, `s` to choose sort order, and `i` to review installation. All results use the effective merged registry and omit disabled/invalid entities. For arbitrary upstream searching beyond configured registries, `nosey search --upstream` may hand off to `skills find`, but Nosey must never scrape its interactive output as a stable API.

**Explore filters and sorting:** Filtering and sorting use the **Canonical filtering and sorting contract** below, preserving the technology → repository → skill hierarchy.

**Grouping and deduplication:** A skill is associated with the union of its `skills[].technologies` and mapping `technologyId` references, so it may appear in multiple technology groups. It appears once in each applicable technology/repository subgroup, with a single canonical selection state keyed by `skill.id`. The selected count and installation plan count **unique** skill IDs, not visible rows. Skills with no technology association appear under `Other / Uncategorized`. A skill belongs to exactly one effective repository: explicit `repositoryId`, or a source-derived repository identity where omitted. If no catalog entry exists, a virtual repository is generated from the Git source or originating local Nosey registry. The source *registry* remains a filter and provenance field, and is not confused with the source skill *repository*.

**Explore empty states:** No query matches; filters exclude all results; no configured registries; repository has no scored skills. These states must be distinguishable and allow clearing filters or navigating to Settings.

**Manage:** Read-only installed skill inventory for project scope with agent/source/status where the underlying CLI exposes them, recent operation results from current session, an `r` refresh shortcut, and a `u` read-only update check that asks the pinned upstream CLI which installed skills have updates and labels them `update available`. The result reports only what upstream compared: skills it could not compare or check are shown as a gap and never as up to date. Installing updates and removal are explicitly out of MVP. Never conflate global inventory with project inventory.

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
| Scan / detected technologies (CLI only) | Technology name/ID and case-insensitive query over matched rule IDs and safe evidence paths, explicit technology ID, **matched** rule kind | `name-asc` (default), `name-desc`, `evidence-count-desc` (distinct evidence paths), `matched-rules-desc` (distinct rule IDs) |
| Scan / recommended skills | Name/ID, description, tags, reasons, source repository text; technology, source registry, repository ID, mapping priority, status (`installed`, `missing`, `unknown`), quality/unassessed plus scored/unscored, min score, min authored repository average | `priority-desc` (default), `score-desc`, `quality-desc`, `repository-average-desc`, `name-asc`; flat skill list |
| Explore / all skills | Search across name/ID/description/tags/technology/repository; source registry, technology, repository, installation status, quality/unassessed plus scored/unscored, min score, min authored repository average | `relevance` (default), `score-desc`, `quality-desc`, `repository-average-desc`, `name-asc`; **always** retains technology → repository → skill tree |

**Text relevance (with query):** Rank exact ID or name, then name prefix, name substring, tags, description, technologies, and repository name; ties fall back to passing mapping priority (`core` > `recommended` > `optional` > unmapped), numeric score descending null-last, normalized name, then stable skill ID. With no query, retain default priority ordering. Quality order: `excellent` > `good` > `fair` > `poor` > unassessed. Numeric score and authored repository average compare descending with missing last; all ties use normalized name and stable ID. Technology headings order detected-workspace technologies before other alphabetical technologies, with `Other / Uncategorized` last; repository headings sort alphabetical by name except for `repository-average-desc`, which sorts authored average descending then name. Within repository groups sort skills by chosen comparator (`repository-average-desc` uses skill score as secondary comparator). Group collapse preserves selected IDs; group counts represent visible rows, selected counts distinct IDs.

**Facet independence:** Scan technology-list facets never implicitly hide recommended skills; recommendation facet `technology`/CLI `--rec-technology` explicitly narrows the recommendations only. CLI `--technology` narrows the detected list. `--query-target` chooses technologies, skills, or both. Scan and Explore each preserve their own sort/filter state through tab changes and rescans. Queries do not change registry content. A filter yielding no rows produces a `Clear filters` affordance, not an empty detection result.

### Dynamic footer keybinding specification

| Context | Visible footer bindings |
|---|---|
| Scan | `↑↓ Move` · `Space Select` · `F Filter` · `S Sort` · `I Install` · `Tab Section` |
| Scan (filter/sort modal) | `↑↓ Navigate` · `Space Toggle` · `Enter Apply` · `Esc Cancel` |
| Explore | `/ Search` · `F Filters` · `S Sort` · `↑↓ Move` · `Space Select` · `I Install` |
| Explore (group focused) | `← Collapse` · `→ Expand` · `Enter Toggle` · `F Filters` · `S Sort` |
| Explore (filter/sort modal) | `↑↓ Navigate` · `Space Toggle` · `Enter Apply` · `Esc Cancel` |
| Manage | `↑↓ Move` · `R Refresh inventory` · `U Check updates` · `? Help` · `Tab Section` · `Q Quit` |
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
