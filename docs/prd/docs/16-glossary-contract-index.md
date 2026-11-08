# Glossary & contracts

[← Specification index](../README.md) · [Previous](15-delivery-and-risks.md)

## 23. Glossary and contract index

**Catalog skill ID:** Nosey-wide logical ID used by registry merge, selection and mappings (e.g. `vercel:web-design-guidelines`). **Upstream skill name:** `source.skillName`, matching `SKILL.md` frontmatter and CLI `--skill`. **Source registry:** Git/local Nosey metadata provider. **Skill repository:** Git/local repository from which skills are installed. **Requested ref:** User-declared Git ref, not necessarily a resolved commit. **Snapshot revision:** Actual validated Git commit SHA. **Logical origin:** Stable installer identity independent of cache checkout location. **Execution source:** Actual path/URI passed to upstream CLI. **Installed unknown:** Reader cannot prove installed or missing at the requested project/agent scope. **Execution complete:** All required detection rules evaluated, not necessarily complete technology knowledge. **Unevaluated recommendation:** Mapping gate contains unknown detection and cannot yet be confirmed. **Author-assessed score:** Source registry's subjective metadata, not measured detection confidence.

Package paths below are relative to `docs/prd/`, the specification root.

### Package-backed contract files

- `source-planning-package/PRD.md` — original 17-section v1.7.1 normative planning specification with AC-01–AC-72.
- `source-planning-package/schemas/registry.schema.json`, `technologies.schema.json`, `technologies.v2.schema.json`, `repositories.schema.json` — registry/technology/repository data.
- `source-planning-package/schemas/skills.schema.json`, `skills.effective.schema.json`, `mappings.schema.json`, `mappings.effective.schema.json` — source overlays and effective objects.
- `source-planning-package/schemas/settings.schema.json`, `template.schema.json`, `scaffold.schema.json`, `install-origins.schema.json` — local configuration, template and origin attribution.
- Active extensions: [install-plan v3](../schemas/install-plan-output.v3.schema.json), [list v1](../schemas/list-output.schema.json), [registry-list v1](../schemas/registry-list-output.schema.json), [doctor v1](../schemas/doctor-output.schema.json); authority and semantics in [D1–D7](17-approved-decisions.md).
- `source-planning-package/schemas/scan-output.schema.json`, `search-output.schema.json`, `install-plan-output.schema.json`, `registry-init-output.schema.json` — original command-output v2 baseline; install-plan v2 is historical, the other three stay active.
- `source-planning-package/example-registry/` — planning examples only, not a fixture test suite.
- `source-planning-package/reference/technology-registry-v2/technologies-894-v2.json` — unchanged reference source catalog; not a runtime-embedded default.

**External upstream reading (identified by the original PRD, not revalidated here):** <https://github.com/vercel-labs/skills>, <https://github.com/vercel-labs/skills/blob/v1.7.6/src/source-parser.ts>, <https://github.com/vercel-labs/skills/blob/v1.7.6/src/cli.ts>. The integration behavior described in §§8–10 remains subject to disposable-workspace execution tests.

---

**End of expanded planning specification.** The Go/ClackBT choice is user-approved and starter facts are read-only inspected. No application implementation, Go compilation/test, install execution, schema validator run or live upstream compatibility certification is implied.
