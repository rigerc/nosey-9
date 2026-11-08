# Delivery & risks

[← Specification index](../README.md) · [Previous](14-nonfunctional-and-validation.md) · [Next](16-glossary-contract-index.md)

## 21. Delivery plan, dependency graph and handoff

Phases follow the full MVP scope of §15; these are **sequence recommendations**, not time or staffing commitments. Estimate after the P0 baseline and early registry/installer evidence reduce uncertainty. Full G1/G2 exit evidence is accumulated during implementation, not required before writing the tests that establish it. The detailed repository work breakdown and comparison live in [IMPLEMENTATION_PLAN.md](../IMPLEMENTATION_PLAN.md#8-comparison-with-the-supplied-go-prd-implementation-plan).

| Phase | Deliverable / vertical slice | Prerequisite | Definition of done |
|---|---|---|---|
| P0 — contractual spike | Freeze version map and Go/starter baseline; inspect/test pinned upstream; propose OS/launcher and registry identity | Source package available | Approved contract baseline and recorded G4 findings; full G1 semantic-test evidence completes during P1–P3 |
| P1 — registry core | Git/local source, cache, trust fingerprint, schema decode, effective merge, provenance, doctor diagnostics | P0 | Invalid snapshot cannot displace valid; editable overrides correct |
| P1R — official registry repository + submodule | Convert pinned user-selected skills-catalog inputs using reviewed ID maps; create separate `nosey-registry` data repo, catalog/templates, validation CI, immutable initial publication; add `registries/default/` at exact gitlink SHA | P0 identity/owner + P1 local validation checkpoint | Real URL/maintainer recorded, original catalog preserved with reviewed metadata-only additions, template sources valid, `.gitmodules`/gitlink reproducible; no M6 init requirement for P1R closure |
| P2 — detector | Workspace boundary discovery, seven rule kinds with providers, MCP opt-in, adapter trust and coverage state | P1 | v2 894-ID reference loads unchanged; rule-state and coverage truth tables pass |
| P3 — recommendation/search | skills/mappings v2, conditional engine, repository scores, ranking, Explore index and JSON scan/search | P2 | No false recommendation from unknown; deterministic query/filter/grouper |
| P4 — project installation | Source/ref/agent preflight, plan/dry-run/print, upstream runner, installed reader, origin receipt | P0, P1, P3 | Reconciled real per-skill project results and failure recovery; G4/G5 |
| P4R — external registry initialization | Data-only template generation, selected workspace seeding, atomic output, scaffold provenance, optional Git/register actions | P1 remote/cache/trust, P1R templates, P2/P3 scan | Both templates work via remote and complete offline cache; collisions/traversal leave no partial generation |
| P5 — TUI and settings | Four tabs, keyboard focus, dynamic footer, modes, filtered selections, registry trust and init wizard | P1–P4R, including P1R | AC-aligned keyboard journeys, accessibility and narrow-terminal checks |
| P6 — release hardening | Full OS/terminal/test matrix, performance, docs, governance and recommendation studies | All preceding phases, including P1R/P4R | G1–G7 all passed and sign-off logged |

**First end-to-end implementation slice:** Run `nosey scan --json` using a trusted local example registry, detect Next.js and TypeScript in a small synthetic project, emit safe rule evidence, confirm one eligible mapping, and validate the output v2 schema. Next prove an installation **dry-run** with source/skill/agent/identity only, then a real disposable-project adapter test before enabling project write actions. This limits integration risk without reducing eventual MVP scope.

**Approved gap resolutions:** [D1–D7](17-approved-decisions.md) assign config/JSON contracts to M1, persistent sources/hash-bound approval to M5, exact-root discovery to M2 and source/effective isolation to M1/M3. P4R owns runtime init acceptance; P1R owns only validated publication and the submodule. G2 aggregates publication, M3 remote-fetch and P4R init proof.

**Dependency clarification:** P1 local validation enables P1R publication while remote adapter work continues; default-source acceptance needs the published P1R remote. A minimal local scan/search/dry-run checkpoint can precede complete P2/P3 capabilities. The submodule is development-only: ordinary builds and runtime source fetching must not depend on it. P4R is required registry-init behavior already in §15, now explicit rather than hidden inside the P5 wizard. See the [registry/submodule contract](04-registry-contracts.md#default-registry-repository-and-development-submodule).

**Handoff checklist:** Each phase passes Go interfaces and packages, JSON Schema references, tested decisions, fixture inventory, diagnostics, security-relevant trust boundaries, integration behavior, rollback/repair limitations and owner sign-off to the next phase. Implementation code and tests are not included in the supplied planning archive and have not been created or executed by this expanded PRD.

## 22. Risk register and unresolved decision owners

| Risk / open issue | Likelihood × impact (qualitative) | Mitigation | Proposed owner / gate |
|---|---|---|---|
| v1.7.6 CLI output or multi-skill grammar differs from source interpretation | High × Critical until tested | Tagged-version real e2e with disposable agent targets; fallback to verified one-skill invocations | Integration lead / G4 |
| Cached local skill installs cannot be reconciled with upstream lockfile/source | High × High until tested | Separate logical and execution source; origin receipts plus physical project inventory | Integration lead / G4–G5 |
| Official default registry repository/URL/template publication not provisioned | Certain × Critical | Execute P1R: confirm owner/visibility, create data repo, validate/publish SHA, attach exact development submodule and prove remote/cache path | Product/registry + CI owner / G2 |
| Development submodule becomes runtime fallback or leaks private CI access | Medium × High | Build without checkout, exclude payloads from releases, initialize only trusted exact gitlink, no fork credentials or recursive runtime fetch | Security/platform / G2,G5 |
| Source overlay can combine misleading source/repo/score provenance | Medium × High | Atomic source fields, explicit overrides, effective repository match and author labels | Domain + security / G1,G5 |
| Disabled MCP/provider creates false negative technology inference | Medium × High | Required/excluded modes, Kleene conditions, `unknown` and coverage gaps | Detection lead / G3 |
| Hostile Git paths, template symlinks or argv tokens trigger unintended effects | Medium × Critical | Canonical path checks, hardened Git subprocess, fuzz/negative security tests | Security + platform / G5 |
| Performance target is unspecifiable without fixture/runner | Certain × Medium | Agree reproducible corpus/hardware and separate refresh/cache measurement | QA/performance / G6 |
| Official scores interpreted as certification or objectively measured quality | Medium × High | Source attribution, rubric, independent correction/reassessment process | Registry governance / G7 |
| Complex TUI grouping and hidden selections create unreviewed install set | Medium × High | Total vs visible counts, confirmation shows hidden selections, keyboard journeys | Product/UX / G6 |
| Schema files are mistaken for executable implementation evidence | Medium × High | Distinct planning and tested gates; reject unsupported success claims | Release manager / G1–G7 |

### Required approvals before implementation and launch

- **Product owner:** confirm MVP scope, target users and exact wording of recommendations with uncertain coverage; define study dataset/acceptance thresholds.
- **Engineering lead:** pin the inspected `rigerc/clackbt` starter commit, approve Go/ClackBT package mapping, supported platforms, Go build/release matrix, Bubble Tea/Clack interaction tests, performance budgets and cache concurrency design.
- **CLI integration lead:** approve real upstream compatibility and per-skill identity reconciliation, or formally block install execution.
- **Security owner:** review source/registry/trust controls, untrusted input boundaries, adapter policy and no-global guarantee.
- **Registry maintainer:** create and publish the official registry repository/template revision through P1R, with scoring editorial policy and recovery procedures; repository/CI owner records and maintains the separate application gitlink.
- **QA/release owner:** map all 72 existing acceptance criteria to executable evidence and attest release gates.
