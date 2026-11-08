# Failure states

[← Specification index](../README.md) · [Previous](09c-architecture-quality-and-decisions.md) · [Next](11-acceptance-criteria.md)

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
