# Governance & decisions

[← Specification index](../README.md) · [Next](01-product-overview.md)

## 0. Document governance and decision summary

### 0.1 Purpose and intended readers

This document is a consolidated engineering-ready **planning** specification for the terminal-first Nosey developer tool. It preserves the source PRD's TUI/CLI functionality, registry and schema contracts, detection semantics, installer safeguards and 72 original acceptance criteria. It adds implementable module boundaries, data flow and trust boundaries, data and API interface specifications, system quality requirements, dependency decisions, critical sequences, phase-level verification, a risk register and release decision gates. Intended readers: product owner, engineering lead, CLI/TUI engineers, test engineers and maintainers of the official external registry.

### 0.2 Product decision in one paragraph

Nosey is a **local-first Go CLI/TUI built from the `rigerc/clackbt` starter**, not a hosted SaaS application. Its core responsibility is detection, explanation, registry composition, recommendations and confirmation-based installation orchestration. A Git-hosted official registry and optional user/workspace registries provide **data**, not executable plugins. The separate pinned Vercel `skills` CLI performs actual skill installations. Nosey maintains no account service and should not own skill update/removal in MVP. The proposed runtime is a **single compiled Go executable with modular internal packages**, local validated file caches and tightly controlled subprocess adapters. Go and the starter are user-selected decisions; actual Nosey implementation and compatibility tests remain outstanding.

### 0.3 Decision status and authority

| Decision | Status | Basis / constraint |
|---|---|---|
| Four-tab one-column TUI and scriptable CLI | Source normative | Sections 3–5; original AC-01–AC-72 |
| Technology v1/v2, skill/mapping v2; scan/search/init v2, install-plan v3, list/registry-list/doctor v1 | Source baseline + approved D4 amendment | Original 16 schemas retained; four new specification schemas under `../schemas/` |
| 894 identities / 3,637 rules; source dataset unchanged | Source-provided asset | Reference technology v2 document; do not make it a built-in runtime registry |
| Official remote Git registry, no embedded scaffold | Source normative + user-requested delivery addition | Create a separate default data repository and pinned development submodule through P1R; owner/URL still unresolved; see §6 |
| Local modular monolith and filesystem cache | **Proposed design** | Reduces deploy and transaction complexity; no server requirement |
| Go with `rigerc/clackbt` (Cobra / Bubble Tea v2 / bundled Clack prompts) | **User-selected implementation decision** | Overrides source-recommended TypeScript / Ink stack; template inspected read-only |
| Pinned upstream `skills@1.7.6` baseline | Unverified integration proposal | Tagged-source interpretation is not execution proof |
| No product telemetry; upstream inherits env by default | Source normative | §10; disabled mode sets telemetry opt-out variables |
| No global install or automatic remediation | Source normative | Non-goals and installation preflight |

### 0.4 Confirmed, proposed and unknown

**Confirmed in the package:** planning documents exist; schemas and sample registry are present; the source reference reports 894 technology IDs, 3,637 authored rules and 19 metadata-only IDs; 72 product acceptance criteria are recorded. **Confirmed implementation direction (user):** Go and `rigerc/clackbt`; **template facts verified by read-only inspection:** Cobra, Bubble Tea v2, bundled Clack prompts, setup script, Go version in `go.mod`. **Proposed:** Nosey integration architecture, processes, filesystem layout, interface layering and detailed validation/test approach in §§12.1–12.18 and §§18–22. **Unknown/blocking:** real v1.7.6 install/list JSON result behavior, grouped `--skill` argv grammar in disposable workspaces, agent identity mapping, cache/lockfile attribution, official registry URL/governance, supported platform matrix, hardware performance benchmark and final scoring study. A checked-in schema is not an executed contract test.

### 0.5 Scope boundary

**In MVP:** Scan, Explore, Manage inventory, Settings, data-only Git/local registry overlays, external registry-init templates, v1/v2 technology decoding, all seven detection-rule kinds subject to documented opt-in/capability semantics, three-state recommendations, search and score facets, project-scope installation planning and verification, offline cache and trust review. **Outside MVP:** account/auth backend, hosted registry API, server database, universal detector execution from registry JSON, global skills management, autonomous skill installation, updater/remover, and complete upstream search mirroring. Keep all required MVP functionality from §15; the architecture must not silently defer it.

### 0.6 Requirement language and change control

The source PRD's **MUST**, **normative** and AC requirements are the preserved baseline; the newer user-approved decisions in [D1–D7](17-approved-decisions.md) override conflicting planning text and version the affected output contract. Newly introduced implementation choices are prefixed **Proposed design** or recorded as an ADR. If an implementation choice conflicts with the source schemas or normative text, the source prevails until an explicit product change is approved, versioned and traced through affected schemas, fixtures, CLI output snapshots and acceptance tests. Scope is a **project directory**, distinct from a user's global skill inventory. Missing scores and unknown states must remain distinguishable from numerical zero and verified absence.

### 0.7 Local integration update — draft.3

The newer Go multifile PRD supersedes the previous consolidated TypeScript architecture. Its phase roadmap and the repository-specific [implementation plan](../IMPLEMENTATION_PLAN.md) are reconciled; the latter retains detailed tests, rollback and the early local CLI slice. The requested default registry repository/submodule is an explicit delivery requirement (P1R), without changing public schemas or AC-01–AC-72. Git transport still treats fetched registry submodules as disabled; the known app development submodule is a separate checkout concern.

The imported draft's read-only upstream inspection statements describe its author's evidence. Separately, this workspace was inspected locally and its unchanged starter passed tests/build during the earlier handoff. No Nosey feature, official registry repository, submodule, upstream installation or release has been implemented by this documentation update.

### 0.8 Approved gap resolutions — draft.4

See [D1–D7](17-approved-decisions.md) for the user-selected decisions and their acceptance requirements. They resolve the registry milestone cycle, durable install-source storage, review-bound approval, all advertised JSON outputs/config commands, exact workspace boundaries and partial effective invalidation. The initial registry imports pinned `rigerc/skills-catalog` authoring inputs through explicit reviewed technology and skill ID maps. Original source documents/schemas remain unchanged. New schemas were authored as specifications; no application/installer/repository/submodule implementation is implied.

---
