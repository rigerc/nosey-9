# Nosey — Product Requirements & Go Technical Architecture

**Product baseline:** v1.7.1 (original normative requirements preserved). **Architecture revision:** `1.7.1-go-clackbt-draft.4` (2026-11-07). **Implementation stack chosen by user:** Go using the private [`rigerc/clackbt`](https://github.com/rigerc/clackbt) starter (Cobra, Bubble Tea v2, bundled Clack prompts). **Status:** planning, not implemented or tested.

## Read this first

- [Approved gap resolutions and catalog import](docs/17-approved-decisions.md): persistent install sources, reviewed-plan approval, JSON versions, scan boundaries and overlay failure semantics.
- [Decisions and document authority](docs/00-governance.md): what is normative, assumed, proposed or unresolved.
- [Go / ClackBT implementation map](docs/go-clackbt-integration.md): verified starter components, bootstrap, package ownership, build and quality gates.
- [Architecture foundation](docs/09-architecture-foundation.md), [data flows](docs/09b-architecture-data-and-flows.md), and [quality / ADRs](docs/09c-architecture-quality-and-decisions.md).
- [Skills and mappings import plan](CATALOG_IMPORT_PLAN.md): conversion of the already verified skills and updated mappings into the official registry; frontend bonus excluded.
- [Repository implementation plan and comparison](IMPLEMENTATION_PLAN.md): detailed execution milestones, mapping to PRD phases, and reconciliation with the earlier plan.
- [Default registry repository and submodule](docs/04-registry-contracts.md#default-registry-repository-and-development-submodule): required data-repository bootstrap and development checkout policy.
- [Original acceptance criteria AC-01–AC-72](docs/11-acceptance-criteria.md) and [release gates](docs/14-nonfunctional-and-validation.md).

## Specification index

- [§0 — Governance & decisions](docs/00-governance.md)
- [§1,2 — Product overview & success](docs/01-product-overview.md)
- [§3,4 — Workflows & TUI](docs/02-experience-and-tui.md)
- [§5 — CLI and machine outputs](docs/03-cli-contract.md)
- [§6 — Registry data contracts](docs/04-registry-contracts.md)
- [§7,8 — Registry merging, fetching & trust](docs/05-registry-trust-merge.md)
- [§9 — Detection & recommendation](docs/06-detection-and-mapping.md)
- [§10 — Install orchestration](docs/07-installation.md)
- [§11 — Settings](docs/08-settings.md)
- [§12.1,2,3,4 — Architecture: foundation, trust & Go packages](docs/09-architecture-foundation.md)
- [§12.5,6,7,8,9,10,11,12 — Architecture: data, workflows & contracts](docs/09b-architecture-data-and-flows.md)
- [§12.13,14,15,16,17,18 — Architecture: security, reliability & ADRs](docs/09c-architecture-quality-and-decisions.md)
- [§13 — Failure states](docs/10-errors.md)
- [§14 — Acceptance criteria](docs/11-acceptance-criteria.md)
- [§15,16,17 — Scope, decisions & upstream](docs/12-scope-and-decisions.md)
- [§18 — Requirement traceability](docs/13-requirement-traceability.md)
- [§19,20 — Nonfunctional & verification](docs/14-nonfunctional-and-validation.md)
- [§21,22 — Delivery & risks](docs/15-delivery-and-risks.md)
- [§23 — Glossary & contracts](docs/16-glossary-contract-index.md)
- [Go / ClackBT integration appendix](docs/go-clackbt-integration.md)

## Precedence and scope

1. The original supplied `source-planning-package/PRD.md` and its 16 schemas are preserved as the product/wire-contract baseline. User-approved amendments in [D1–D7](docs/17-approved-decisions.md) take precedence where stated. Four new schemas under `schemas/` define install-plan v3 and three inventory/doctor outputs at v1; scan/search/init stay v2. The split documents preserve all 72 original acceptance criteria.
2. The user's newer decision **Go + `rigerc/clackbt`** supersedes the original TypeScript/Node/Ink implementation recommendation wherever the source planning documents say otherwise. No JSON schemas or product behavior were intentionally changed by this stack decision.
3. Remaining design decisions are proposals until engineering sign-off. The private starter was inspected through read-only GitHub access; it was not forked, cloned, altered, built or tested. Go 1.26.10 and the pinned libraries are observed from its current `go.mod` and should be frozen against an immutable commit.
4. Node.js is not a runtime requirement of Nosey itself, although the **external** Vercel `skills` CLI launcher may require Node.js/npm. Real installer compatibility is a release blocker.

**Current documentation is Markdown.** Contract/reference assets remain under `source-planning-package/`. The previous consolidated Markdown/DOCX are retained under `archive/expanded-typescript-draft/` for history only; their TypeScript architecture is superseded. Files are split by ownership and navigable using relative links; section numbering remains stable.

Local revision draft.4 records approved gap resolutions and the selected `rigerc/skills-catalog` authoring sources. It fixes milestone acceptance ordering, defines persistent installation sources and hash-bound approval, completes seven JSON contracts, specifies exact-root scanning and invalid-overlay handling, and adds a reviewed catalog conversion/crosswalk. Schema files are specification artifacts, not Go implementation evidence. Repository/submodule creation is planned, not executed. The original source package and all 72 acceptance criteria are unchanged.
