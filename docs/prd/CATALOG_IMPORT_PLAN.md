# Skills and mappings import plan

Status: imported and now maintained as native registry data. `registries/default/` is authoritative; the one-time importer and its inputs were removed. The user confirms the supplied skill sources are already verified. Reuse that verification; do not repeat source audits, license reviews or installation tests as import prerequisites. Ignore frontend bonus data entirely.

## Inputs and result

Use the supplied `rigerc/skills-catalog` source ledger and updated mappings from the same recorded revision. The inspected baseline is `d1bc68a826cd233f85f291eca0cfa9b4303713e6`:

- [Source ledger](https://github.com/rigerc/skills-catalog/blob/d1bc68a826cd233f85f291eca0cfa9b4303713e6/data/catalog-source-ledger.json): 877 verified skill records.
- [Updated mappings](https://github.com/rigerc/skills-catalog/blob/d1bc68a826cd233f85f291eca0cfa9b4303713e6/data/mappings.json): 289 technology entries and 35 combinations.

Produce official registry `skills.json` and `mappings.json` at schemaVersion 2, plus repository identities and technology-ID alignment needed by those documents. Preserve source URLs/commits and upstream skill names. Import metadata and references; do not copy skill payloads or install skills.

## 1. Read the verified catalog

Load the ledger and mappings, record their revision, and accept the existing source verification. Apply declared repository aliases, including `affaan-m/everything-claude-code` → `affaan-m/ECC`.

Use supplied source/skill metadata directly. If a required output field is absent, resolve only that missing metadata; this is not a new verification campaign. In particular, `source.skillName` must be the upstream installation name, using an explicit override where supplied. Do not guess a different name merely to make the output validate. Optional descriptions/tags/ratings remain absent when not supplied.

## 2. Align registry IDs

Maintain explicit stable ID maps:

- **Skills:** map slash-separated upstream IDs to valid Nosey skill IDs, checking collisions and preserving the upstream ID/source identity in import provenance. Apply the same map to every mapping reference.
- **Technologies:** preserve exact matches to the 894-technology catalog. Review the 41 nonmatching IDs through the approved explicit crosswalk; map equivalent identities to existing IDs and add genuinely new identities as metadata-only. Preserve the original 894 definitions/rules unchanged.

Do not merge distinct skills merely because their names match. Record confirmed aliases explicitly. These are compatibility conversions for the official registry, not source re-verification.

## 3. Convert skills and updated mappings

Generate `skills.json` entries with stable ID, name, supported metadata, canonical repository identity, and Git source URI/ref/skillName from the verified catalog.

Generate `mappings.json` as follows:

1. Convert each technology's `skill_ids` into Nosey recommendation entries using the skill and technology ID maps.
2. Convert the 35 combinations into separate conditional mappings: a deterministic required technology is the anchor, with remaining required technologies in `when.allTechnologies`. Combination `requires` means technology conditions, not skill prerequisites.
3. Keep conditional combinations separate from unconditional technology mappings so their conditions are not overwritten by ID-based merging.
4. Preserve supplied reasons/priorities when represented in the input; otherwise use the normative optional priority and a factual mapping explanation. Do not invent scores, quality assessments, prerequisites or conflicts.
5. Retain detect-only technologies without inventing recommendations. Metadata-only conditions remain unknown until detection rules are separately supplied.

Ignore `frontend_essentials` completely: no bonus skills, bonus conditions, flags or follow-up review. Skills in that section are still imported if present in the verified ledger; the bonus section itself creates no recommendations. Ignore `mcp_scan_context` and source detection examples for this import as well; the task is skills and mappings, not changes to Nosey's detector or scan behavior.

## 4. Check the generated registry

Limit import validation to the generated data:

- Skills/mappings source and effective schemas, unique IDs and resolved references.
- All 877 input skill records accounted for, with explicit aliases if generated unique count differs.
- All technology mappings and 35 combinations accounted for after crosswalk conversion; frontend bonus intentionally excluded.
- Source URI/commit/skill-name metadata preserved, original technology rules unchanged, no fabricated ratings.
- Passing/failing/unknown combination conditions, deterministic ordering and byte-identical regeneration.

Fix conversion errors without silently dropping skills or mappings. Missing required metadata or ambiguous identity conversion is reported specifically; it does not trigger a full source audit. General pinned-installer compatibility remains a separate Nosey implementation concern, not a prerequisite to importing this already verified catalog.

## 5. Use in the official registry

Write the generated documents into the separate official registry, keeping a small import manifest, ID maps and summary alongside authoring metadata. Review the resulting registry diff and validation results. The registry repository/submodule setup remains M1R; runtime remote/cache checks remain M3 and registry-init checks remain M6.

Future imports use the same ID maps and show skill/source/mapping changes against the previous imported revision. Failed generation leaves the prior registry documents unchanged. No additional source-verification system, frontend bonus feature, skill installation or publishing automation is part of the importer.

## Completion criteria

The official registry contains the supplied verified skill catalog and its updated technology/combination mappings in Nosey's supported formats, with valid references and stable IDs. Repository destination details still need concrete values before writing to a remote. The local import produced 877 skills, 131 repository identities, 898 technologies (894 preserved plus four metadata-only additions), and 258 mappings including all 35 combinations. No remote registry publication or Git submodule creation has been performed.

Verify committed output with `make registry-check` (`nosey registry generate registries/default --check`). New changes are normal edits to the root data documents followed by `registry generate`; there is no bulk re-import or migration command.
