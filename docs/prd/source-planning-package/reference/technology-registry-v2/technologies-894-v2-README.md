# Nosey technologies registry — schema v2

## Contents

- `technologies-894-v2.json`: complete 894-identity registry using exact original Stack Analyser IDs.
- `schemas/technologies.v2.schema.json` (at package root): unchanged JSON Schema Draft 2020-12 for the registry (partial override documents are permitted).
- `technologies-894-v2-report.json`: conversion statistics, lists of rule-backed and metadata-only identities, and caveats.

## What's changed

The schema is versioned to `schemaVersion: 2` and adds:

1. `dependency.ecosystem` values `docker`, `terraform`, `terraform.resource`, `githubAction`, `deno`, and `hex` alongside existing ecosystems.
2. A dedicated `mcp_package` rule kind, requiring `name`. Package matches are exact names or that name followed by an explicit version suffix such as `@latest`, not arbitrary substring matches.
3. `metadataOnly: true` for source registry identities with no active rule. Such identities remain addressable in the registry but **must not be reported as detected** in the absence of another evidence source.
4. Support for original scoped ID `@react-three/fiber`, removing the v1 alias `react-three-fiber`.
5. Source-native custom detector references (`kind: "detector"`) for 14 formerly omitted rules. These are **references**, not embedded custom detector implementations.

## Status

- 894 technology identities, all present, none `disabled: true`.
- 875 identities with one or more rule definitions.
- 19 explicit metadata-only identities without any source-backed active rule.
- Previously omitted signals now represented: **659/659** (637 dependencies, 14 custom detector references, 8 MCP package signals).
- Total rule definitions: **3,637**.
- Existing 357 non-dependency catalog path/glob/content/extension signals were already converted, and all 8 MCP signals are now represented, giving **365/365**.
- All 66 original catalog glob/content patterns remain represented.
- JSON Schema Draft 2020-12 validation passes; 580 exported regex patterns compile with Node.js.

The 19 metadata-only identities are listed in the report. They include provider/product grouping identities and two package managers (`cargo`, `phpcomposer`) whose candidate file patterns were commented out in the original code. New active detection rules were **not invented** for those technologies.

## MCP detection

This registry supports MCP detection when the consumer opts in and parses its MCP configuration files. The planned detector looks for explicit package references in `command`, `args` and `package` values of the `mcpServers`, `servers` or `mcp` configuration blocks. It understands JSON, JSONC and `.codex/config.toml` files in these locations (including nested workspace roots):

- `.mcp.json`
- `.cursor/mcp.json`
- `.vscode/mcp.json`
- `.claude/settings.json`
- `.gemini/settings.json`
- `.codex/config.toml`
- `opencode.json` and `opencode.jsonc`
- `.continue/config.json`

The specified matcher returns project-relative positive evidence, does not run commands, excludes symlinks, enforces a **65,536-byte** per-file read limit, and defaults to disabled without `--enable-mcp`.

Example `mcp_package` rule:

```json
{"id": "mcp_package-0001", "kind": "mcp_package", "name": "@playwright/mcp"}
```

## Integrating dependency ecosystems

The schema now **represents** Docker image, Terraform resource/provider, GitHub Actions, Deno, and Hex package signals. To *detect* them, the consuming application must supply corresponding normalized dependency observations. Adding ecosystem values to JSON Schema alone does not create manifest parsers. The source Stack Analyser already implements many of these scanners; adapt the registry to the correct observation types rather than treating the JSON file as an executable scanner.

Similarly, the 14 `kind: "detector"` rule entries use their original exported native function names and file paths. An engine that does not expose those functions needs a detector registry/adapter before the rules can run. The `mcp_package` rule vocabulary does not implement the other custom detectors or manifest parsers. No executable detector is included in this planning package.

## Planning-only distribution

The original technology rule JSON and v2 schema are preserved unchanged. Test suites, fixture workspaces, validators and reference Python programs are intentionally not distributed in this package. Historical source-conversion statistics are retained in the report but do not demonstrate executable Nosey behavior.

## Migration

1. Update consumers to understand `schemaVersion: 2`, `metadataOnly`, `mcp_package` and the new dependency ecosystems.
2. Load the new JSON and schema. Preserve the `@react-three/fiber` source ID (no alias needed).
3. Route `mcp_package` to the project-local MCP matcher, explicitly opt-in.
4. Connect new ecosystem signals to the relevant dependency parsers; wire custom detector references to implementations.
5. Never emit a positive detection for an entry with `metadataOnly: true` without an independent supporting signal.
