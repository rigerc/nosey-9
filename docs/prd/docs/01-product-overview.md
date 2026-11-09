# Product overview & success

[← Specification index](../README.md) · [Previous](00-governance.md) · [Next](02-experience-and-tui.md)

## 1. Executive summary

Nosey is a terminal-first CLI/TUI application for individual developers who work with AI coding agents. It inspects the current workspace, detects technologies using externally defined rules, recommends relevant agent skills using a separate externally defined mapping, and installs only user-selected skills through Vercel's `skills` CLI. Users can also browse a merged registry manually, install skills that were not recommended, and initialize from externally hosted templates and create Git-backed or local custom registries containing detection rules, skill mappings, references to external skill repositories, local `SKILL.md` skills, skill quality/score metadata, and authored repository-level average scores in the manifest. The registry manifest uses schemaVersion 1, verified technology documents use version 2, and the newly designed skill/mapping documents use version 2. These document version numbers are independent.

Running `nosey` launches a single-column, keyboard-driven TUI that immediately scans the current workspace. The persistent header displays the logo and `Scan · Explore · Manage · Settings` menu; Tab cycles menu sections. A persistent footer lists context-sensitive keybindings. Shell-friendly subcommands expose the same core operations for scripts and CI.

The upstream Skills CLI provides `find`, `check`, `update` and experimental sync. Nosey adds detection, context-specific mapping, layered registries and quality/score metadata, and installs, checks and (in a later release) updates and removes skills itself.

**Core product principle:** Nosey owns *discovery, recommendation, curation, and installation*; its installs interoperate with Vercel's Skills CLI (`skills@1.7.6` layout and `skills-lock.json`) but do not depend on it ([D9](17-approved-decisions.md#d9--native-installation-engine)).

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
