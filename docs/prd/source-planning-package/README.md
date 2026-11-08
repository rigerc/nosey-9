# Nosey PRD v1.7.1 — Planning package

This is a **planning-only specification**. No Nosey implementation, executable matcher, integration test, fixture, validator, or sample workspace is included. The PRD defines proposed product behavior, and its JSON Schemas define the unpublished data and machine-output contracts.

## Package contents

- `PRD.md` — complete v1.7.1 PRD, including workflows, seven rule kinds, coverage/completeness, recommendation mappings, source trust, the pinned upstream interface, installation provenance, acceptance criteria and launch gates.
- `schemas/` — JSON Schema Draft 2020-12 definitions for registry manifests, v1/v2 technology documents, skills/mappings source and effective documents, repositories, settings, registry-init templates, four CLI output shapes, scaffold provenance and project install-origin receipts.
- `example-registry/` — a sample registry with Next.js/TypeScript detection, skill catalog, recommendations, repository scores, local skill `SKILL.md` sources and two external-registry-init template examples.
- `reference/technology-registry-v2/technologies-894-v2.json` — **unchanged** authored technology identities and detection rules: 894 identities, 3,637 rules, including metadata-only entries.
- `reference/technology-registry-v2/technologies-894-v2-report.json` and `technologies-894-v2-README.md` — source catalog description and original statistics (the README's obsolete script/test instructions have been removed).

`schemas/technologies.v2.schema.json` is also unchanged from the supplied reference. The 894-technology catalog is a planning/reference asset, not a registry embedded in Nosey: the application will fetch its official registry from a Git source at runtime.

## Document versioning

- Registry manifest, settings, scaffold and template descriptors: `schemaVersion: 1`.
- Technology documents: `schemaVersion: 1` or `2`, dispatched by the technology document independently of the registry manifest.
- Skill and mapping documents: `schemaVersion: 2` only. Source overlays and effective documents have separate schemas.
- CLI machine output: `outputVersion: 2` in `scan`, `search`, dry-run install plan and registry-init output schemas. These contracts are not yet implemented.

## Interpretation

All behavior is specified, not demonstrated. `skills@1.7.6` is the proposed pinned integration baseline; tagged-source inspection establishes some parser/JSON support but **not** successful installation, cache/lockfile identity, or agent reconciliation. Section 16 of the PRD records implementation-entry and launch gates. Accepting these planning schemas must not be mistaken for runtime verification.

No fixture files, test suites, validation scripts, sample workspaces or Python detector reference implementation are distributed.
