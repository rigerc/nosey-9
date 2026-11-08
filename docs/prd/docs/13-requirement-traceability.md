# Requirement traceability

[← Specification index](../README.md) · [Previous](12-scope-and-decisions.md) · [Next](14-nonfunctional-and-validation.md)

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
